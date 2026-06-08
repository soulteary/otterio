/*
 * MinIO Cloud Storage, (C) 2015, 2016, 2017, 2018 MinIO, Inc.
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
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/minio/minio-go/v7/pkg/set"
	"github.com/soulteary/otterio/pkg/bucket/bandwidth"
	"github.com/soulteary/otterio/pkg/handlers"
	"github.com/soulteary/otterio/pkg/kms"

	"github.com/dustin/go-humanize"
	"github.com/soulteary/otterio/cmd/config/cache"
	"github.com/soulteary/otterio/cmd/config/compress"
	"github.com/soulteary/otterio/cmd/config/dns"
	xldap "github.com/soulteary/otterio/cmd/config/identity/ldap"
	"github.com/soulteary/otterio/cmd/config/identity/openid"
	"github.com/soulteary/otterio/cmd/config/policy/opa"
	"github.com/soulteary/otterio/cmd/config/storageclass"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	etcd "go.etcd.io/etcd/client/v3"

	"github.com/soulteary/otterio/pkg/certs"
	"github.com/soulteary/otterio/pkg/event"
	"github.com/soulteary/otterio/pkg/pubsub"
)

// otterio configuration related constants.
const (
	GlobalOtterioDefaultPort = "9000"

	globalOtterioDefaultRegion = ""
	// This is a sha256 output of ``arn:aws:iam::otterio:user/admin``,
	// this is kept in present form to be compatible with S3 owner ID
	// requirements -
	//
	// ```
	//    The canonical user ID is the Amazon S3–only concept.
	//    It is 64-character obfuscated version of the account ID.
	// ```
	// http://docs.aws.amazon.com/AmazonS3/latest/dev/example-walkthroughs-managing-access-example4.html
	globalOtterioDefaultOwnerID      = "02d6176db174dc93cb1b899f7c6078f08654445fe8cf1b6ce98d8855f66bdbf4"
	globalOtterioDefaultStorageClass = "STANDARD"
	globalWindowsOSName              = "windows"
	globalMacOSName                  = "darwin"
	globalOtterioModeFS              = "mode-server-fs"
	globalOtterioModeErasure         = "mode-server-xl"
	globalOtterioModeDistErasure     = "mode-server-distributed-xl"
	globalOtterioModeGatewayPrefix   = "mode-gateway-"
	globalDirSuffix                  = "__XLDIR__"
	globalDirSuffixWithSlash         = globalDirSuffix + slashSeparator

	// Add new global values here.
)

const (
	// Limit fields size (except file) to 1Mib since Policy document
	// can reach that size according to https://aws.amazon.com/articles/1434
	maxFormFieldSize = int64(1 * humanize.MiByte)

	// Limit memory allocation to store multipart data
	maxFormMemory = int64(5 * humanize.MiByte)

	// The maximum allowed time difference between the incoming request
	// date and server date during signature verification.
	globalMaxSkewTime = 15 * time.Minute // 15 minutes skew allowed.

	// GlobalStaleUploadsExpiry - Expiry duration after which the uploads in multipart, tmp directory are deemed stale.
	GlobalStaleUploadsExpiry = time.Hour * 24 // 24 hrs.

	// GlobalStaleUploadsCleanupInterval - Cleanup interval when the stale uploads cleanup is initiated.
	GlobalStaleUploadsCleanupInterval = time.Hour * 12 // 12 hrs.

	// GlobalServiceExecutionInterval - Executes the Lifecycle events.
	GlobalServiceExecutionInterval = time.Hour * 24 // 24 hrs.

	// Refresh interval to update in-memory iam config cache.
	globalRefreshIAMInterval = 5 * time.Minute

	// Limit of location constraint XML for unauthenticated PUT bucket operations.
	maxLocationConstraintSize = 3 * humanize.MiByte

	// Maximum size of default bucket encryption configuration allowed
	maxBucketSSEConfigSize = 1 * humanize.MiByte

	// diskFillFraction is the fraction of a disk we allow to be filled.
	diskFillFraction = 0.95
)

var globalCLIContext = struct {
	JSON, Quiet     bool
	Anonymous       bool
	Addr            string
	ConsoleAddr     string
	ConsoleCertsDir string
	StrictS3Compat  bool
}{}

var (
	// Indicates if the running otterio server is distributed setup.
	globalIsDistErasure = false

	// Indicates if the running otterio server is an erasure-code backend.
	globalIsErasure = false

	// Indicates if the running otterio is in gateway mode.
	globalIsGateway = false

	// Name of gateway server, e.g S3, NAS, etc
	globalGatewayName = ""

	// This flag is set to 'true' by default
	globalBrowserEnabled = true

	// This flag is set to 'us-east-1' by default
	globalServerRegion = globalOtterioDefaultRegion

	// OtterIO local server address (in `host:port` format)
	globalOtterioAddr = ""
	// OtterIO default port, can be changed through command line.
	globalOtterioPort = GlobalOtterioDefaultPort
	// Holds the host that was passed using --address
	globalOtterioHost = ""
	// Holds the possible host endpoint.
	globalOtterioEndpoint = ""

	// Optional dedicated console listener address (in `host:port` format).
	// Empty means the console is served on the same listener as S3.
	globalOtterioConsoleAddr = ""
	// Holds the host part of --console-address (when set).
	globalOtterioConsoleHost = ""
	// Holds the port part of --console-address (when set).
	globalOtterioConsolePort = ""
	// Pre-rendered endpoint for the console listener (when split).
	globalOtterioConsoleEndpoint = ""

	// globalConfigSys server config system.
	globalConfigSys *ConfigSys

	globalNotificationSys  *NotificationSys
	globalConfigTargetList *event.TargetList
	// globalEnvTargetList has list of targets configured via env.
	globalEnvTargetList *event.TargetList

	globalBucketMetadataSys *BucketMetadataSys
	globalBucketMonitor     *bandwidth.Monitor
	globalPolicySys         *PolicySys
	globalIAMSys            *IAMSys

	globalLifecycleSys       *LifecycleSys
	globalBucketSSEConfigSys *BucketSSEConfigSys
	globalBucketTargetSys    *BucketTargetSys
	// globalAPIConfig controls S3 API requests throttling,
	// healthcheck readiness deadlines and cors settings.
	globalAPIConfig = apiConfig{listQuorum: 3}

	globalStorageClass storageclass.Config
	globalLDAPConfig   xldap.Config
	globalOpenIDConfig openid.Config

	// CA root certificates, a nil value means system certs pool will be used
	globalRootCAs *x509.CertPool

	// IsSSL indicates if the server is configured with SSL.
	globalIsTLS bool

	globalTLSCerts *certs.Manager

	// Optional dedicated TLS material for the console listener.
	// When nil, the console listener falls back to globalTLSCerts (shared with S3).
	globalConsoleTLSCerts    *certs.Manager
	globalConsolePublicCerts []*x509.Certificate
	// Indicates whether the dedicated console listener has its own TLS keypair.
	globalConsoleIsTLS bool

	globalHTTPServer        *xhttp.Server
	globalConsoleHTTPServer *xhttp.Server
	globalHTTPServerErrorCh = make(chan error)
	globalOSSignalCh        = make(chan os.Signal, 1)

	// global Trace system to send HTTP request/response
	// and Storage/OS calls info to registered listeners.
	globalTrace = pubsub.New()

	// global Listen system to send S3 API events to registered listeners
	globalHTTPListen = pubsub.New()

	// global console system to send console logs to
	// registered listeners
	globalConsoleSys *HTTPConsoleLoggerSys

	globalEndpoints EndpointServerPools

	// The name of this local node, fetched from arguments
	globalLocalNodeName string

	globalRemoteEndpoints map[string]Endpoint

	// Global server's network statistics
	globalConnStats = newConnStats()

	// Global HTTP request statistics
	globalHTTPStats = newHTTPStats()

	// Time when the server is started
	globalBootTime = UTCNow()

	globalActiveCred auth.Credentials

	// Hold the old server credentials passed by the environment
	globalOldCred auth.Credentials

	// Indicates if config is to be encrypted
	globalConfigEncrypted bool

	globalPublicCerts []*x509.Certificate

	globalDomainNames []string      // Root domains for virtual host style requests
	globalDomainIPs   set.StringSet // Root domain IP address(s) for a distributed OtterIO deployment

	globalOperationTimeout       = newDynamicTimeout(10*time.Minute, 5*time.Minute) // default timeout for general ops
	globalDeleteOperationTimeout = newDynamicTimeout(5*time.Minute, 1*time.Minute)  // default time for delete ops

	globalBucketObjectLockSys *BucketObjectLockSys
	globalBucketQuotaSys      *BucketQuotaSys
	globalBucketVersioningSys *BucketVersioningSys

	// Disk cache drives
	globalCacheConfig cache.Config

	// Initialized KMS configuration for disk cache
	globalCacheKMS kms.KMS

	// Allocated etcd endpoint for config and bucket DNS.
	globalEtcdClient *etcd.Client

	// Is set to true when Bucket federation is requested
	// and is 'true' when etcdConfig.PathPrefix is empty
	globalBucketFederation bool

	// Allocated DNS config wrapper over etcd client.
	globalDNSConfig dns.Store

	// GlobalKMS initialized KMS configuration
	GlobalKMS kms.KMS

	// Auto-Encryption, if enabled, turns any non-SSE-C request
	// into an SSE-S3 request. If enabled a valid, non-empty KMS
	// configuration must be present.
	globalAutoEncryption bool

	// Is compression enabled?
	globalCompressConfigMu sync.Mutex
	globalCompressConfig   compress.Config

	// Some standard object extensions which we strictly dis-allow for compression.
	standardExcludeCompressExtensions = []string{".gz", ".bz2", ".rar", ".zip", ".7z", ".xz", ".mp4", ".mkv", ".mov", ".jpg", ".png", ".gif"}

	// Some standard content-types which we strictly dis-allow for compression.
	standardExcludeCompressContentTypes = []string{"video/*", "audio/*", "application/zip", "application/x-gzip", "application/x-zip-compressed", " application/x-compress", "application/x-spoon"}

	// Authorization validators list.
	globalOpenIDValidators *openid.Validators

	// OPA policy system.
	globalPolicyOPA *opa.Opa

	// Deployment ID - unique per deployment
	globalDeploymentID string

	// GlobalGatewaySSE sse options
	GlobalGatewaySSE gatewaySSE

	globalAllHealState *allHealState

	// The always present healing routine ready to heal objects
	globalBackgroundHealRoutine *healRoutine
	globalBackgroundHealState   *allHealState

	// If writes to FS backend should be O_SYNC.
	globalFSOSync bool

	globalProxyEndpoints []ProxyEndpoint

	globalInternodeTransport http.RoundTripper

	globalProxyTransport http.RoundTripper

	globalDNSCache *xhttp.DNSCache

	globalForwarder *handlers.Forwarder
	// Add new variable global values here.
)

var errSelfTestFailure = errors.New("self test failed. unsafe to start server")

// Returns otterio global information, as a key value map.
// returned list of global values is not an exhaustive
// list. Feel free to add new relevant fields.
func getGlobalInfo() (globalInfo map[string]interface{}) {
	globalInfo = map[string]interface{}{
		"serverRegion": globalServerRegion,
		"domains":      globalDomainNames,
		// Add more relevant global settings here.
	}

	return globalInfo
}
