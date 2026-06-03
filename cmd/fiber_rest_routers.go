/*
 * MinIO Cloud Storage, (C) 2018-2019 MinIO, Inc.
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
	"net/http"
	"path"

	"github.com/gofiber/fiber/v3"
)

func registerInternalPOST(app *fiber.App, routePath string, handler MinioHandler, queries map[string]string) {
	rules := []routeRule{{
		methods: []string{http.MethodPost},
		queries: queries,
		handler: handler,
	}}
	app.All(routePath, func(c fiber.Ctx) error {
		matched, err := dispatchInternalRules(c, rules)
		if !matched {
			return methodNotAllowedHandlerFiber("Internal")(c)
		}
		return err
	})
}

func internalHdr(h func(http.ResponseWriter, *http.Request)) MinioHandler {
	return wrapInternalHandler(toMinioHandler(h))
}

func internalAll(h func(http.ResponseWriter, *http.Request)) MinioHandler {
	return httpTraceAllFiber(toMinioHandler(h))
}

func internalRaw(h func(http.ResponseWriter, *http.Request)) MinioHandler {
	return toMinioHandler(h)
}

// internalRawStream is the streaming counterpart of internalRaw. It must be
// used for internal handlers that produce an unbounded (infinite event stream)
// or long-lived / large streamed response: the buffered bridge cannot flush
// mid-handler (responseBodyWriter has no Flush), so such handlers would buffer
// their output in memory, never deliver it incrementally, defeat their
// keepalive, and (for infinite streams) never terminate on peer disconnect.
func internalRawStream(h func(http.ResponseWriter, *http.Request)) MinioHandler {
	return toMinioStreamHandler(h)
}

// internalHdrStream is the streaming counterpart of internalHdr; it preserves
// the header-only trace wrapping (TraceFiber is stream-aware and does not drain
// a SetBodyStream body) while bridging the handler through the streaming path.
func internalHdrStream(h func(http.ResponseWriter, *http.Request)) MinioHandler {
	return httpTraceHdrsFiber(toMinioStreamHandler(h))
}

func registerStorageRESTHandlersFiber(app *fiber.App, endpointServerPools EndpointServerPools) {
	for _, ep := range endpointServerPools {
		for _, endpoint := range ep.Endpoints {
			if !endpoint.IsLocal {
				continue
			}
			storage, err := newXLStorage(endpoint)
			if err != nil {
				logFatalErrs(err, endpoint, false)
			}

			server := &storageRESTServer{storage: storage}
			base := path.Join(storageRESTPrefix, endpoint.Path, storageRESTVersionPrefix)

			registerInternalPOST(app, base+storageRESTMethodHealth, internalHdr(server.HealthHandler), nil)
			registerInternalPOST(app, base+storageRESTMethodDiskInfo, internalHdr(server.DiskInfoHandler), nil)
			registerInternalPOST(app, base+storageRESTMethodNSScanner, internalHdrStream(server.NSScannerHandler), nil)
			registerInternalPOST(app, base+storageRESTMethodMakeVol, internalHdr(server.MakeVolHandler), queryRules(storageRESTVolume))
			registerInternalPOST(app, base+storageRESTMethodMakeVolBulk, internalHdr(server.MakeVolBulkHandler), queryRules(storageRESTVolumes))
			registerInternalPOST(app, base+storageRESTMethodStatVol, internalHdr(server.StatVolHandler), queryRules(storageRESTVolume))
			registerInternalPOST(app, base+storageRESTMethodDeleteVol, internalHdr(server.DeleteVolHandler), queryRules(storageRESTVolume))
			registerInternalPOST(app, base+storageRESTMethodListVols, internalHdr(server.ListVolsHandler), nil)

			registerInternalPOST(app, base+storageRESTMethodAppendFile, internalHdr(server.AppendFileHandler), queryRules(storageRESTVolume, storageRESTFilePath))
			registerInternalPOST(app, base+storageRESTMethodWriteAll, internalHdr(server.WriteAllHandler), queryRules(storageRESTVolume, storageRESTFilePath))
			registerInternalPOST(app, base+storageRESTMethodWriteMetadata, internalHdr(server.WriteMetadataHandler), queryRules(storageRESTVolume, storageRESTFilePath))
			registerInternalPOST(app, base+storageRESTMethodUpdateMetadata, internalHdr(server.UpdateMetadataHandler), queryRules(storageRESTVolume, storageRESTFilePath))
			registerInternalPOST(app, base+storageRESTMethodDeleteVersion, internalHdr(server.DeleteVersionHandler), queryRules(storageRESTVolume, storageRESTFilePath, storageRESTForceDelMarker))
			registerInternalPOST(app, base+storageRESTMethodReadVersion, internalHdr(server.ReadVersionHandler), queryRules(storageRESTVolume, storageRESTFilePath, storageRESTVersionID, storageRESTReadData))
			registerInternalPOST(app, base+storageRESTMethodRenameData, internalHdr(server.RenameDataHandler), queryRules(storageRESTSrcVolume, storageRESTSrcPath, storageRESTDstVolume, storageRESTDstPath))
			registerInternalPOST(app, base+storageRESTMethodCreateFile, internalHdr(server.CreateFileHandler), queryRules(storageRESTVolume, storageRESTFilePath, storageRESTLength))

			registerInternalPOST(app, base+storageRESTMethodCheckFile, internalHdr(server.CheckFileHandler), queryRules(storageRESTVolume, storageRESTFilePath))
			registerInternalPOST(app, base+storageRESTMethodCheckParts, internalHdr(server.CheckPartsHandler), queryRules(storageRESTVolume, storageRESTFilePath))

			registerInternalPOST(app, base+storageRESTMethodReadAll, internalHdr(server.ReadAllHandler), queryRules(storageRESTVolume, storageRESTFilePath))
			registerInternalPOST(app, base+storageRESTMethodReadFile, internalHdr(server.ReadFileHandler), queryRules(storageRESTVolume, storageRESTFilePath, storageRESTOffset, storageRESTLength, storageRESTBitrotAlgo, storageRESTBitrotHash))
			registerInternalPOST(app, base+storageRESTMethodReadFileStream, internalHdrStream(server.ReadFileStreamHandler), queryRules(storageRESTVolume, storageRESTFilePath, storageRESTOffset, storageRESTLength))
			registerInternalPOST(app, base+storageRESTMethodListDir, internalHdr(server.ListDirHandler), queryRules(storageRESTVolume, storageRESTDirPath, storageRESTCount))

			registerInternalPOST(app, base+storageRESTMethodDeleteVersions, internalHdr(server.DeleteVersionsHandler), queryRules(storageRESTVolume, storageRESTTotalVersions))
			registerInternalPOST(app, base+storageRESTMethodDeleteFile, internalHdr(server.DeleteFileHandler), queryRules(storageRESTVolume, storageRESTFilePath, storageRESTRecursive))

			registerInternalPOST(app, base+storageRESTMethodRenameFile, internalHdr(server.RenameFileHandler), queryRules(storageRESTSrcVolume, storageRESTSrcPath, storageRESTDstVolume, storageRESTDstPath))
			registerInternalPOST(app, base+storageRESTMethodVerifyFile, internalHdrStream(server.VerifyFileHandler), queryRules(storageRESTVolume, storageRESTFilePath))
			registerInternalPOST(app, base+storageRESTMethodWalkDir, internalHdrStream(server.WalkDirHandler), queryRules(storageRESTVolume, storageRESTDirPath, storageRESTRecursive))
		}
	}
}

func registerPeerRESTHandlersFiber(app *fiber.App) {
	server := &peerRESTServer{}
	base := peerRESTPrefix + peerRESTVersionPrefix

	registerInternalPOST(app, base+peerRESTMethodHealth, internalHdr(server.HealthHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodGetLocks, internalHdr(server.GetLocksHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodServerInfo, internalHdr(server.ServerInfoHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodProcInfo, internalHdr(server.ProcInfoHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodMemInfo, internalHdr(server.MemInfoHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodOsInfo, internalHdr(server.OsInfoHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodDiskHwInfo, internalHdr(server.DiskHwInfoHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodCPUInfo, internalHdr(server.CPUInfoHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodDriveInfo, internalHdr(server.DriveInfoHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodNetInfo, internalHdr(server.NetInfoHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodDispatchNetInfo, internalHdr(server.DispatchNetInfoHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodCycleBloom, internalHdr(server.CycleServerBloomFilterHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodDeleteBucketMetadata, internalHdr(server.DeleteBucketMetadataHandler), queryRules(peerRESTBucket))
	registerInternalPOST(app, base+peerRESTMethodLoadBucketMetadata, internalHdr(server.LoadBucketMetadataHandler), queryRules(peerRESTBucket))
	registerInternalPOST(app, base+peerRESTMethodGetBucketStats, internalHdr(server.GetBucketStatsHandler), queryRules(peerRESTBucket))
	registerInternalPOST(app, base+peerRESTMethodSignalService, internalHdr(server.SignalServiceHandler), queryRules(peerRESTSignal))
	registerInternalPOST(app, base+peerRESTMethodServerUpdate, internalHdr(server.ServerUpdateHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodDeletePolicy, internalAll(server.DeletePolicyHandler), queryRules(peerRESTPolicy))
	registerInternalPOST(app, base+peerRESTMethodLoadPolicy, internalAll(server.LoadPolicyHandler), queryRules(peerRESTPolicy))
	registerInternalPOST(app, base+peerRESTMethodLoadPolicyMapping, internalAll(server.LoadPolicyMappingHandler), queryRules(peerRESTUserOrGroup))
	registerInternalPOST(app, base+peerRESTMethodDeleteUser, internalAll(server.DeleteUserHandler), queryRules(peerRESTUser))
	registerInternalPOST(app, base+peerRESTMethodDeleteServiceAccount, internalAll(server.DeleteServiceAccountHandler), queryRules(peerRESTUser))
	registerInternalPOST(app, base+peerRESTMethodLoadUser, internalAll(server.LoadUserHandler), queryRules(peerRESTUser, peerRESTUserTemp))
	registerInternalPOST(app, base+peerRESTMethodLoadServiceAccount, internalAll(server.LoadServiceAccountHandler), queryRules(peerRESTUser))
	registerInternalPOST(app, base+peerRESTMethodLoadGroup, internalAll(server.LoadGroupHandler), queryRules(peerRESTGroup))

	registerInternalPOST(app, base+peerRESTMethodStartProfiling, internalAll(server.StartProfilingHandler), queryRules(peerRESTProfiler))
	registerInternalPOST(app, base+peerRESTMethodDownloadProfilingData, internalHdr(server.DownloadProfilingDataHandler), nil)
	// Trace / Listen / Log are unbounded event streams: they must use the
	// streaming bridge or the buffered bridge would never deliver events, never
	// terminate on peer disconnect, and leak a goroutine + memory per request.
	registerInternalPOST(app, base+peerRESTMethodTrace, internalRawStream(server.TraceHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodListen, internalHdrStream(server.ListenHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodBackgroundHealStatus, internalRaw(server.BackgroundHealStatusHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodLog, internalRawStream(server.ConsoleLogHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodGetLocalDiskIDs, internalHdr(server.GetLocalDiskIDs), nil)
	registerInternalPOST(app, base+peerRESTMethodGetBandwidth, internalHdr(server.GetBandwidth), nil)
	registerInternalPOST(app, base+peerRESTMethodGetMetacacheListing, internalHdr(server.GetMetacacheListingHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodUpdateMetacacheListing, internalHdr(server.UpdateMetacacheListingHandler), nil)
	registerInternalPOST(app, base+peerRESTMethodGetPeerMetrics, internalHdr(server.GetPeerMetrics), nil)
}

func registerBootstrapRESTHandlersFiber(app *fiber.App) {
	server := &bootstrapRESTServer{}
	base := bootstrapRESTPrefix + bootstrapRESTVersionPrefix

	registerInternalPOST(app, base+bootstrapRESTMethodHealth, internalHdr(server.HealthHandler), nil)
	registerInternalPOST(app, base+bootstrapRESTMethodVerify, internalHdr(server.VerifyHandler), nil)
}

func registerLockRESTHandlersFiber(app *fiber.App) {
	lockServer := &lockRESTServer{
		ll: newLocker(),
	}

	base := lockRESTPrefix + lockRESTVersionPrefix

	registerInternalPOST(app, base+lockRESTMethodHealth, internalHdr(lockServer.HealthHandler), nil)
	registerInternalPOST(app, base+lockRESTMethodRefresh, internalHdr(lockServer.RefreshHandler), nil)
	registerInternalPOST(app, base+lockRESTMethodLock, internalHdr(lockServer.LockHandler), nil)
	registerInternalPOST(app, base+lockRESTMethodRLock, internalHdr(lockServer.RLockHandler), nil)
	registerInternalPOST(app, base+lockRESTMethodUnlock, internalHdr(lockServer.UnlockHandler), nil)
	registerInternalPOST(app, base+lockRESTMethodRUnlock, internalHdr(lockServer.RUnlockHandler), nil)
	registerInternalPOST(app, base+lockRESTMethodForceUnlock, internalAll(lockServer.ForceUnlockHandler), nil)

	globalLockServer = lockServer.ll

	go lockMaintenance(GlobalContext)
}
