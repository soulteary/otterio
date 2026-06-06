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
	"context"
	"io"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/soulteary/otterio/cmd/config"
	xnet "github.com/soulteary/otterio/pkg/net"
)

///////////////////////////////////////////////////////////////////////////////
//
// Storage REST server, storageRESTReceiver and StorageRESTClient are
// inter-dependent, below test functions are sufficient to test all of them.
//
///////////////////////////////////////////////////////////////////////////////

func testStorageAPIDiskInfo(t *testing.T, storage StorageAPI) {
	testCases := []struct {
		expectErr bool
	}{
		{true},
	}

	for i, testCase := range testCases {
		_, err := storage.DiskInfo(context.Background())
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}
		if err != errUnformattedDisk {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, errUnformattedDisk, err)
		}
	}
}

func testStorageAPIMakeVol(t *testing.T, storage StorageAPI) {
	testCases := []struct {
		volumeName string
		expectErr  bool
	}{
		{"foo", false},
		// volume exists error.
		{"foo", true},
	}

	for i, testCase := range testCases {
		err := storage.MakeVol(context.Background(), testCase.volumeName)
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}
	}
}

func testStorageAPIListVols(t *testing.T, storage StorageAPI) {
	testCases := []struct {
		volumeNames    []string
		expectedResult []VolInfo
		expectErr      bool
	}{
		{nil, []VolInfo{{Name: ".otterio.sys"}}, false},
		{[]string{"foo"}, []VolInfo{{Name: ".otterio.sys"}, {Name: "foo"}}, false},
	}

	for i, testCase := range testCases {
		for _, volumeName := range testCase.volumeNames {
			err := storage.MakeVol(context.Background(), volumeName)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
		}

		result, err := storage.ListVols(context.Background())
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}

		if !testCase.expectErr {
			if len(result) != len(testCase.expectedResult) {
				t.Fatalf("case %v: result: expected: %+v, got: %+v", i+1, testCase.expectedResult, result)
			}
		}
	}
}

func testStorageAPIStatVol(t *testing.T, storage StorageAPI) {
	err := storage.MakeVol(context.Background(), "foo")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	testCases := []struct {
		volumeName string
		expectErr  bool
	}{
		{"foo", false},
		// volume not found error.
		{"bar", true},
	}

	for i, testCase := range testCases {
		result, err := storage.StatVol(context.Background(), testCase.volumeName)
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}

		if !testCase.expectErr {
			if result.Name != testCase.volumeName {
				t.Fatalf("case %v: result: expected: %+v, got: %+v", i+1, testCase.volumeName, result.Name)
			}
		}
	}
}

func testStorageAPIDeleteVol(t *testing.T, storage StorageAPI) {
	err := storage.MakeVol(context.Background(), "foo")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	testCases := []struct {
		volumeName string
		expectErr  bool
	}{
		{"foo", false},
		// volume not found error.
		{"bar", true},
	}

	for i, testCase := range testCases {
		err := storage.DeleteVol(context.Background(), testCase.volumeName, false)
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}
	}
}

func testStorageAPICheckFile(t *testing.T, storage StorageAPI) {
	err := storage.MakeVol(context.Background(), "foo")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	err = storage.AppendFile(context.Background(), "foo", pathJoin("myobject", xlStorageFormatFile), []byte("foo"))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	testCases := []struct {
		volumeName string
		objectName string
		expectErr  bool
	}{
		{"foo", "myobject", false},
		// file not found error.
		{"foo", "yourobject", true},
	}

	for i, testCase := range testCases {
		err := storage.CheckFile(context.Background(), testCase.volumeName, testCase.objectName)
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}
	}
}

func testStorageAPIListDir(t *testing.T, storage StorageAPI) {
	err := storage.MakeVol(context.Background(), "foo")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	err = storage.AppendFile(context.Background(), "foo", "path/to/myobject", []byte("foo"))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	testCases := []struct {
		volumeName     string
		prefix         string
		expectedResult []string
		expectErr      bool
	}{
		{"foo", "path", []string{"to/"}, false},
		// prefix not found error.
		{"foo", "nodir", nil, true},
	}

	for i, testCase := range testCases {
		result, err := storage.ListDir(context.Background(), testCase.volumeName, testCase.prefix, -1)
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}

		if !testCase.expectErr {
			if !reflect.DeepEqual(result, testCase.expectedResult) {
				t.Fatalf("case %v: result: expected: %v, got: %v", i+1, testCase.expectedResult, result)
			}
		}
	}
}

func testStorageAPIReadAll(t *testing.T, storage StorageAPI) {
	err := storage.MakeVol(context.Background(), "foo")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	err = storage.AppendFile(context.Background(), "foo", "myobject", []byte("foo"))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	testCases := []struct {
		volumeName     string
		objectName     string
		expectedResult []byte
		expectErr      bool
	}{
		{"foo", "myobject", []byte("foo"), false},
		// file not found error.
		{"foo", "yourobject", nil, true},
	}

	for i, testCase := range testCases {
		result, err := storage.ReadAll(context.Background(), testCase.volumeName, testCase.objectName)
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}

		if !testCase.expectErr {
			if !reflect.DeepEqual(result, testCase.expectedResult) {
				t.Fatalf("case %v: result: expected: %v, got: %v", i+1, string(testCase.expectedResult), string(result))
			}
		}
	}
}

func testStorageAPIReadFile(t *testing.T, storage StorageAPI) {
	err := storage.MakeVol(context.Background(), "foo")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	err = storage.AppendFile(context.Background(), "foo", "myobject", []byte("foo"))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	testCases := []struct {
		volumeName     string
		objectName     string
		offset         int64
		expectedResult []byte
		expectErr      bool
	}{
		{"foo", "myobject", 0, []byte("foo"), false},
		{"foo", "myobject", 1, []byte("oo"), false},
		// file not found error.
		{"foo", "yourobject", 0, nil, true},
	}

	for i, testCase := range testCases {
		result := make([]byte, len(testCase.expectedResult))
		_, err := storage.ReadFile(context.Background(), testCase.volumeName, testCase.objectName, testCase.offset, result, nil)
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}

		if !testCase.expectErr {
			if !reflect.DeepEqual(result, testCase.expectedResult) {
				t.Fatalf("case %v: result: expected: %v, got: %v", i+1, string(testCase.expectedResult), string(result))
			}
		}
	}
}

func testStorageAPIAppendFile(t *testing.T, storage StorageAPI) {
	err := storage.MakeVol(context.Background(), "foo")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	testCases := []struct {
		volumeName string
		objectName string
		data       []byte
		expectErr  bool
	}{
		{"foo", "myobject", []byte("foo"), false},
		{"foo", "myobject", []byte{}, false},
		// volume not found error.
		{"bar", "myobject", []byte{}, true},
	}

	for i, testCase := range testCases {
		err := storage.AppendFile(context.Background(), testCase.volumeName, testCase.objectName, testCase.data)
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}
	}
}

func testStorageAPIDeleteFile(t *testing.T, storage StorageAPI) {
	err := storage.MakeVol(context.Background(), "foo")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	err = storage.AppendFile(context.Background(), "foo", "myobject", []byte("foo"))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	testCases := []struct {
		volumeName string
		objectName string
		expectErr  bool
	}{
		{"foo", "myobject", false},
		// should removed by above case.
		{"foo", "myobject", true},
		// file not found error
		{"foo", "yourobject", true},
	}

	for i, testCase := range testCases {
		err := storage.Delete(context.Background(), testCase.volumeName, testCase.objectName, false)
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}
	}
}

func testStorageAPIRenameFile(t *testing.T, storage StorageAPI) {
	err := storage.MakeVol(context.Background(), "foo")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	err = storage.MakeVol(context.Background(), "bar")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	err = storage.AppendFile(context.Background(), "foo", "myobject", []byte("foo"))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	err = storage.AppendFile(context.Background(), "foo", "otherobject", []byte("foo"))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	testCases := []struct {
		volumeName     string
		objectName     string
		destVolumeName string
		destObjectName string
		expectErr      bool
	}{
		{"foo", "myobject", "foo", "yourobject", false},
		{"foo", "yourobject", "bar", "myobject", false},
		// overwrite.
		{"foo", "otherobject", "bar", "myobject", false},
	}

	for i, testCase := range testCases {
		err := storage.RenameFile(context.Background(), testCase.volumeName, testCase.objectName, testCase.destVolumeName, testCase.destObjectName)
		expectErr := (err != nil)

		if expectErr != testCase.expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v", i+1, testCase.expectErr, expectErr)
		}
	}
}

func newStorageRESTHTTPServerClient(t *testing.T) (*httptest.Server, *storageRESTClient, config.Config, string) {
	prevHost, prevPort := globalOtterioHost, globalOtterioPort
	defer func() {
		globalOtterioHost, globalOtterioPort = prevHost, prevPort
	}()

	endpointPath, err := os.MkdirTemp("", ".TestStorageREST.")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	app := newFiberApp()
	httpServer := httptest.NewServer(adaptor.FiberApp(app))

	url, err := xnet.ParseHTTPURL(httpServer.URL)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	url.Path = endpointPath

	globalOtterioHost, globalOtterioPort = mustSplitHostPort(url.Host)

	endpoint, err := NewEndpoint(url.String())
	if err != nil {
		t.Fatalf("NewEndpoint failed %v", endpoint)
	}

	if err = endpoint.UpdateIsLocal(); err != nil {
		t.Fatalf("UpdateIsLocal failed %v", err)
	}

	registerStorageRESTHandlersFiber(app, []PoolEndpoints{{
		Endpoints: Endpoints{endpoint},
	}})

	prevGlobalServerConfig := globalServerConfig
	globalServerConfig = newServerConfig()
	lookupConfigs(globalServerConfig, nil)

	restClient := newStorageRESTClient(endpoint, false)

	return httpServer, restClient, prevGlobalServerConfig, endpointPath
}

func TestStorageRESTClientDiskInfo(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIDiskInfo(t, restClient)
}

func TestStorageRESTClientMakeVol(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIMakeVol(t, restClient)
}

func TestStorageRESTClientListVols(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIListVols(t, restClient)
}

func TestStorageRESTClientStatVol(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIStatVol(t, restClient)
}

func TestStorageRESTClientDeleteVol(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIDeleteVol(t, restClient)
}

func TestStorageRESTClientCheckFile(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPICheckFile(t, restClient)
}

func TestStorageRESTClientListDir(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIListDir(t, restClient)
}

func TestStorageRESTClientReadAll(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIReadAll(t, restClient)
}

func TestStorageRESTClientReadFile(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIReadFile(t, restClient)
}

func TestStorageRESTClientAppendFile(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIAppendFile(t, restClient)
}

func TestStorageRESTClientDeleteFile(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIDeleteFile(t, restClient)
}

func TestStorageRESTClientRenameFile(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	testStorageAPIRenameFile(t, restClient)
}

// TestStorageRESTClientReadFileStream exercises ReadFileStreamHandler, which is
// bridged through the streaming path (io.Copy + Content-Length). It validates
// that a full and a partial (offset/length) read round-trip correctly through
// the streamed response over the real REST client.
func TestStorageRESTClientReadFileStream(t *testing.T) {
	httpServer, restClient, prevGlobalServerConfig, endpointPath := newStorageRESTHTTPServerClient(t)
	defer httpServer.Close()
	defer func() {
		globalServerConfig = prevGlobalServerConfig
	}()
	defer os.RemoveAll(endpointPath)

	if err := restClient.MakeVol(context.Background(), "foo"); err != nil {
		t.Fatalf("MakeVol failed: %v", err)
	}
	payload := "the quick brown fox jumps over the lazy dog"
	if err := restClient.AppendFile(context.Background(), "foo", "myobject", []byte(payload)); err != nil {
		t.Fatalf("AppendFile failed: %v", err)
	}

	// Full read of the streamed body.
	rc, err := restClient.ReadFileStream(context.Background(), "foo", "myobject", 0, int64(len(payload)))
	if err != nil {
		t.Fatalf("ReadFileStream failed: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("reading streamed body failed: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("full read mismatch: got %q want %q", got, payload)
	}

	// Partial read (offset/length) of the streamed body.
	rc, err = restClient.ReadFileStream(context.Background(), "foo", "myobject", 4, 5)
	if err != nil {
		t.Fatalf("ReadFileStream (offset) failed: %v", err)
	}
	got, err = io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("reading partial streamed body failed: %v", err)
	}
	if string(got) != "quick" {
		t.Fatalf("partial read mismatch: got %q want %q", got, "quick")
	}
}
