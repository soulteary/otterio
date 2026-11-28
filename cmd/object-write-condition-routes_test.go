// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/otterio/pkg/auth"
)

func TestUnsupportedObjectWriteConditionRoutes(t *testing.T) {
	apis := []string{"copyobject", "copyobjectpart", "putobjectpart", "newmultipartupload", "abortmultipartupload",
		"deleteobject", "deletemultipleobjects", "postpolicybucket", "putobjectacl", "putobjecttagging",
		"deleteobjecttagging", "putobjectretention", "putobjectlegalhold", "restoreobject"}
	for _, api := range apis {
		for _, header := range []string{"If-Match", "If-None-Match"} {
			t.Run(api+"/"+header, func(t *testing.T) {
				called := false
				handler := guardUnsupportedObjectWriteConditions(api, func(w http.ResponseWriter, _ *http.Request) {
					called = true
					w.WriteHeader(http.StatusNoContent)
				})
				request := httptest.NewRequest(http.MethodPut, "/bucket/object", nil)
				request.Header.Set(header, "*")
				response := httptest.NewRecorder()
				handler(response, request)
				if called || response.Code != http.StatusNotImplemented || !strings.Contains(response.Body.String(), "<Code>NotImplemented</Code>") {
					t.Fatalf("unsupported condition reached handler or lost error: called=%v status=%d body=%s", called, response.Code, response.Body.String())
				}
				request.Header.Del(header)
				request.Header.Set("X-Amz-Copy-Source-If-Match", `"source-etag"`)
				response = httptest.NewRecorder()
				handler(response, request)
				if !called || response.Code != http.StatusNoContent {
					t.Fatal("source-copy condition or unconditional request was rejected")
				}
			})
		}
	}
	for _, api := range []string{"getobject", "headobject", "putobject", "completemutipartupload"} {
		t.Run(api+"/handled-by-operation", func(t *testing.T) {
			called := false
			handler := guardUnsupportedObjectWriteConditions(api, func(http.ResponseWriter, *http.Request) { called = true })
			request := httptest.NewRequest(http.MethodPut, "/bucket/object", nil)
			request.Header.Set("If-Match", `"current"`)
			handler(httptest.NewRecorder(), request)
			if !called {
				t.Fatal("supported operation did not receive its own precondition")
			}
		})
	}
}

func TestAPIUnsupportedObjectWriteConditions(t *testing.T) {
	// Use the production route order so CopyObject and archive extraction are
	// selected before the ordinary PUT fallback.
	ExecObjectLayerAPITest(t, testAPIUnsupportedObjectWriteConditions, nil)
}

func testAPIUnsupportedObjectWriteConditions(obj ObjectLayer, instance, bucket string, router http.Handler, credentials auth.Credentials, t *testing.T) {
	ctx := context.Background()
	data := []byte("preserve this object")
	info, err := obj.PutObject(ctx, bucket, "original", mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"If-Match", "If-None-Match"} {
		for _, operation := range []struct {
			name, method, url string
			headers           map[string]string
		}{
			{"delete", http.MethodDelete, getDeleteObjectURL("", bucket, "original"), map[string]string{}},
			{"copy", http.MethodPut, getCopyObjectURL("", bucket, "copy-target"), map[string]string{"X-Amz-Copy-Source": "/" + bucket + "/original"}},
			{"archive", http.MethodPut, getPutObjectURL("", bucket, "original"), map[string]string{"X-Amz-Meta-Snowball-Auto-Extract": "true"}},
			{"initiate", http.MethodPost, getNewMultipartURL("", bucket, "original"), map[string]string{}},
		} {
			operation.headers[header] = `"` + info.ETag + `"`
			request, err := newTestSignedRequestV4(operation.method, operation.url, 0, nil, credentials.AccessKey, credentials.SecretKey, operation.headers)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNotImplemented {
				t.Fatalf("%s %s %s: got %d, want 501: %s", instance, operation.name, header, response.Code, response.Body.String())
			}
			current, err := obj.GetObjectInfo(ctx, bucket, "original", ObjectOptions{})
			if err != nil || current.ETag != info.ETag {
				t.Fatalf("rejected request changed original: %+v, %v", current, err)
			}
			if _, err := obj.GetObjectInfo(ctx, bucket, "copy-target", ObjectOptions{}); !isErrObjectNotFound(err) {
				t.Fatalf("rejected copy created a destination: %v", err)
			}
		}
	}
	// The destination guard must not disable the existing source condition.
	request, err := newTestSignedRequestV4(http.MethodPut, getCopyObjectURL("", bucket, "copy-target"), 0, nil,
		credentials.AccessKey, credentials.SecretKey, map[string]string{
			"X-Amz-Copy-Source":          "/" + bucket + "/original",
			"X-Amz-Copy-Source-If-Match": `"` + info.ETag + `"`,
		})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s: source conditional copy rejected: %d %s", instance, response.Code, response.Body.String())
	}
}
