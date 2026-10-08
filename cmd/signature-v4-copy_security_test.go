/*
 * OtterIO Cloud Storage, (C) 2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	s3signer "github.com/soulteary/otterio-sdk/v7/pkg/signer"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

// Use the full production Fiber routing table and real FS/erasure storage:
// testing CopyObjectHandler alone misses the PutObject -> CopyObject switch.
func TestAPISignedHeaderCopyIsolation(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, testAPISignedHeaderCopyIsolation, nil)
}

func testAPISignedHeaderCopyIsolation(obj ObjectLayer, instanceType, bucket string, router http.Handler, cred auth.Credentials, t *testing.T) {
	ctx := context.Background()
	sourceBucket := getRandomBucketName()
	if err := obj.MakeBucketWithLocation(ctx, sourceBucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	const secret = "PRIVATE-SOURCE-CONTENT"
	const original = "UPLOAD-CONTENT"
	putTaggedObject(t, obj, sourceBucket, "secret", "", []byte(secret))
	installAllowPolicy(t, bucket, "target")

	request := func(t *testing.T, mode, method, path, body string, headers http.Header, signer auth.Credentials) *http.Request {
		t.Helper()
		r, err := http.NewRequest(method, "http://otterio.test/"+path, bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		r.Header = headers.Clone()
		if r.Header == nil {
			r.Header = make(http.Header)
		}
		if mode == "presigned" {
			// An independent SDK signs host alone for a plain PUT, matching
			// delegated upload URLs that do not bind the upload body.
			r = s3signer.PreSignV4(*r, signer.AccessKey, signer.SecretKey, "", globalServerRegion, 900)
		} else {
			r.Header.Set(xhttp.AmzContentSha256, unsignedPayload)
			r = s3signer.SignV4(*r, signer.AccessKey, signer.SecretKey, "", globalServerRegion)
		}
		return r
	}
	send := func(t *testing.T, r *http.Request, status int, code string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s %s: got %d, want %d: %s", instanceType, r.Method, r.URL.Path, w.Code, status, w.Body.String())
		}
		if code != "" {
			var response APIErrorResponse
			if err := xml.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Code != code {
				t.Fatalf("want %s: %s (%v)", code, w.Body.String(), err)
			}
		}
		return w
	}
	readPublic := func(t *testing.T, want string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://otterio.test/"+bucket+"/target", nil)
		if w := send(t, r, http.StatusOK, ""); w.Body.String() != want {
			t.Fatalf("anonymous read-back got %q, want %q", w.Body.String(), want)
		}
	}
	send(t, httptest.NewRequest(http.MethodGet, "http://otterio.test/"+sourceBucket+"/secret", nil), http.StatusForbidden, "AccessDenied")
	copyHeaders := http.Header{xhttp.AmzCopySource: {"/" + sourceBucket + "/secret"}}

	for _, mode := range []string{"presigned", "authorization"} {
		t.Run(instanceType+"/"+mode, func(t *testing.T) {
			send(t, request(t, mode, http.MethodPut, bucket+"/target", original, nil, cred), http.StatusOK, "")
			readPublic(t, original)
			injected := request(t, mode, http.MethodPut, bucket+"/target", "", nil, cred)
			injected.Header.Set(xhttp.AmzCopySource, copyHeaders.Get(xhttp.AmzCopySource))
			send(t, injected, http.StatusForbidden, "AccessDenied")
			readPublic(t, original)

			// A genuinely signed CopyObject must still work, and changing an
			// already-signed source must fail without modifying the target.
			tampered := request(t, mode, http.MethodPut, bucket+"/target", "", copyHeaders, cred)
			tampered.Header.Set(xhttp.AmzCopySource, "/"+sourceBucket+"/other")
			send(t, tampered, http.StatusForbidden, "SignatureDoesNotMatch")
			readPublic(t, original)
			send(t, request(t, mode, http.MethodPut, bucket+"/target", "", copyHeaders, cred), http.StatusOK, "")
			readPublic(t, secret)

			// An UploadPart URL must not acquire UploadPartCopy semantics.
			uploadID, err := obj.NewMultipartUpload(ctx, bucket, "multipart", ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			partPath := bucket + "/multipart?partNumber=1&uploadId=" + uploadID
			w := send(t, request(t, mode, http.MethodPut, partPath, original, nil, cred), http.StatusOK, "")
			// The Fiber test bridge preserves wire header casing, so read
			// ETag case-insensitively instead of using Header.Get's Etag key.
			var etag string
			for name, values := range w.Header() {
				if strings.EqualFold(name, xhttp.ETag) && len(values) == 1 {
					etag = canonicalizeETag(values[0])
				}
			}
			if etag == "" {
				t.Fatal("successful part upload did not return an ETag")
			}
			injected = request(t, mode, http.MethodPut, partPath, "", nil, cred)
			injected.Header.Set(xhttp.AmzCopySource, copyHeaders.Get(xhttp.AmzCopySource))
			send(t, injected, http.StatusForbidden, "AccessDenied")
			if _, err := obj.CompleteMultipartUpload(ctx, bucket, "multipart", uploadID, []CompletePart{{PartNumber: 1, ETag: etag}}, ObjectOptions{}); err != nil {
				t.Fatal(err)
			}
			reader, err := obj.GetObjectNInfo(ctx, bucket, "multipart", nil, nil, readLock, ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || string(data) != original {
				t.Fatalf("multipart content changed after rejected copy: %q, %v", data, err)
			}
		})
	}

	// Properly signing a copy must not bypass the source IAM check either.
	// The API test harness creates IAMSys without initializing its storage.
	globalIAMSys.InitStore(obj)
	const user = "upload-only-user"
	const password = "upload-only-secret"
	if err := globalIAMSys.CreateUser(user, madmin.UserInfo{SecretKey: password, Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	var uploadPolicy iampolicy.Policy
	policyJSON := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::` + bucket + `/*"]}]}`
	if err := json.Unmarshal([]byte(policyJSON), &uploadPolicy); err != nil {
		t.Fatal(err)
	}
	if err := globalIAMSys.SetPolicy("upload-only-policy", uploadPolicy); err != nil {
		t.Fatal(err)
	}
	if err := globalIAMSys.PolicyDBSet(user, "upload-only-policy", false); err != nil {
		t.Fatal(err)
	}
	limited := auth.Credentials{AccessKey: user, SecretKey: password}
	for _, mode := range []string{"presigned", "authorization"} {
		send(t, request(t, mode, http.MethodPut, bucket+"/target", original, nil, limited), http.StatusOK, "")
		send(t, request(t, mode, http.MethodPut, bucket+"/target", "", copyHeaders, limited), http.StatusForbidden, "AccessDenied")
		readPublic(t, original)
	}

	// Stored tags are internal policy inputs, not extra client headers. Both
	// authenticated GET and HEAD must keep working with strict coverage.
	putTaggedObject(t, obj, bucket, "tagged", "dept=engineering", []byte(original))
	for _, mode := range []string{"presigned", "authorization"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			send(t, request(t, mode, method, bucket+"/tagged", "", nil, cred), http.StatusOK, "")
		}
	}
}
