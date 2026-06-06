/*
 * MinIO Cloud Storage, (C) 2019 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"time"

	"github.com/minio/minio-go/v7/pkg/set"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/cmd/rest"
)

const (
	bootstrapRESTVersion       = "v1"
	bootstrapRESTVersionPrefix = SlashSeparator + bootstrapRESTVersion
	bootstrapRESTPrefix        = otterioReservedBucketPath + "/bootstrap"
	bootstrapRESTPath          = bootstrapRESTPrefix + bootstrapRESTVersionPrefix
)

const (
	bootstrapRESTMethodHealth = "/health"
	bootstrapRESTMethodVerify = "/verify"
)

// To abstract a node over network.
type bootstrapRESTServer struct{}

// ServerSystemConfig - captures information about server configuration.
type ServerSystemConfig struct {
	OtterioPlatform  string
	OtterioRuntime   string
	OtterioEndpoints EndpointServerPools
}

// Diff - returns error on first difference found in two configs.
func (s1 ServerSystemConfig) Diff(s2 ServerSystemConfig) error {
	if s1.OtterioPlatform != s2.OtterioPlatform {
		return fmt.Errorf("Expected platform '%s', found to be running '%s'",
			s1.OtterioPlatform, s2.OtterioPlatform)
	}
	if s1.OtterioEndpoints.NEndpoints() != s2.OtterioEndpoints.NEndpoints() {
		return fmt.Errorf("Expected number of endpoints %d, seen %d", s1.OtterioEndpoints.NEndpoints(),
			s2.OtterioEndpoints.NEndpoints())
	}

	for i, ep := range s1.OtterioEndpoints {
		if ep.SetCount != s2.OtterioEndpoints[i].SetCount {
			return fmt.Errorf("Expected set count %d, seen %d", ep.SetCount,
				s2.OtterioEndpoints[i].SetCount)
		}
		if ep.DrivesPerSet != s2.OtterioEndpoints[i].DrivesPerSet {
			return fmt.Errorf("Expected drives pet set %d, seen %d", ep.DrivesPerSet,
				s2.OtterioEndpoints[i].DrivesPerSet)
		}
		for j, endpoint := range ep.Endpoints {
			if endpoint.String() != s2.OtterioEndpoints[i].Endpoints[j].String() {
				return fmt.Errorf("Expected endpoint %s, seen %s", endpoint,
					s2.OtterioEndpoints[i].Endpoints[j])
			}
		}

	}
	return nil
}

func getServerSystemCfg() ServerSystemConfig {
	return ServerSystemConfig{
		OtterioPlatform:  fmt.Sprintf("OS: %s | Arch: %s", runtime.GOOS, runtime.GOARCH),
		OtterioEndpoints: globalEndpoints,
	}
}

// HealthHandler returns success if request is valid
func (b *bootstrapRESTServer) HealthHandler(_ http.ResponseWriter, _ *http.Request) {}

func (b *bootstrapRESTServer) VerifyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "VerifyHandler")
	// SECURITY (CVE-2023-28432): the bootstrap verify endpoint replies with
	// the cluster topology (OtterioPlatform / OtterioRuntime / OtterioEndpoints).
	// Authenticate the caller against the inter-node JWT (the same validator
	// peer-rest / storage-rest / lock-rest already use) so an unauthenticated
	// probe of /otterio/bootstrap/v1/verify cannot enumerate the cluster.
	// HealthHandler intentionally stays anonymous: it returns no body and
	// exists for external liveness probes.
	if err := storageServerRequestValidate(r); err != nil {
		w.Header().Set("X-Otterio-Bootstrap-Error", err.Error())
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(err.Error()))
		w.(http.Flusher).Flush()
		return
	}
	cfg := getServerSystemCfg()
	logger.LogIf(ctx, json.NewEncoder(w).Encode(&cfg))
	w.(http.Flusher).Flush()
}

// client to talk to bootstrap NEndpoints.
type bootstrapRESTClient struct {
	endpoint   Endpoint
	restClient *rest.Client
}

// Wrapper to restClient.Call to handle network errors, in case of network error the connection is marked disconnected
// permanently. The only way to restore the connection is at the xl-sets layer by xlsets.monitorAndConnectEndpoints()
// after verifying format.json
func (client *bootstrapRESTClient) callWithContext(ctx context.Context, method string, values url.Values, body io.Reader, length int64) (respBody io.ReadCloser, err error) {
	if values == nil {
		values = make(url.Values)
	}

	respBody, err = client.restClient.Call(ctx, method, values, body, length)
	if err == nil {
		return respBody, nil
	}

	return nil, err
}

// Stringer provides a canonicalized representation of node.
func (client *bootstrapRESTClient) String() string {
	return client.endpoint.String()
}

// Verify - fetches system server config.
func (client *bootstrapRESTClient) Verify(ctx context.Context, srcCfg ServerSystemConfig) (err error) {
	if newObjectLayerFn() != nil {
		return nil
	}
	respBody, err := client.callWithContext(ctx, bootstrapRESTMethodVerify, nil, nil, -1)
	if err != nil {
		return
	}
	defer xhttp.DrainBody(respBody)
	recvCfg := ServerSystemConfig{}
	if err = json.NewDecoder(respBody).Decode(&recvCfg); err != nil {
		return err
	}
	return srcCfg.Diff(recvCfg)
}

func verifyServerSystemConfig(ctx context.Context, endpointServerPools EndpointServerPools) error {
	srcCfg := getServerSystemCfg()
	clnts := newBootstrapRESTClients(endpointServerPools)
	var onlineServers int
	var offlineEndpoints []string
	var retries int
	for onlineServers < len(clnts)/2 {
		for _, clnt := range clnts {
			if err := clnt.Verify(ctx, srcCfg); err != nil {
				if isNetworkError(err) {
					offlineEndpoints = append(offlineEndpoints, clnt.String())
					continue
				}
				return fmt.Errorf("%s as has incorrect configuration: %w", clnt.String(), err)
			}
			onlineServers++
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Sleep for a while - so that we don't go into
			// 100% CPU when half the endpoints are offline.
			time.Sleep(100 * time.Millisecond)
			retries++
			// after 5 retries start logging that servers are not reachable yet
			if retries >= 5 {
				logger.Info(fmt.Sprintf("Waiting for atleast %d remote servers to be online for bootstrap check", len(clnts)/2))
				logger.Info(fmt.Sprintf("Following servers are currently offline or unreachable %s", offlineEndpoints))
				retries = 0 // reset to log again after 5 retries.
			}
			offlineEndpoints = nil
		}
	}
	return nil
}

func newBootstrapRESTClients(endpointServerPools EndpointServerPools) []*bootstrapRESTClient {
	seenHosts := set.NewStringSet()
	var clnts []*bootstrapRESTClient
	for _, ep := range endpointServerPools {
		for _, endpoint := range ep.Endpoints {
			if seenHosts.Contains(endpoint.Host) {
				continue
			}
			seenHosts.Add(endpoint.Host)

			// Only proceed for remote endpoints.
			if !endpoint.IsLocal {
				clnts = append(clnts, newBootstrapRESTClient(endpoint))
			}
		}
	}
	return clnts
}

// Returns a new bootstrap client.
func newBootstrapRESTClient(endpoint Endpoint) *bootstrapRESTClient {
	serverURL := &url.URL{
		Scheme: endpoint.Scheme,
		Host:   endpoint.Host,
		Path:   bootstrapRESTPath,
	}

	restClient := rest.NewClient(serverURL, globalInternodeTransport, newAuthToken)
	restClient.HealthCheckFn = nil

	return &bootstrapRESTClient{endpoint: endpoint, restClient: restClient}
}
