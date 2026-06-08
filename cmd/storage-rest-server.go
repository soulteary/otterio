/*
 * MinIO Cloud Storage, (C) 2018 MinIO, Inc.
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
	"encoding/binary"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/tinylib/msgp/msgp"

	jwtreq "github.com/dgrijalva/jwt-go/request"
	"github.com/soulteary/otterio/cmd/config"
	xhttp "github.com/soulteary/otterio/cmd/http"
	xjwt "github.com/soulteary/otterio/cmd/jwt"
	"github.com/soulteary/otterio/cmd/logger"
	xnet "github.com/soulteary/otterio/pkg/net"
)

var errDiskStale = errors.New("disk stale")

// To abstract a disk over network.
type storageRESTServer struct {
	storage *xlStorage
}

func (s *storageRESTServer) writeErrorResponse(w http.ResponseWriter, err error) {
	if errors.Is(err, errDiskStale) {
		w.WriteHeader(http.StatusPreconditionFailed)
	} else {
		w.WriteHeader(http.StatusForbidden)
	}
	w.Write([]byte(err.Error()))
	w.(http.Flusher).Flush()
}

// DefaultSkewTime - skew time is 15 minutes between otterio peers.
const DefaultSkewTime = 15 * time.Minute

// Authenticates storage client's requests and validates for skewed time.
func storageServerRequestValidate(r *http.Request) error {
	token, err := jwtreq.AuthorizationHeaderExtractor.ExtractToken(r)
	if err != nil {
		if err == jwtreq.ErrNoTokenInRequest {
			return errNoAuthToken
		}
		return err
	}

	claims := xjwt.NewStandardClaims()
	if err = xjwt.ParseWithStandardClaims(token, claims, []byte(globalActiveCred.SecretKey)); err != nil {
		return errAuthentication
	}

	owner := claims.AccessKey == globalActiveCred.AccessKey || claims.Subject == globalActiveCred.AccessKey
	if !owner {
		return errAuthentication
	}

	if claims.Audience != r.URL.RawQuery {
		return errAuthentication
	}

	requestTimeStr := r.Header.Get("X-Otterio-Time")
	requestTime, err := time.Parse(time.RFC3339, requestTimeStr)
	if err != nil {
		return err
	}
	utcNow := UTCNow()
	delta := requestTime.Sub(utcNow)
	if delta < 0 {
		delta *= -1
	}
	if delta > DefaultSkewTime {
		return fmt.Errorf("client time %v is too apart with server time %v", requestTime, utcNow)
	}

	return nil
}

// IsValid - To authenticate and verify the time difference.
func (s *storageRESTServer) IsValid(w http.ResponseWriter, r *http.Request) bool {
	if s.storage == nil {
		s.writeErrorResponse(w, errDiskNotFound)
		return false
	}

	if err := storageServerRequestValidate(r); err != nil {
		s.writeErrorResponse(w, err)
		return false
	}

	diskID := r.URL.Query().Get(storageRESTDiskID)
	if diskID == "" {
		// Request sent empty disk-id, we allow the request
		// as the peer might be coming up and trying to read format.json
		// or create format.json
		return true
	}

	storedDiskID, err := s.storage.GetDiskID()
	if err != nil {
		s.writeErrorResponse(w, err)
		return false
	}

	if diskID != storedDiskID {
		s.writeErrorResponse(w, errDiskStale)
		return false
	}

	// If format.json is available and request sent the right disk-id, we allow the request
	return true
}

// HealthHandler handler checks if disk is stale
func (s *storageRESTServer) HealthHandler(w http.ResponseWriter, r *http.Request) {
	s.IsValid(w, r)
}

// DiskInfoHandler - returns disk info.
func (s *storageRESTServer) DiskInfoHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	info, err := s.storage.DiskInfo(r.Context())
	if err != nil {
		info.Error = err.Error()
	}
	defer w.(http.Flusher).Flush()
	logger.LogIf(r.Context(), msgp.Encode(w, &info))
}

func (s *storageRESTServer) NSScannerHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}

	setEventStreamHeaders(w)

	var cache dataUsageCache
	err := cache.deserialize(r.Body)
	if err != nil {
		logger.LogIf(r.Context(), err)
		s.writeErrorResponse(w, err)
		return
	}

	resp := streamHTTPResponse(w)
	usageInfo, err := s.storage.NSScanner(r.Context(), cache)
	if err != nil {
		resp.CloseWithError(err)
		return
	}
	resp.CloseWithError(usageInfo.serializeTo(resp))
}

// MakeVolHandler - make a volume.
func (s *storageRESTServer) MakeVolHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	err := s.storage.MakeVol(r.Context(), volume)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// MakeVolBulkHandler - create multiple volumes as a bulk operation.
func (s *storageRESTServer) MakeVolBulkHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volumes := strings.Split(r.URL.Query().Get(storageRESTVolumes), ",")
	err := s.storage.MakeVolBulk(r.Context(), volumes...)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// ListVolsHandler - list volumes.
func (s *storageRESTServer) ListVolsHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	infos, err := s.storage.ListVols(r.Context())
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	defer w.(http.Flusher).Flush()
	logger.LogIf(r.Context(), msgp.Encode(w, VolsInfo(infos)))
}

// StatVolHandler - stat a volume.
func (s *storageRESTServer) StatVolHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	info, err := s.storage.StatVol(r.Context(), volume)
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	defer w.(http.Flusher).Flush()
	logger.LogIf(r.Context(), msgp.Encode(w, &info))
}

// DeleteVolumeHandler - delete a volume.
func (s *storageRESTServer) DeleteVolHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	forceDelete := r.URL.Query().Get(storageRESTForceDelete) == "true"
	err := s.storage.DeleteVol(r.Context(), volume, forceDelete)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// AppendFileHandler - append data from the request to the file specified.
func (s *storageRESTServer) AppendFileHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)

	buf := make([]byte, r.ContentLength)
	_, err := io.ReadFull(r.Body, buf)
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	err = s.storage.AppendFile(r.Context(), volume, filePath, buf)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// CreateFileHandler - fallocate() space for a file and copy the contents from the request.
func (s *storageRESTServer) CreateFileHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)

	fileSizeStr := r.URL.Query().Get(storageRESTLength)
	fileSize, err := strconv.Atoi(fileSizeStr)
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	done := keepHTTPResponseAlive(w)
	done(s.storage.CreateFile(r.Context(), volume, filePath, int64(fileSize), r.Body))
}

// DeleteVersion delete updated metadata.
func (s *storageRESTServer) DeleteVersionHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)
	forceDelMarker, err := strconv.ParseBool(r.URL.Query().Get(storageRESTForceDelMarker))
	if err != nil {
		s.writeErrorResponse(w, errInvalidArgument)
		return
	}

	if r.ContentLength < 0 {
		s.writeErrorResponse(w, errInvalidArgument)
		return
	}

	var fi FileInfo
	if err := msgp.Decode(r.Body, &fi); err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	err = s.storage.DeleteVersion(r.Context(), volume, filePath, fi, forceDelMarker)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// ReadVersion read metadata of versionID
func (s *storageRESTServer) ReadVersionHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)
	versionID := r.URL.Query().Get(storageRESTVersionID)
	readData, err := strconv.ParseBool(r.URL.Query().Get(storageRESTReadData))
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	fi, err := s.storage.ReadVersion(r.Context(), volume, filePath, versionID, readData)
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	logger.LogIf(r.Context(), msgp.Encode(w, &fi))
}

// WriteMetadata write new updated metadata.
func (s *storageRESTServer) WriteMetadataHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)

	if r.ContentLength < 0 {
		s.writeErrorResponse(w, errInvalidArgument)
		return
	}

	var fi FileInfo
	if err := msgp.Decode(r.Body, &fi); err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	err := s.storage.WriteMetadata(r.Context(), volume, filePath, fi)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// UpdateMetadata update new updated metadata.
func (s *storageRESTServer) UpdateMetadataHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)

	if r.ContentLength < 0 {
		s.writeErrorResponse(w, errInvalidArgument)
		return
	}

	var fi FileInfo
	if err := msgp.Decode(r.Body, &fi); err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	err := s.storage.UpdateMetadata(r.Context(), volume, filePath, fi)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// WriteAllHandler - write to file all content.
func (s *storageRESTServer) WriteAllHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)

	if r.ContentLength < 0 {
		s.writeErrorResponse(w, errInvalidArgument)
		return
	}
	tmp := make([]byte, r.ContentLength)
	_, err := io.ReadFull(r.Body, tmp)
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	err = s.storage.WriteAll(r.Context(), volume, filePath, tmp)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// CheckPartsHandler - check if a file metadata exists.
func (s *storageRESTServer) CheckPartsHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)

	if r.ContentLength < 0 {
		s.writeErrorResponse(w, errInvalidArgument)
		return
	}

	var fi FileInfo
	if err := msgp.Decode(r.Body, &fi); err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	if err := s.storage.CheckParts(r.Context(), volume, filePath, fi); err != nil {
		s.writeErrorResponse(w, err)
	}
}

// CheckFileHandler - check if a file metadata exists.
func (s *storageRESTServer) CheckFileHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)

	if err := s.storage.CheckFile(r.Context(), volume, filePath); err != nil {
		s.writeErrorResponse(w, err)
	}
}

// ReadAllHandler - read all the contents of a file.
func (s *storageRESTServer) ReadAllHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)

	buf, err := s.storage.ReadAll(r.Context(), volume, filePath)
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	w.Header().Set(xhttp.ContentLength, strconv.Itoa(len(buf)))
	w.Write(buf)
	w.(http.Flusher).Flush()
}

// ReadFileHandler - read section of a file.
func (s *storageRESTServer) ReadFileHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)
	offset, err := strconv.Atoi(r.URL.Query().Get(storageRESTOffset))
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	length, err := strconv.Atoi(r.URL.Query().Get(storageRESTLength))
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	if offset < 0 || length < 0 {
		s.writeErrorResponse(w, errInvalidArgument)
		return
	}
	var verifier *BitrotVerifier
	if r.URL.Query().Get(storageRESTBitrotAlgo) != "" {
		hashStr := r.URL.Query().Get(storageRESTBitrotHash)
		var hash []byte
		hash, err = hex.DecodeString(hashStr)
		if err != nil {
			s.writeErrorResponse(w, err)
			return
		}
		verifier = NewBitrotVerifier(BitrotAlgorithmFromString(r.URL.Query().Get(storageRESTBitrotAlgo)), hash)
	}
	buf := make([]byte, length)
	_, err = s.storage.ReadFile(r.Context(), volume, filePath, int64(offset), buf, verifier)
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	w.Header().Set(xhttp.ContentLength, strconv.Itoa(len(buf)))
	w.Write(buf)
	w.(http.Flusher).Flush()
}

// ReadFileHandler - read section of a file.
func (s *storageRESTServer) ReadFileStreamHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)
	offset, err := strconv.Atoi(r.URL.Query().Get(storageRESTOffset))
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	length, err := strconv.Atoi(r.URL.Query().Get(storageRESTLength))
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	rc, err := s.storage.ReadFileStream(r.Context(), volume, filePath, int64(offset), int64(length))
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	defer rc.Close()

	w.Header().Set(xhttp.ContentLength, strconv.Itoa(length))
	if _, err = io.Copy(w, rc); err != nil {
		if !xnet.IsNetworkOrHostDown(err, true) { // do not need to log disconnected clients
			logger.LogIf(r.Context(), err)
		}
		return
	}
	w.(http.Flusher).Flush()
}

// ListDirHandler - list a directory.
func (s *storageRESTServer) ListDirHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	dirPath := r.URL.Query().Get(storageRESTDirPath)
	count, err := strconv.Atoi(r.URL.Query().Get(storageRESTCount))
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	entries, err := s.storage.ListDir(r.Context(), volume, dirPath, count)
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	gob.NewEncoder(w).Encode(&entries)
	w.(http.Flusher).Flush()
}

// DeleteFileHandler - delete a file.
func (s *storageRESTServer) DeleteFileHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)
	recursive, err := strconv.ParseBool(r.URL.Query().Get(storageRESTRecursive))
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	err = s.storage.Delete(r.Context(), volume, filePath, recursive)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// DeleteVersionsErrsResp - collection of delete errors
// for bulk version deletes
type DeleteVersionsErrsResp struct {
	Errs []error
}

// DeleteVersionsHandler - delete a set of a versions.
func (s *storageRESTServer) DeleteVersionsHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}

	vars := r.URL.Query()
	volume := vars.Get(storageRESTVolume)

	totalVersions, err := strconv.Atoi(vars.Get(storageRESTTotalVersions))
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	versions := make([]FileInfo, totalVersions)
	decoder := msgp.NewReader(r.Body)
	for i := 0; i < totalVersions; i++ {
		dst := &versions[i]
		if err := dst.DecodeMsg(decoder); err != nil {
			s.writeErrorResponse(w, err)
			return
		}
	}

	dErrsResp := &DeleteVersionsErrsResp{Errs: make([]error, totalVersions)}

	setEventStreamHeaders(w)
	encoder := gob.NewEncoder(w)
	done := keepHTTPResponseAlive(w)
	errs := s.storage.DeleteVersions(r.Context(), volume, versions)
	done(nil)
	for idx := range versions {
		if errs[idx] != nil {
			dErrsResp.Errs[idx] = StorageErr(errs[idx].Error())
		}
	}
	encoder.Encode(dErrsResp)
	w.(http.Flusher).Flush()
}

// RenameDataHandler - renames a meta object and data dir to destination.
func (s *storageRESTServer) RenameDataHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}

	srcVolume := r.URL.Query().Get(storageRESTSrcVolume)
	srcFilePath := r.URL.Query().Get(storageRESTSrcPath)
	dstVolume := r.URL.Query().Get(storageRESTDstVolume)
	dstFilePath := r.URL.Query().Get(storageRESTDstPath)

	if r.ContentLength < 0 {
		s.writeErrorResponse(w, errInvalidArgument)
		return
	}

	var fi FileInfo
	if err := msgp.Decode(r.Body, &fi); err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	err := s.storage.RenameData(r.Context(), srcVolume, srcFilePath, fi, dstVolume, dstFilePath)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// RenameFileHandler - rename a file.
func (s *storageRESTServer) RenameFileHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	srcVolume := r.URL.Query().Get(storageRESTSrcVolume)
	srcFilePath := r.URL.Query().Get(storageRESTSrcPath)
	dstVolume := r.URL.Query().Get(storageRESTDstVolume)
	dstFilePath := r.URL.Query().Get(storageRESTDstPath)
	err := s.storage.RenameFile(r.Context(), srcVolume, srcFilePath, dstVolume, dstFilePath)
	if err != nil {
		s.writeErrorResponse(w, err)
	}
}

// keepHTTPResponseAlive can be used to avoid timeouts with long storage
// operations, such as bitrot verification or data usage scanning.
// Every 10 seconds a space character is sent.
// The returned function should always be called to release resources.
// An optional error can be sent which will be picked as text only error,
// without its original type by the receiver.
// waitForHTTPResponse should be used to the receiving side.
func keepHTTPResponseAlive(w http.ResponseWriter) func(error) {
	doneCh := make(chan error)
	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(time.Second * 10)
		for {
			select {
			case <-ticker.C:
				// Response not ready, write a filler byte.
				w.Write([]byte{32})
				w.(http.Flusher).Flush()
			case err := <-doneCh:
				if err != nil {
					w.Write([]byte{1})
					w.Write([]byte(err.Error()))
				} else {
					w.Write([]byte{0})
				}
				ticker.Stop()
				return
			}
		}
	}()
	return func(err error) {
		if doneCh == nil {
			return
		}
		// Indicate we are ready to write.
		doneCh <- err

		// Wait for channel to be closed so we don't race on writes.
		<-doneCh

		// Clear so we can be called multiple times without crashing.
		doneCh = nil
	}
}

// waitForHTTPResponse will wait for responses where keepHTTPResponseAlive
// has been used.
// The returned reader contains the payload.
func waitForHTTPResponse(respBody io.Reader) (io.Reader, error) {
	reader := bufio.NewReader(respBody)
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		// Check if we have a response ready or a filler byte.
		switch b {
		case 0:
			return reader, nil
		case 1:
			errorText, err := io.ReadAll(reader)
			if err != nil {
				return nil, err
			}
			return nil, errors.New(string(errorText))
		case 32:
			continue
		default:
			return nil, fmt.Errorf("unexpected filler byte: %d", b)
		}
	}
}

// drainCloser can be used for wrapping an http response.
// It will drain the body before closing.
//
//nolint:unused
type drainCloser struct {
	rc io.ReadCloser
}

// Read forwards the read operation.
//
//nolint:unused
func (f drainCloser) Read(p []byte) (n int, err error) {
	return f.rc.Read(p)
}

// Close drains the body and closes the upstream.
//
//nolint:unused
func (f drainCloser) Close() error {
	xhttp.DrainBody(f.rc)
	return nil
}

// httpStreamResponse allows streaming a response, but still send an error.
type httpStreamResponse struct {
	done  chan error
	block chan []byte
	err   error
}

// Write part of the the streaming response.
// Note that upstream errors are currently not forwarded, but may be in the future.
func (h *httpStreamResponse) Write(b []byte) (int, error) {
	if len(b) == 0 || h.err != nil {
		// Ignore 0 length blocks
		return 0, h.err
	}
	tmp := make([]byte, len(b))
	copy(tmp, b)
	h.block <- tmp
	return len(b), h.err
}

// CloseWithError will close the stream and return the specified error.
// This can be done several times, but only the first error will be sent.
// After calling this the stream should not be written to.
func (h *httpStreamResponse) CloseWithError(err error) {
	if h.done == nil {
		return
	}
	h.done <- err
	h.err = err
	// Indicates that the response is done.
	<-h.done
	h.done = nil
}

// streamHTTPResponse can be used to avoid timeouts with long storage
// operations, such as bitrot verification or data usage scanning.
// Every 10 seconds a space character is sent.
// The returned function should always be called to release resources.
// An optional error can be sent which will be picked as text only error,
// without its original type by the receiver.
// waitForHTTPStream should be used to the receiving side.
func streamHTTPResponse(w http.ResponseWriter) *httpStreamResponse {
	doneCh := make(chan error)
	blockCh := make(chan []byte)
	h := httpStreamResponse{done: doneCh, block: blockCh}
	go func() {
		ticker := time.NewTicker(time.Second * 10)
		for {
			select {
			case <-ticker.C:
				// Response not ready, write a filler byte.
				w.Write([]byte{32})
				w.(http.Flusher).Flush()
			case err := <-doneCh:
				ticker.Stop()
				defer close(doneCh)
				if err != nil {
					w.Write([]byte{1})
					w.Write([]byte(err.Error()))
				} else {
					w.Write([]byte{0})
				}
				return
			case block := <-blockCh:
				var tmp [5]byte
				tmp[0] = 2
				binary.LittleEndian.PutUint32(tmp[1:], uint32(len(block)))
				w.Write(tmp[:])
				w.Write(block)
				w.(http.Flusher).Flush()
			}
		}
	}()
	return &h
}

// waitForHTTPStream will wait for responses where
// streamHTTPResponse has been used.
// The returned reader contains the payload and must be closed if no error is returned.
func waitForHTTPStream(respBody io.ReadCloser, w io.Writer) error {
	var tmp [1]byte
	for {
		_, err := io.ReadFull(respBody, tmp[:])
		if err != nil {
			return err
		}
		// Check if we have a response ready or a filler byte.
		switch tmp[0] {
		case 0:
			// 0 is unbuffered, copy the rest.
			_, err := io.Copy(w, respBody)
			respBody.Close()
			if err == io.EOF {
				return nil
			}
			return err
		case 1:
			errorText, err := io.ReadAll(respBody)
			if err != nil {
				return err
			}
			respBody.Close()
			return errors.New(string(errorText))
		case 3:
			// gob style is already deprecated, we can remove this when
			// storage API version will be greater or equal to 23.
			defer respBody.Close()
			dec := gob.NewDecoder(respBody)
			var err error
			if de := dec.Decode(&err); de == nil {
				return err
			}
			return errors.New("rpc error")
		case 2:
			// Block of data
			var tmp [4]byte
			_, err := io.ReadFull(respBody, tmp[:])
			if err != nil {
				return err
			}
			length := binary.LittleEndian.Uint32(tmp[:])
			_, err = io.CopyN(w, respBody, int64(length))
			if err != nil {
				return err
			}
			continue
		case 32:
			continue
		default:
			go xhttp.DrainBody(respBody)
			return fmt.Errorf("unexpected filler byte: %d", tmp[0])
		}
	}
}

// VerifyFileResp - VerifyFile()'s response.
type VerifyFileResp struct {
	Err error
}

// VerifyFileHandler - Verify all part of file for bitrot errors.
func (s *storageRESTServer) VerifyFileHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	volume := r.URL.Query().Get(storageRESTVolume)
	filePath := r.URL.Query().Get(storageRESTFilePath)

	if r.ContentLength < 0 {
		s.writeErrorResponse(w, errInvalidArgument)
		return
	}

	var fi FileInfo
	if err := msgp.Decode(r.Body, &fi); err != nil {
		s.writeErrorResponse(w, err)
		return
	}

	setEventStreamHeaders(w)
	encoder := gob.NewEncoder(w)
	done := keepHTTPResponseAlive(w)
	err := s.storage.VerifyFile(r.Context(), volume, filePath, fi)
	done(nil)
	vresp := &VerifyFileResp{}
	if err != nil {
		vresp.Err = StorageErr(err.Error())
	}
	encoder.Encode(vresp)
	w.(http.Flusher).Flush()
}

// A single function to write certain errors to be fatal
// or informative based on the `exit` flag, please look
// at each implementation of error for added hints.
//
// FIXME: This is an unusual function but serves its purpose for
// now, need to revisit the overall erroring structure here.
// Do not like it :-(
func logFatalErrs(err error, endpoint Endpoint, exit bool) {
	if errors.Is(err, errMinDiskSize) {
		logger.Fatal(config.ErrUnableToWriteInBackend(err).Hint("%s", err.Error()), "Unable to initialize backend")
	} else if errors.Is(err, errUnsupportedDisk) {
		var hint string
		if endpoint.URL != nil {
			hint = fmt.Sprintf("Disk '%s' does not support O_DIRECT flags, OtterIO erasure coding requires filesystems with O_DIRECT support", endpoint.Path)
		} else {
			hint = "Disks do not support O_DIRECT flags, OtterIO erasure coding requires filesystems with O_DIRECT support"
		}
		logger.Fatal(config.ErrUnsupportedBackend(err).Hint("%s", hint), "Unable to initialize backend")
	} else if errors.Is(err, errDiskNotDir) {
		var hint string
		if endpoint.URL != nil {
			hint = fmt.Sprintf("Disk '%s' is not a directory, OtterIO erasure coding needs a directory", endpoint.Path)
		} else {
			hint = "Disks are not directories, OtterIO erasure coding needs directories"
		}
		logger.Fatal(config.ErrUnableToWriteInBackend(err).Hint("%s", hint), "Unable to initialize backend")
	} else if errors.Is(err, errFileAccessDenied) {
		// Show a descriptive error with a hint about how to fix it.
		var username string
		if u, err := user.Current(); err == nil {
			username = u.Username
		} else {
			username = "<your-username>"
		}
		var hint string
		if endpoint.URL != nil {
			hint = fmt.Sprintf("Run the following command to add write permissions: `sudo chown -R %s %s && sudo chmod u+rxw %s`",
				username, endpoint.Path, endpoint.Path)
		} else {
			hint = fmt.Sprintf("Run the following command to add write permissions: `sudo chown -R %s. <path> && sudo chmod u+rxw <path>`", username)
		}
		logger.Fatal(config.ErrUnableToWriteInBackend(err).Hint("%s", hint), "Unable to initialize backend")
	} else if errors.Is(err, errFaultyDisk) {
		if !exit {
			logger.LogIf(GlobalContext, fmt.Errorf("disk is faulty at %s, please replace the drive - disk will be offline", endpoint))
		} else {
			logger.Fatal(err, "Unable to initialize backend")
		}
	} else if errors.Is(err, errDiskFull) {
		if !exit {
			logger.LogIf(GlobalContext, fmt.Errorf("disk is already full at %s, incoming I/O will fail - disk will be offline", endpoint))
		} else {
			logger.Fatal(err, "Unable to initialize backend")
		}
	} else {
		if !exit {
			logger.LogIf(GlobalContext, fmt.Errorf("disk returned an unexpected error at %s, please investigate - disk will be offline", endpoint))
		} else {
			logger.Fatal(err, "Unable to initialize backend")
		}
	}
}
