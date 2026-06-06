/*
 * MinIO Cloud Storage, (C) 2015-2021 MinIO, Inc.
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
	"bufio"
	"os"
	"runtime"
	"strings"

	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/env"
)

// IsDocker - returns if the environment otterio is running in docker or
// not. The check is a simple file existence check.
//
// https://github.com/moby/moby/blob/master/daemon/initlayer/setup_unix.go#L25
//
//	"/.dockerenv":      "file",
func IsDocker() bool {
	if env.Get("OTTERIO_CI_CD", "") == "" {
		_, err := os.Stat("/.dockerenv")
		if osIsNotExist(err) {
			return false
		}

		// Log error, as we will not propagate it to caller
		logger.LogIf(GlobalContext, err)

		return err == nil
	}
	return false
}

// IsDCOS returns true if otterio is running in DCOS.
func IsDCOS() bool {
	if env.Get("OTTERIO_CI_CD", "") == "" {
		// http://mesos.apache.org/documentation/latest/docker-containerizer/
		// Mesos docker containerizer sets this value
		return env.Get("MESOS_CONTAINER_NAME", "") != ""
	}
	return false
}

// IsKubernetesReplicaSet returns true if otterio is running in kubernetes replica set.
func IsKubernetesReplicaSet() bool {
	return IsKubernetes() && (env.Get("KUBERNETES_REPLICA_SET", "") != "")
}

// IsKubernetes returns true if otterio is running in kubernetes.
func IsKubernetes() bool {
	if env.Get("OTTERIO_CI_CD", "") == "" {
		// Kubernetes env used to validate if we are
		// indeed running inside a kubernetes pod
		// is KUBERNETES_SERVICE_HOST
		// https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kubelet_pods.go#L541
		return env.Get("KUBERNETES_SERVICE_HOST", "") != ""
	}
	return false
}

// IsBOSH returns true if otterio is deployed from a bosh package
func IsBOSH() bool {
	// "/var/vcap/bosh" exists in BOSH deployed instance.
	_, err := os.Stat("/var/vcap/bosh")
	if osIsNotExist(err) {
		return false
	}

	// Log error, as we will not propagate it to caller
	logger.LogIf(GlobalContext, err)

	return err == nil
}

// OtterIO Helm chart uses DownwardAPIFile to write pod label info to /podinfo/labels
// More info: https://kubernetes.io/docs/tasks/inject-data-application/downward-api-volume-expose-pod-information/#store-pod-fields
// Check if this is Helm package installation and report helm chart version
func getHelmVersion(helmInfoFilePath string) string {
	// Read the file exists.
	helmInfoFile, err := os.Open(helmInfoFilePath)
	if err != nil {
		// Log errors and return "" as OtterIO can be deployed
		// without Helm charts as well.
		if !osIsNotExist(err) {
			reqInfo := (&logger.ReqInfo{}).AppendTags("helmInfoFilePath", helmInfoFilePath)
			ctx := logger.SetReqInfo(GlobalContext, reqInfo)
			logger.LogIf(ctx, err)
		}
		return ""
	}

	scanner := bufio.NewScanner(helmInfoFile)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "chart=") {
			helmChartVersion := strings.TrimPrefix(scanner.Text(), "chart=")
			// remove quotes from the chart version
			return strings.Trim(helmChartVersion, `"`)
		}
	}

	return ""
}

// IsSourceBuild - returns if this binary is a non-official build from
// source code. Official OtterIO releases use a fixed version string
// constant baked in at build time; anything else (e.g. "DEVELOPMENT.GOGET")
// is treated as a source build.
func IsSourceBuild() bool {
	return Version == "DEVELOPMENT.GOGET"
}

// IsPCFTile returns if server is running in PCF
func IsPCFTile() bool {
	return env.Get("OTTERIO_PCF_TILE_VERSION", "") != ""
}

// DO NOT CHANGE USER AGENT STYLE.
// The style should be
//
//	OtterIO (<OS>; <ARCH>[; <MODE>][; dcos][; kubernetes][; docker][; source]) OtterIO/<VERSION> OtterIO/<RELEASE-TAG> OtterIO/<COMMIT-ID> [OtterIO/universe-<PACKAGE-NAME>] [OtterIO/helm-<HELM-VERSION>]
//
// Any change here should be discussed by opening an issue at
// https://github.com/soulteary/otterio/issues.
func getUserAgent(mode string) string {

	userAgentParts := []string{}
	// Helper function to concisely append a pair of strings to a
	// the user-agent slice.
	uaAppend := func(p, q string) {
		userAgentParts = append(userAgentParts, p, q)
	}

	uaAppend("OtterIO (", runtime.GOOS)
	uaAppend("; ", runtime.GOARCH)
	if mode != "" {
		uaAppend("; ", mode)
	}
	if IsDCOS() {
		uaAppend("; ", "dcos")
	}
	if IsKubernetes() {
		uaAppend("; ", "kubernetes")
	}
	if IsDocker() {
		uaAppend("; ", "docker")
	}
	if IsBOSH() {
		uaAppend("; ", "bosh")
	}
	if IsSourceBuild() {
		uaAppend("; ", "source")
	}

	uaAppend(") OtterIO/", Version)
	uaAppend(" OtterIO/", ReleaseTag)
	uaAppend(" OtterIO/", CommitID)
	if IsDCOS() {
		universePkgVersion := env.Get("MARATHON_APP_LABEL_DCOS_PACKAGE_VERSION", "")
		// On DC/OS environment try to the get universe package version.
		if universePkgVersion != "" {
			uaAppend(" OtterIO/universe-", universePkgVersion)
		}
	}

	if IsKubernetes() {
		// In Kubernetes environment, try to fetch the helm package version
		helmChartVersion := getHelmVersion("/podinfo/labels")
		if helmChartVersion != "" {
			uaAppend(" OtterIO/helm-", helmChartVersion)
		}
		// In Kubernetes environment, try to fetch the Operator, VSPHERE plugin version
		opVersion := env.Get("OTTERIO_OPERATOR_VERSION", "")
		if opVersion != "" {
			uaAppend(" OtterIO/operator-", opVersion)
		}
		vsphereVersion := env.Get("OTTERIO_VSPHERE_PLUGIN_VERSION", "")
		if vsphereVersion != "" {
			uaAppend(" OtterIO/vsphere-plugin-", vsphereVersion)
		}
	}

	if IsPCFTile() {
		pcfTileVersion := env.Get("OTTERIO_PCF_TILE_VERSION", "")
		if pcfTileVersion != "" {
			uaAppend(" OtterIO/pcf-tile-", pcfTileVersion)
		}
	}

	return strings.Join(userAgentParts, "")
}
