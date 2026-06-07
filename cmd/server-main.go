/*
 * MinIO Cloud Storage, (C) 2015-2019 MinIO, Inc.
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
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/minio/cli"
	"github.com/soulteary/otterio/cmd/config"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/cmd/rest"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/bandwidth"
	"github.com/soulteary/otterio/pkg/certs"
	"github.com/soulteary/otterio/pkg/color"
	"github.com/soulteary/otterio/pkg/env"
	"github.com/soulteary/otterio/pkg/fips"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/soulteary/otterio/pkg/sync/errgroup"
)

// ServerFlags - server command specific flags
var ServerFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "address",
		Value: ":" + GlobalOtterioDefaultPort,
		Usage: "bind to a specific ADDRESS:PORT, ADDRESS can be an IP or hostname",
	},
	cli.StringFlag{
		Name:   "console-address",
		Value:  "",
		Usage:  "bind the web console + admin API to a separate ADDRESS:PORT (default: same as --address)",
		EnvVar: "OTTERIO_BROWSER_ADDRESS",
	},
	cli.StringFlag{
		Name:   "console-certs-dir",
		Value:  "",
		Usage:  "path to a separate certs directory for the console listener (requires --console-address)",
		EnvVar: "OTTERIO_BROWSER_CERTS_DIR",
	},
}

var serverCmd = cli.Command{
	Name:   "server",
	Usage:  "start object storage server",
	Flags:  append(ServerFlags, GlobalFlags...),
	Action: serverMain,
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} {{if .VisibleFlags}}[FLAGS] {{end}}DIR1 [DIR2..]
  {{.HelpName}} {{if .VisibleFlags}}[FLAGS] {{end}}DIR{1...64}
  {{.HelpName}} {{if .VisibleFlags}}[FLAGS] {{end}}DIR{1...64} DIR{65...128}

DIR:
  DIR points to a directory on a filesystem. When you want to combine
  multiple drives into a single large system, pass one directory per
  filesystem separated by space. You may also use a '...' convention
  to abbreviate the directory arguments. Remote directories in a
  distributed setup are encoded as HTTP(s) URIs.
{{if .VisibleFlags}}
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}
EXAMPLES:
  1. Start otterio server on "/home/shared" directory.
     {{.Prompt}} {{.HelpName}} /home/shared

  2. Start single node server with 64 local drives "/mnt/data1" to "/mnt/data64".
     {{.Prompt}} {{.HelpName}} /mnt/data{1...64}

  3. Start distributed otterio server on an 32 node setup with 32 drives each, run following command on all the nodes
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_ROOT_USER{{.AssignmentOperator}}otterio
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_ROOT_PASSWORD{{.AssignmentOperator}}otteriostorage
     {{.Prompt}} {{.HelpName}} http://node{1...32}.example.com/mnt/export{1...32}

  4. Start distributed otterio server in an expanded setup, run the following command on all the nodes
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_ROOT_USER{{.AssignmentOperator}}otterio
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_ROOT_PASSWORD{{.AssignmentOperator}}otteriostorage
     {{.Prompt}} {{.HelpName}} http://node{1...16}.example.com/mnt/export{1...32} \
            http://node{17...64}.example.com/mnt/export{1...64}

  5. Start otterio server with the web console listening on a separate port.
     {{.Prompt}} {{.HelpName}} --address ":9000" --console-address ":9001" /home/shared

  6. Same as above, with a dedicated TLS keypair for the console listener.
     {{.Prompt}} {{.HelpName}} --address ":9000" --console-address ":9001" \
            --certs-dir /etc/otterio/certs/s3 \
            --console-certs-dir /etc/otterio/certs/console /home/shared
`,
}

func serverCmdArgs(ctx *cli.Context) []string {
	v := env.Get(config.EnvArgs, "")
	if v == "" {
		// Fall back to older ENV OTTERIO_ENDPOINTS
		v = env.Get(config.EnvEndpoints, "")
	}
	if v == "" {
		if !ctx.Args().Present() || ctx.Args().First() == "help" {
			cli.ShowCommandHelpAndExit(ctx, ctx.Command.Name, 1)
		}
		return ctx.Args()
	}
	return strings.Fields(v)
}

func serverHandleCmdArgs(ctx *cli.Context) {
	// Handle common command args.
	handleCommonCmdArgs(ctx)

	logger.FatalIf(CheckLocalServerAddr(globalCLIContext.Addr), "Unable to validate passed arguments")

	var err error
	var setupType SetupType

	// Check and load TLS certificates.
	globalPublicCerts, globalTLSCerts, globalIsTLS, err = getTLSConfig()
	logger.FatalIf(err, "Unable to load the TLS configuration")

	// Optionally load a dedicated TLS keypair for the console listener.
	logger.FatalIf(loadConsoleTLSConfig(), "Unable to load the console TLS configuration")

	// Check and load Root CAs.
	globalRootCAs, err = certs.GetRootCAs(globalCertsCADir.Get())
	logger.FatalIf(err, "Failed to read root CAs (%v)", err)

	// Add the global public crts as part of global root CAs
	for _, publicCrt := range globalPublicCerts {
		globalRootCAs.AddCert(publicCrt)
	}

	// Register root CAs for remote ENVs
	env.RegisterGlobalCAs(globalRootCAs)

	globalOtterioAddr = globalCLIContext.Addr

	globalOtterioHost, globalOtterioPort = mustSplitHostPort(globalOtterioAddr)
	globalEndpoints, setupType, err = createServerEndpoints(globalCLIContext.Addr, serverCmdArgs(ctx)...)
	logger.FatalIf(err, "Invalid command line arguments")

	globalLocalNodeName = GetLocalPeer(globalEndpoints, globalOtterioHost, globalOtterioPort)

	globalRemoteEndpoints = make(map[string]Endpoint)
	for _, z := range globalEndpoints {
		for _, ep := range z.Endpoints {
			if ep.IsLocal {
				globalRemoteEndpoints[globalLocalNodeName] = ep
			} else {
				globalRemoteEndpoints[ep.Host] = ep
			}
		}
	}

	// allow transport to be HTTP/1.1 for proxying.
	globalProxyTransport = newCustomHTTPProxyTransport(&tls.Config{
		RootCAs:          globalRootCAs,
		CipherSuites:     fips.CipherSuitesTLS(),
		CurvePreferences: fips.EllipticCurvesTLS(),
	}, rest.DefaultTimeout)()
	globalProxyEndpoints = GetProxyEndpoints(globalEndpoints)
	globalInternodeTransport = newInternodeHTTPTransport(&tls.Config{
		RootCAs:          globalRootCAs,
		CipherSuites:     fips.CipherSuitesTLS(),
		CurvePreferences: fips.EllipticCurvesTLS(),
	}, rest.DefaultTimeout)()

	// On macOS, if a process already listens on LOCALIPADDR:PORT, net.Listen() falls back
	// to IPv6 address ie otterio will start listening on IPv6 address whereas another
	// (non-)otterio process is listening on IPv4 of given port.
	// To avoid this error situation we check for port availability.
	logger.FatalIf(checkPortAvailability(globalOtterioHost, globalOtterioPort), "Unable to start the server")

	if consoleAddr := strings.TrimSpace(globalCLIContext.ConsoleAddr); consoleAddr != "" {
		logger.FatalIf(CheckLocalServerAddr(consoleAddr), "Unable to validate --console-address")

		globalOtterioConsoleAddr = consoleAddr
		globalOtterioConsoleHost, globalOtterioConsolePort = mustSplitHostPort(globalOtterioConsoleAddr)

		if globalOtterioConsolePort == globalOtterioPort {
			logger.Fatal(errors.New("--console-address must use a port different from --address"),
				"Unable to start the server with the same port for S3 and console")
		}

		logger.FatalIf(checkPortAvailability(globalOtterioConsoleHost, globalOtterioConsolePort),
			"Unable to start the console listener")
	}

	globalIsErasure = (setupType == ErasureSetupType)
	globalIsDistErasure = (setupType == DistErasureSetupType)
	if globalIsDistErasure {
		globalIsErasure = true
	}
}

func serverHandleEnvVars() {
	// Handle common environment variables.
	handleCommonEnvVars()
}

var globalHealStateLK sync.RWMutex

func newAllSubsystems() {
	if globalIsErasure {
		globalHealStateLK.Lock()
		// New global heal state
		globalAllHealState = newHealState(GlobalContext, true)
		globalBackgroundHealState = newHealState(GlobalContext, false)
		globalHealStateLK.Unlock()
	}

	// Tear down the previous notification system (if any) before replacing it,
	// otherwise the targetResCh consumer goroutine would leak across test runs.
	if globalNotificationSys != nil {
		globalNotificationSys.Shutdown(GlobalContext)
	}

	// Create new notification system and initialize notification targets
	globalNotificationSys = NewNotificationSys(globalEndpoints)

	// Create new bucket metadata system.
	if globalBucketMetadataSys == nil {
		globalBucketMetadataSys = NewBucketMetadataSys()
	} else {
		// Reinitialize safely when testing.
		globalBucketMetadataSys.Reset()
	}

	// Stop the previous bucket bandwidth monitor (if any) before replacing it,
	// to avoid leaking trackEWMA goroutines across repeated test setups.
	if globalBucketMonitor != nil {
		globalBucketMonitor.Stop()
	}

	// Create the bucket bandwidth monitor
	globalBucketMonitor = bandwidth.NewMonitor(GlobalContext, GlobalServiceDoneCh)

	// Create a new config system.
	globalConfigSys = NewConfigSys()

	// Create new IAM system.
	globalIAMSys = NewIAMSys()

	// Create new policy system.
	globalPolicySys = NewPolicySys()

	// Create new lifecycle system.
	globalLifecycleSys = NewLifecycleSys()

	// Create new bucket encryption subsystem
	globalBucketSSEConfigSys = NewBucketSSEConfigSys()

	// Create new bucket object lock subsystem
	globalBucketObjectLockSys = NewBucketObjectLockSys()

	// Create new bucket quota subsystem
	globalBucketQuotaSys = NewBucketQuotaSys()

	// Create new bucket versioning subsystem
	if globalBucketVersioningSys == nil {
		globalBucketVersioningSys = NewBucketVersioningSys()
	} else {
		globalBucketVersioningSys.Reset()
	}

	// Create new bucket replication subsytem
	globalBucketTargetSys = NewBucketTargetSys()
}

func configRetriableErrors(err error) bool {
	// Initializing sub-systems needs a retry mechanism for
	// the following reasons:
	//  - Read quorum is lost just after the initialization
	//    of the object layer.
	//  - Write quorum not met when upgrading configuration
	//    version is needed, migration is needed etc.
	rquorum := InsufficientReadQuorum{}
	wquorum := InsufficientWriteQuorum{}

	// One of these retriable errors shall be retried.
	return errors.Is(err, errDiskNotFound) ||
		errors.Is(err, errConfigNotFound) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errErasureWriteQuorum) ||
		errors.Is(err, errErasureReadQuorum) ||
		errors.As(err, &rquorum) ||
		errors.As(err, &wquorum) ||
		isErrBucketNotFound(err) ||
		errors.Is(err, os.ErrDeadlineExceeded)
}

func initServer(ctx context.Context, newObject ObjectLayer) error {
	// Once the config is fully loaded, initialize the new object layer.
	setObjectLayer(newObject)

	// Make sure to hold lock for entire migration to avoid
	// such that only one server should migrate the entire config
	// at a given time, this big transaction lock ensures this
	// appropriately. This is also true for rotation of encrypted
	// content.
	txnLk := newObject.NewNSLock(otterioMetaBucket, otterioConfigPrefix+"/transaction.lock")

	// ****  WARNING ****
	// Migrating to encrypted backend should happen before initialization of any
	// sub-systems, make sure that we do not move the above codeblock elsewhere.

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	lockTimeout := newDynamicTimeout(5*time.Second, 3*time.Second)

	var err error
	for {
		select {
		case <-ctx.Done():
			// Retry was canceled successfully.
			return fmt.Errorf("Initializing sub-systems stopped gracefully %w", ctx.Err())
		default:
		}

		// let one of the server acquire the lock, if not let them timeout.
		// which shall be retried again by this loop.
		if _, err = txnLk.GetLock(ctx, lockTimeout); err != nil {
			logger.Info("Waiting for all OtterIO sub-systems to be initialized.. trying to acquire lock")

			time.Sleep(time.Duration(r.Float64() * float64(5*time.Second)))
			continue
		}

		// These messages only meant primarily for distributed setup, so only log during distributed setup.
		if globalIsDistErasure {
			logger.Info("Waiting for all OtterIO sub-systems to be initialized.. lock acquired")
		}

		// Migrate all backend configs to encrypted backend configs, optionally
		// handles rotating keys for encryption, if there is any retriable failure
		// that shall be retried if there is an error.
		if err = handleEncryptedConfigBackend(newObject); err == nil {
			// Upon success migrating the config, initialize all sub-systems
			// if all sub-systems initialized successfully return right away
			if err = initAllSubsystems(ctx, newObject); err == nil {
				txnLk.Unlock()
				// All successful return.
				if globalIsDistErasure {
					// These messages only meant primarily for distributed setup, so only log during distributed setup.
					logger.Info("All OtterIO sub-systems initialized successfully")
				}
				return nil
			}
		}

		txnLk.Unlock() // Unlock the transaction lock and allow other nodes to acquire the lock if possible.

		if configRetriableErrors(err) {
			logger.Info("Waiting for all OtterIO sub-systems to be initialized.. possible cause (%v)", err)
			time.Sleep(time.Duration(r.Float64() * float64(5*time.Second)))
			continue
		}

		// Any other unhandled return right here.
		return fmt.Errorf("Unable to initialize sub-systems: %w", err)
	}
}

func initAllSubsystems(ctx context.Context, newObject ObjectLayer) (err error) {
	// %w is used by all error returns here to make sure
	// we wrap the underlying error, make sure when you
	// are modifying this code that you do so, if and when
	// you want to add extra context to your error. This
	// ensures top level retry works accordingly.
	// List buckets to heal, and be re-used for loading configs.

	buckets, err := newObject.ListBuckets(ctx)
	if err != nil {
		return fmt.Errorf("Unable to list buckets to heal: %w", err)
	}

	if globalIsErasure {
		if len(buckets) > 0 {
			if len(buckets) == 1 {
				logger.Info(fmt.Sprintf("Verifying if %d bucket is consistent across drives...", len(buckets)))
			} else {
				logger.Info(fmt.Sprintf("Verifying if %d buckets are consistent across drives...", len(buckets)))
			}
		}

		// Limit to no more than 50 concurrent buckets.
		g := errgroup.WithNErrs(len(buckets)).WithConcurrency(50)
		ctx, cancel := g.WithCancelOnError(ctx)
		defer cancel()
		for index := range buckets {
			index := index
			g.Go(func() error {
				_, berr := newObject.HealBucket(ctx, buckets[index].Name, madmin.HealOpts{Recreate: true})
				return berr
			}, index)
		}
		if err := g.WaitErr(); err != nil {
			return fmt.Errorf("Unable to list buckets to heal: %w", err)
		}
	}

	// Initialize config system.
	if err = globalConfigSys.Init(newObject); err != nil {
		if configRetriableErrors(err) {
			return fmt.Errorf("Unable to initialize config system: %w", err)
		}
		// Any other config errors we simply print a message and proceed forward.
		logger.LogIf(ctx, fmt.Errorf("Unable to initialize config, some features may be missing %w", err))
	}

	// Populate existing buckets to the etcd backend
	if globalDNSConfig != nil {
		// Background this operation.
		go initFederatorBackend(buckets, newObject)
	}

	// Initialize bucket metadata sub-system.
	globalBucketMetadataSys.Init(ctx, buckets, newObject)

	// Initialize notification system.
	globalNotificationSys.Init(ctx, buckets, newObject)

	// Initialize bucket targets sub-system.
	globalBucketTargetSys.Init(ctx, buckets, newObject)

	return nil
}

// serverMain handler called for 'otterio server' command.
func serverMain(ctx *cli.Context) {
	defer globalDNSCache.Stop()

	signal.Notify(globalOSSignalCh, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)

	go handleSignals()

	setDefaultProfilerRates()

	// Initialize globalConsoleSys system
	globalConsoleSys = NewConsoleLogger(GlobalContext)
	logger.AddTarget(globalConsoleSys)

	// Perform any self-tests
	bitrotSelfTest()
	erasureSelfTest()
	compressSelfTest()

	// Handle all server command args.
	serverHandleCmdArgs(ctx)

	// Handle all server environment vars.
	serverHandleEnvVars()

	// Set node name, only set for distributed setup.
	globalConsoleSys.SetNodeName(globalLocalNodeName)

	// Initialize all help
	initHelp()

	// Initialize all sub-systems
	newAllSubsystems()

	globalOtterioEndpoint = func() string {
		host := globalOtterioHost
		if host == "" {
			host = sortIPs(localIP4.ToSlice())[0]
		}
		return fmt.Sprintf("%s://%s", getURLScheme(globalIsTLS), net.JoinHostPort(host, globalOtterioPort))
	}()

	// Is distributed setup, error out if no certificates are found for HTTPS endpoints.
	if globalIsDistErasure {
		if globalEndpoints.HTTPS() && !globalIsTLS {
			logger.Fatal(config.ErrNoCertsAndHTTPSEndpoints(nil), "Unable to start the server")
		}
		if !globalEndpoints.HTTPS() && globalIsTLS {
			logger.Fatal(config.ErrCertsAndHTTPEndpoints(nil), "Unable to start the server")
		}
	}

	if !globalActiveCred.IsValid() && globalIsDistErasure {
		logger.Fatal(config.ErrEnvCredentialsMissingDistributed(nil),
			"Unable to initialize the server in distributed mode")
	}

	// Set system resources to maximum.
	setMaxResources()

	// Configure server.
	s3App, consoleApp, err := configureServerHandlers(globalEndpoints)
	if err != nil {
		logger.Fatal(config.ErrUnexpectedError(err), "Unable to configure one of server's RPC services")
	}

	var getCert certs.GetCertificateFunc
	if globalTLSCerts != nil {
		getCert = globalTLSCerts.GetCertificate
	}

	httpServer := xhttp.NewServer([]string{globalOtterioAddr}, s3App, getCert)
	httpServer.BaseContext = func(_ net.Listener) context.Context {
		return GlobalContext
	}
	go func() {
		globalHTTPServerErrorCh <- httpServer.Start()
	}()

	setHTTPServer(httpServer)

	if consoleApp != nil {
		consoleScheme := getURLScheme(globalIsTLS)
		consoleGetCert := getCert
		if globalConsoleTLSCerts != nil {
			consoleGetCert = globalConsoleTLSCerts.GetCertificate
			consoleScheme = getURLScheme(true)
		}
		globalOtterioConsoleEndpoint = fmt.Sprintf("%s://%s",
			consoleScheme,
			net.JoinHostPort(func() string {
				if globalOtterioConsoleHost == "" {
					return sortIPs(localIP4.ToSlice())[0]
				}
				return globalOtterioConsoleHost
			}(), globalOtterioConsolePort),
		)

		consoleServer := xhttp.NewServer([]string{globalOtterioConsoleAddr}, consoleApp, consoleGetCert)
		consoleServer.BaseContext = func(_ net.Listener) context.Context {
			return GlobalContext
		}
		go func() {
			globalHTTPServerErrorCh <- consoleServer.Start()
		}()

		setConsoleHTTPServer(consoleServer)
	}

	if globalIsDistErasure && globalEndpoints.FirstLocal() {
		for {
			// Additionally in distributed setup, validate the setup and configuration.
			err := verifyServerSystemConfig(GlobalContext, globalEndpoints)
			if err == nil || errors.Is(err, context.Canceled) {
				break
			}
			logger.LogIf(GlobalContext, err, "Unable to initialize distributed setup, retrying.. after 5 seconds")
			select {
			case <-GlobalContext.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}

	newObject, err := newObjectLayer(GlobalContext, globalEndpoints)
	if err != nil {
		logFatalErrs(err, Endpoint{}, true)
	}

	logger.SetDeploymentID(globalDeploymentID)

	// Enable background operations for erasure coding
	if globalIsErasure {
		initAutoHeal(GlobalContext, newObject)
		initBackgroundTransition(GlobalContext, newObject)
	}

	initBackgroundExpiry(GlobalContext, newObject)
	initDataScanner(GlobalContext, newObject)

	if err = initServer(GlobalContext, newObject); err != nil {
		var cerr config.Err
		// For any config error, we don't need to drop into safe-mode
		// instead its a user error and should be fixed by user.
		if errors.As(err, &cerr) {
			logger.FatalIf(err, "Unable to initialize the server")
		}

		// If context was canceled
		if errors.Is(err, context.Canceled) {
			logger.FatalIf(err, "Server startup canceled upon user request")
		}
	}

	if globalIsErasure { // to be done after config init
		initBackgroundReplication(GlobalContext, newObject)
	}
	if globalCacheConfig.Enabled {
		// initialize the new disk cache objects.
		var cacheAPI CacheObjectLayer
		cacheAPI, err = newServerCacheObjects(GlobalContext, globalCacheConfig)
		logger.FatalIf(err, "Unable to initialize disk caching")

		setCacheObjectLayer(cacheAPI)
	}

	// Initialize users credentials and policies in background right after config has initialized.
	go globalIAMSys.Init(GlobalContext, newObject)

	// Prints the formatted startup message, if err is not nil then it prints additional information as well.
	printStartupMessage(getAPIEndpoints(), err)

	if globalActiveCred.Equal(auth.DefaultCredentials) {
		msg := fmt.Sprintf("Detected default credentials '%s', please change the credentials immediately using 'OTTERIO_ROOT_USER' and 'OTTERIO_ROOT_PASSWORD'", globalActiveCred)
		logger.StartupMessage(color.RedBold(msg))
	}

	<-globalOSSignalCh
}

// Initialize object layer with the supplied disks, objectLayer is nil upon any error.
func newObjectLayer(ctx context.Context, endpointServerPools EndpointServerPools) (newObject ObjectLayer, err error) {
	// For FS only, directly use the disk.
	if endpointServerPools.NEndpoints() == 1 {
		// Initialize new FS object layer.
		return NewFSObjectLayer(ctx, endpointServerPools[0].Endpoints[0].Path)
	}

	return newErasureServerPools(ctx, endpointServerPools)
}
