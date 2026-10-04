/*
 * OtterIO Cloud Storage, (C) 2026 soulteary, https://github.com/soulteary/otterio
 * Licensed under the Apache License, Version 2.0.
 */

package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func securityStorage(t *testing.T) *xlStorage {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bucket"), 0755); err != nil {
		t.Fatal(err)
	}
	return &xlStorage{diskPath: root}
}

func securityFileInfo() FileInfo {
	return FileInfo{
		Volume: "bucket", Name: "object", DataDir: "c99b6572-9e2e-4b06-9d38-3c3c64c56402", ModTime: UTCNow(), Size: 8,
		Parts:   []ObjectPartInfo{{Number: 1, Size: 8}},
		Erasure: ErasureInfo{Algorithm: erasureAlgorithm, DataBlocks: 2, ParityBlocks: 2, BlockSize: 4, Index: 1, Distribution: []int{1, 2, 3, 4}, Checksums: []ChecksumInfo{{PartNumber: 1, Algorithm: HighwayHash256S}}},
	}
}

func TestStorageSecurityPaths(t *testing.T) {
	s := securityStorage(t)
	for _, name := range []string{"bucket", "bucket/nested", otterioMetaBucket, otterioMetaTmpBucket, otterioMetaMultipartBucket, "bucket/目录/", "bucket/a..b"} {
		t.Run("valid/"+name, func(t *testing.T) {
			got, err := s.getVolDir(name)
			if err != nil || !storagePathWithin(s.diskPath, got) {
				t.Fatalf("valid volume %q rejected: %q, %v", name, got, err)
			}
		})
	}
	for _, name := range []string{"", ".", "..", "/", "../outside", "bucket/../../outside", "bucket/../other", "bucket/./object", "/absolute", `C:\outside`, `C:outside`, `bucket\..\outside`, "nul\x00path"} {
		t.Run("invalid/"+strconv.Quote(name), func(t *testing.T) {
			if _, err := s.getVolDir(name); err == nil {
				t.Fatalf("accepted volume %q", name)
			}
		})
	}
	// Root listing/empty Delete are explicitly distinct from root aliases.
	if !validStoragePath("") {
		t.Fatal("empty object path must preserve root operations")
	}
	if _, err := s.ListDir(context.Background(), "bucket", "", -1); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), "bucket", "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.diskPath, "bucket")); err != nil {
		t.Fatal("root no-op removed volume:", err)
	}
}

func TestStorageSecurityBoundaryOperations(t *testing.T) {
	s := securityStorage(t)
	ctx := context.Background()
	outside := filepath.Join(s.diskPath, "bucket-other")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.deleteFile(filepath.Join(s.diskPath, "bucket"), sentinel, false); err == nil {
		t.Fatal("sibling-prefix deletion accepted")
	}
	fi := securityFileInfo()
	for _, name := range []string{"../bucket-other/sentinel", "folder/../../bucket-other/sentinel", ".", "/", "folder/..", `..\bucket-other\sentinel`, `C:sentinel`} {
		t.Run(strconv.Quote(name), func(t *testing.T) {
			calls := map[string]func() error{
				"write":     func() error { return s.WriteAll(ctx, "bucket", name, []byte("bad")) },
				"append":    func() error { return s.AppendFile(ctx, "bucket", name, []byte("bad")) },
				"create":    func() error { return s.CreateFile(ctx, "bucket", name, 3, strings.NewReader("bad")) },
				"read":      func() error { _, err := s.ReadAll(ctx, "bucket", name); return err },
				"read-file": func() error { _, err := s.ReadFile(ctx, "bucket", name, 0, make([]byte, 4), nil); return err },
				"stream": func() error {
					r, err := s.ReadFileStream(ctx, "bucket", name, 0, 4)
					if r != nil {
						r.Close()
					}
					return err
				},
				"delete":             func() error { return s.Delete(ctx, "bucket", name, true) },
				"metadata":           func() error { return s.WriteMetadata(ctx, "bucket", name, fi) },
				"update":             func() error { return s.UpdateMetadata(ctx, "bucket", name, FileInfo{}) },
				"read-version":       func() error { _, err := s.ReadVersion(ctx, "bucket", name, "", false); return err },
				"delete-version":     func() error { return s.DeleteVersion(ctx, "bucket", name, FileInfo{}, false) },
				"check":              func() error { return s.CheckParts(ctx, "bucket", name, fi) },
				"verify":             func() error { return s.VerifyFile(ctx, "bucket", name, fi) },
				"rename-source":      func() error { return s.RenameFile(ctx, "bucket", name, "bucket", "target") },
				"rename-destination": func() error { return s.RenameFile(ctx, "bucket", "source", "bucket", name) },
				"rename-data":        func() error { return s.RenameData(ctx, "bucket", "source", fi, "bucket", name) },
			}
			for operation, call := range calls {
				if err := call(); err == nil {
					t.Errorf("%s accepted %q", operation, name)
				}
			}
		})
	}
	b, err := os.ReadFile(sentinel)
	if err != nil || string(b) != "keep" {
		t.Fatalf("outside sentinel changed: %q %v", b, err)
	}
	var output bytes.Buffer
	if err := s.WalkDir(ctx, WalkDirOptions{Bucket: "bucket", BaseDir: "../bucket-other"}, &output); err == nil || output.Len() != 0 {
		t.Fatal("invalid walk emitted a stream")
	}
}

func TestStorageSecurityBatchPreflight(t *testing.T) {
	s := securityStorage(t)
	ctx := context.Background()
	if err := s.MakeVolBulk(ctx, "fresh-volume", "../outside"); err == nil {
		t.Fatal("invalid bulk volume accepted")
	}
	if _, err := os.Stat(filepath.Join(s.diskPath, "fresh-volume")); !os.IsNotExist(err) {
		t.Fatal("first volume created before late validation")
	}
	good := filepath.Join(s.diskPath, "bucket", "good")
	if err := os.MkdirAll(good, 0755); err != nil {
		t.Fatal(err)
	}
	errs := s.DeleteVersions(ctx, "bucket", []FileInfo{{Name: "good/"}, {Name: "../outside/"}})
	if len(errs) != 2 || errs[0] == nil || errs[1] == nil {
		t.Fatalf("bad batch errors: %v", errs)
	}
	if _, err := os.Stat(good); err != nil {
		t.Fatal("first entry deleted before late validation", err)
	}
	if _, err := s.NSScanner(ctx, dataUsageCache{Info: dataUsageCacheInfo{Name: "bucket"}, Cache: map[string]dataUsageEntry{"../outside": {}}}); err == nil {
		t.Fatal("scanner accepted body traversal")
	}
}

func TestStorageSecurityMetadata(t *testing.T) {
	s := securityStorage(t)
	ctx := context.Background()
	valid := securityFileInfo()
	partPath := filepath.Join(s.diskPath, "bucket", "object", valid.DataDir, "part.1")
	if err := os.MkdirAll(filepath.Dir(partPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, make([]byte, 4), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckParts(ctx, "bucket", "object", valid); err != nil {
		t.Fatal("valid shard rejected", err)
	}
	for name, mutate := range map[string]func(*FileInfo){
		"zero-block":        func(fi *FileInfo) { fi.Erasure.BlockSize = 0 },
		"negative-block":    func(fi *FileInfo) { fi.Erasure.BlockSize = -1 },
		"huge-block":        func(fi *FileInfo) { fi.Erasure.BlockSize = blockSizeV1 + 1 },
		"zero-data":         func(fi *FileInfo) { fi.Erasure.DataBlocks = 0 },
		"negative-parity":   func(fi *FileInfo) { fi.Erasure.ParityBlocks = -1 },
		"overflow-data":     func(fi *FileInfo) { fi.Erasure.DataBlocks = int(^uint(0) >> 1) },
		"negative-part":     func(fi *FileInfo) { fi.Parts[0].Size = -1 },
		"negative-total":    func(fi *FileInfo) { fi.Size = -1 },
		"datadir-traversal": func(fi *FileInfo) { fi.DataDir = "../../outside" },
		"name-traversal":    func(fi *FileInfo) { fi.Name = "../outside" },
	} {
		t.Run(name, func(t *testing.T) {
			fi := securityFileInfo()
			mutate(&fi)
			if err := s.CheckParts(ctx, "bucket", "object", fi); err == nil {
				t.Fatal("CheckParts accepted invalid metadata")
			}
			if err := s.VerifyFile(ctx, "bucket", "object", fi); err == nil {
				t.Fatal("VerifyFile accepted invalid metadata")
			}
			var meta xlMetaV2
			if err := meta.AddVersion(fi); err == nil {
				t.Fatal("AddVersion persisted invalid metadata")
			}
			if len(meta.Versions) != 0 {
				t.Fatal("invalid write mutated metadata")
			}
		})
	}
	if err := os.Truncate(partPath, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckParts(ctx, "bucket", "object", valid); err != errFileCorrupt {
		t.Fatalf("truncated shard reported healthy: %v", err)
	}
	var meta xlMetaV2
	if err := meta.AddVersion(valid); err != nil {
		t.Fatal(err)
	}
	object := *meta.Versions[0].ObjectV2
	object.PartSizes = nil
	if _, err := object.ToFileInfo("bucket", "object"); err == nil {
		t.Fatal("mismatched parallel arrays accepted")
	}
	object = *meta.Versions[0].ObjectV2
	object.PartSizes = []int64{-1}
	if object.validData() {
		t.Fatal("negative persisted part accepted")
	}
	fi := securityFileInfo()
	fi.Erasure.Checksums[0].Algorithm = 0
	if err := s.VerifyFile(ctx, "bucket", "object", fi); err == nil {
		t.Fatal("unknown bitrot algorithm accepted")
	}
}

func TestStorageSecurityArithmetic(t *testing.T) {
	for _, e := range []ErasureInfo{{}, {BlockSize: 0, DataBlocks: 2}, {BlockSize: 4, DataBlocks: 0}, {BlockSize: -1, DataBlocks: 2}} {
		if e.ShardFileSize(1) != -1 || e.ShardSize() != -1 {
			t.Fatal("invalid arithmetic must return -1", e)
		}
	}
	e := ErasureInfo{BlockSize: 4, DataBlocks: 2}
	for size, want := range map[int64]int64{0: 0, 1: 1, 4: 2, 5: 3, 8: 4, -1: -1, -2: -1} {
		if got := e.ShardFileSize(size); got != want {
			t.Errorf("%d -> %d, want %d", size, got, want)
		}
	}
	maxInt64 := int64(^uint64(0) >> 1)
	if got := (ErasureInfo{BlockSize: maxInt64, DataBlocks: 2}).ShardFileSize(maxInt64); got != maxInt64/2+1 {
		t.Fatal("overflow rounding", got)
	}
	if bitrotShardFileSize(maxInt64, 1, HighwayHash256S) != -1 {
		t.Fatal("bitrot overflow accepted")
	}
	for _, test := range []struct {
		algo BitrotAlgorithm
		size int64
	}{{0, 4}, {HighwayHash256S, 0}, {HighwayHash256S, -1}, {HighwayHash256S, blockSizeV1 + 1}} {
		if err := bitrotVerify(strings.NewReader(""), 0, 0, test.algo, nil, test.size); err == nil {
			t.Fatal("invalid bitrot parameters accepted")
		}
	}
	// Whole-file verification intentionally does not require a streaming shard size.
	h := SHA256.New()
	h.Write([]byte("ok"))
	if err := bitrotVerify(strings.NewReader("ok"), 2, 2, SHA256, h.Sum(nil), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := NewErasure(context.Background(), 2, 2, 0); err == nil {
		t.Fatal("zero encoder block accepted")
	}
}

func securityRequest(t *testing.T, values url.Values, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal?"+values.Encode(), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+newAuthToken(req.URL.RawQuery))
	req.Header.Set("X-Otterio-Time", UTCNow().Format(time.RFC3339))
	return req
}

func TestStorageSecurityAuthenticatedREST(t *testing.T) {
	s := &storageRESTServer{storage: securityStorage(t)}
	for _, n := range []int64{-1, maxStorageRESTBuffer + 1, int64(^uint64(0) >> 1)} {
		for name, handler := range map[string]http.HandlerFunc{"append": s.AppendFileHandler, "write-all": s.WriteAllHandler} {
			t.Run(name+strconv.FormatInt(n, 10), func(t *testing.T) {
				req := securityRequest(t, url.Values{storageRESTVolume: {"bucket"}, storageRESTFilePath: {"object"}}, nil)
				req.ContentLength = n
				rec := httptest.NewRecorder()
				handler(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
				}
			})
		}
	}
	for _, n := range []int64{-1, maxStorageRESTBuffer + 1, int64(^uint64(0) >> 1)} {
		req := securityRequest(t, url.Values{storageRESTVolume: {"bucket"}, storageRESTFilePath: {"object"}, storageRESTOffset: {"0"}, storageRESTLength: {strconv.FormatInt(n, 10)}}, nil)
		rec := httptest.NewRecorder()
		s.ReadFileHandler(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatal("unbounded read accepted")
		}
	}
	for _, n := range []int{-1, maxStorageRESTVersions + 1, int(^uint(0) >> 1)} {
		req := securityRequest(t, url.Values{storageRESTTotalVersions: {strconv.Itoa(n)}}, nil)
		rec := httptest.NewRecorder()
		s.DeleteVersionsHandler(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatal("unbounded batch accepted")
		}
	}
	fi := securityFileInfo()
	fi.DataDir = "../../outside"
	body, err := fi.MarshalMsg(nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, handler := range map[string]http.HandlerFunc{"check": s.CheckPartsHandler, "verify": s.VerifyFileHandler, "write-metadata": s.WriteMetadataHandler} {
		t.Run(name, func(t *testing.T) {
			req := securityRequest(t, url.Values{storageRESTVolume: {"bucket"}, storageRESTFilePath: {"object"}}, body)
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("body path bypass/early streaming: %d %q", rec.Code, rec.Body.String())
			}
		})
	}
	// A positive case ensures the preceding tests did not pass at the auth gate.
	req := securityRequest(t, url.Values{storageRESTVolume: {"bucket"}, storageRESTFilePath: {"positive"}}, []byte("ok"))
	rec := httptest.NewRecorder()
	s.WriteAllHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid authenticated write: %d %s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(s.storage.diskPath, "bucket", "positive"))
	if err != nil || string(got) != "ok" {
		t.Fatalf("valid write missing: %q %v", got, err)
	}
}

func TestStorageSecurityMsgpack(t *testing.T) {
	fi := securityFileInfo()
	b, err := fi.MarshalMsg(nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	infos, err := decodeStorageFileInfos(req, 1)
	if err != nil || len(infos) != 1 || infos[0].Name != fi.Name {
		t.Fatalf("roundtrip: %v %v", infos, err)
	}
	for name, body := range map[string][]byte{
		"huge-array":       {0xdd, 0xff, 0xff, 0xff, 0xff},
		"huge-map":         {0xdf, 0xff, 0xff, 0xff, 0xff},
		"truncated-string": {0xdb, 0, 0, 1, 0},
		"nested":           append(bytes.Repeat([]byte{0x91}, maxStorageMsgpackDepth+2), 0xc0),
		"trailing":         append(append([]byte(nil), b...), 0xc0),
		"truncated":        b[:len(b)-1],
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			if _, err := decodeStorageFileInfos(req, 1); err == nil {
				t.Fatal("malformed frame accepted")
			}
		})
	}
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	req.ContentLength = -1
	if _, err := decodeStorageFileInfos(req, 1); err != nil {
		t.Fatal("bounded unknown-length body rejected", err)
	}
	if !validStorageRESTBufferSize(maxStorageRESTBuffer) || validStorageRESTBufferSize(maxStorageRESTBuffer+1) {
		t.Fatal("buffer boundary")
	}
	if !validStorageRESTVersionCount(maxStorageRESTVersions) || validStorageRESTVersionCount(maxStorageRESTVersions+1) {
		t.Fatal("batch boundary")
	}
	// Validate all declared-length wire types without calling their allocating decoder.
	for _, body := range [][]byte{{0xc4, 1, 0}, {0xc5, 0, 1, 0}, {0xc6, 0, 0, 0, 1, 0}, {0xc7, 1, 0, 0}, {0xc8, 0, 1, 0, 0}, {0xc9, 0, 0, 0, 1, 0, 0}, {0xd4, 0, 0}, {0xdc, 0, 1, 0xc0}, {0xde, 0, 1, 0xc0, 0xc0}} {
		budget := 100
		rest, err := checkStorageMsgpack(body, 0, &budget)
		if err != nil || len(rest) != 0 {
			t.Fatalf("wire type %x rejected: %v", body, err)
		}
	}
}

func FuzzStorageSecurityMsgpack(f *testing.F) {
	fi := securityFileInfo()
	b, _ := fi.MarshalMsg(nil)
	f.Add(b)
	f.Add([]byte{0xdd, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(_ *testing.T, b []byte) {
		if len(b) > 1<<20 {
			return
		}
		budget := maxStorageMsgpackValues
		_, _ = checkStorageMsgpack(b, 0, &budget)
	})
}
