// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
	"github.com/soulteary/otterio/cmd/crypto"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/kms"
)

const conditionalMatchTestKeyID = "conditional-test-key"

func conditionalMatchTestKMS(t *testing.T) crypto.KMS {
	t.Helper()
	k, err := kms.New(conditionalMatchTestKeyID, bytes.Repeat([]byte{19}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// The Fiber test bridge preserves wire spelling such as "ETag" rather than
// canonicalizing the map key to net/http's "Etag". Inspect names as HTTP does,
// including in negative assertions that must not miss a leaked header.
func conditionalMatchResponseHeader(header http.Header, name string) string {
	for key, values := range header {
		if strings.EqualFold(key, name) {
			for _, value := range values {
				if value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func conditionalMatchMetadataHeadersAbsent(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	for _, name := range []string{xhttp.LastModified, xhttp.ETag, xhttp.AmzVersionID, "Expires", "Cache-Control"} {
		if value := conditionalMatchResponseHeader(response.Header(), name); value != "" {
			t.Errorf("response leaked %s=%q after authorization/condition failure", name, value)
		}
	}
}

func TestConditionalMatchParser(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		etag   string
		any    bool
		code   APIErrorCode
	}{
		{name: "absent"},
		{name: "quoted", values: []string{`"AbC-2"`}, etag: "AbC-2"},
		{name: "bare-s3", values: []string{"AbC-2"}, etag: "AbC-2"},
		{name: "ows", values: []string{" \t\"abc\" \t"}, etag: "abc"},
		{name: "opaque-comma", values: []string{`"a,b"`}, etag: "a,b"},
		{name: "wildcard", values: []string{"*"}, any: true},
		{name: "empty", values: []string{""}, code: ErrInvalidRequest},
		{name: "empty-tag", values: []string{`""`}, code: ErrInvalidRequest},
		{name: "duplicate", values: []string{`"a"`, `"a"`}, code: ErrInvalidRequest},
		{name: "weak", values: []string{`W/"a"`}, code: ErrNotImplemented},
		{name: "list", values: []string{`"a", "b"`}, code: ErrNotImplemented},
		{name: "bare-list", values: []string{"a,b"}, code: ErrNotImplemented},
		{name: "wildcard-list", values: []string{"*,a"}, code: ErrNotImplemented},
		{name: "unclosed", values: []string{`"a`}, code: ErrInvalidRequest},
		{name: "trailing", values: []string{`"a" junk`}, code: ErrInvalidRequest},
		{name: "embedded-quote", values: []string{`a"b`}, code: ErrInvalidRequest},
		{name: "control", values: []string{"a\r\nb"}, code: ErrInvalidRequest},
		{name: "space-in-tag", values: []string{`"a b"`}, code: ErrInvalidRequest},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			condition, code := parseObjectIfMatch(test.values)
			if code != test.code {
				t.Fatalf("code=%v want %v", code, test.code)
			}
			if code != ErrNone || len(test.values) == 0 {
				if condition != nil {
					t.Fatal("absent/invalid condition returned a usable condition")
				}
				return
			}
			if condition == nil || condition.ETag != test.etag || condition.Any != test.any {
				t.Fatalf("condition=%+v", condition)
			}
		})
	}
}

func TestConditionalMatchCapabilities(t *testing.T) {
	object, cache, gateway, distributed := newObjectLayerFn(), newCachedObjectLayerFn(), globalIsGateway, globalIsDistErasure
	t.Cleanup(func() {
		setObjectLayer(object)
		setCacheObjectLayer(cache)
		globalIsGateway, globalIsDistErasure = gateway, distributed
	})
	setObjectLayer(&FSObjects{})
	setCacheObjectLayer(nil)
	globalIsGateway, globalIsDistErasure = false, false
	if !conditionalMatchWritesSupported() {
		t.Fatal("FS must advertise conditional match writes")
	}
	recorder := httptest.NewRecorder()
	setCommonHeaders(recorder)
	if recorder.Header().Get(conditionalWritesHeader) != "v1" || recorder.Header().Get(conditionalMatchWritesHeader) != "v1" {
		t.Fatalf("separate capabilities missing: %v", recorder.Header())
	}
	globalIsDistErasure = true
	if conditionalMatchWritesSupported() || !conditionalWritesSupported() {
		t.Fatal("new match scope must not change the existing create-only capability")
	}
	globalIsDistErasure = false
	for _, candidate := range []CacheObjectLayer{&cacheObjects{commitWriteback: true}} {
		setCacheObjectLayer(candidate)
		if conditionalMatchWritesSupported() {
			t.Fatal("write-back must not advertise match writes")
		}
	}
	setCacheObjectLayer(nil)
	setObjectLayer(&erasureServerPools{serverPools: []*erasureSets{{}, {}}})
	if conditionalMatchWritesSupported() {
		t.Fatal("multiple pools must not advertise match writes")
	}
	setObjectLayer(nil)
	if conditionalMatchWritesSupported() {
		t.Fatal("uninitialized storage must not advertise match writes")
	}
}

func TestConditionalMatchLockedCheck(t *testing.T) {
	ctx := context.Background()
	current := ObjectInfo{ETag: "current", UserTags: "environment=prod"}
	readErr := error(nil)
	reads, authorized := 0, 0
	get := func(_ context.Context, _, _ string, opts ObjectOptions) (ObjectInfo, error) {
		reads++
		if !opts.NoLock || opts.VersionID != "" || !opts.Versioned {
			t.Fatalf("must inspect the locked current version: %+v", opts)
		}
		return current, readErr
	}
	opts := ObjectOptions{Versioned: true, IfMatch: &ObjectIfMatchCondition{ETag: "current", Authorize: func(actual ObjectInfo) error {
		authorized++
		if actual.UserTags != "environment=prod" {
			t.Fatalf("authorization did not receive stored tags: %+v", actual)
		}
		return nil
	}}}
	if err := checkObjectWritePreconditions(ctx, "bucket", "key", ObjectOptions{}, get); err != nil || reads != 0 {
		t.Fatalf("unconditional write performed extra metadata IO: %v/%d", err, reads)
	}
	if err := checkObjectWritePreconditions(ctx, "bucket", "key", opts, get); err != nil || authorized != 1 {
		t.Fatalf("authorized match failed: %v", err)
	}
	opts.IfMatch.ETag = "stale"
	if err := checkObjectWritePreconditions(ctx, "bucket", "key", opts, get); !isErrPreconditionFailed(err) {
		t.Fatalf("mismatch=%v", err)
	}
	opts.IfMatch.Authorize = func(ObjectInfo) error { return errAuthentication }
	if err := checkObjectWritePreconditions(ctx, "bucket", "key", opts, get); err != errAuthentication {
		t.Fatalf("condition revealed before authorization: %v", err)
	}
	readErr = ObjectNotFound{Bucket: "bucket", Object: "key"}
	if err := checkObjectWritePreconditions(ctx, "bucket", "key", opts, get); err != errAuthentication {
		t.Fatalf("missing object revealed before authorization: %v", err)
	}
	opts.IfMatch.Authorize = func(actual ObjectInfo) error {
		if actual.UserTags != "" {
			t.Fatal("missing object inherited stale tags")
		}
		return nil
	}
	if err := checkObjectWritePreconditions(ctx, "bucket", "key", opts, get); !isErrObjectNotFound(err) {
		t.Fatalf("missing match=%v", err)
	}
	readErr = errors.New("metadata unavailable")
	if err := checkObjectWritePreconditions(ctx, "bucket", "key", opts, get); err != readErr {
		t.Fatalf("storage error hidden: %v", err)
	}
	readErr = nil
	opts.IfMatch.Authorize = nil
	current.UserDefined = map[string]string{ReservedMetadataPrefix + "compression": compressionAlgorithmV2}
	if err := checkObjectWritePreconditions(ctx, "bucket", "key", opts, get); toAPIErrorCode(ctx, err) != ErrNotImplemented {
		t.Fatalf("compressed validator must be rejected explicitly: %v", err)
	}
	current.UserDefined = nil
	opts.IfMatch.Any = true
	for _, invalid := range []ObjectInfo{{ETag: defaultEtag}, {}, {ETag: "directory", IsDir: true}} {
		current = invalid
		if err := checkObjectWritePreconditions(ctx, "bucket", "key", opts, get); toAPIErrorCode(ctx, err) != ErrNotImplemented {
			t.Fatalf("non-authoritative wildcard accepted: %+v/%v", invalid, err)
		}
	}
}

func TestConditionalMatchEncryptedETag(t *testing.T) {
	withTempGlobalKMS(t, conditionalMatchTestKMS(t))
	const bucket, object = "conditional-bucket", "encrypted"
	digest := md5.Sum([]byte("conditional contents"))
	plain := hex.EncodeToString(digest[:])
	for _, mode := range []string{"S3", "KMS", "SSEC"} {
		t.Run(mode, func(t *testing.T) {
			metadata := make(map[string]string)
			var key crypto.ObjectKey
			var err error
			switch mode {
			case "S3":
				key, err = newEncryptMetadata(nil, bucket, object, metadata, true)
			case "KMS":
				key, err = newEncryptMetadataKMS(bucket, object, metadata, conditionalMatchTestKeyID, nil)
			default:
				key, err = newEncryptMetadata(bytes.Repeat([]byte{7}, 32), bucket, object, metadata, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			stored := ObjectInfo{Bucket: bucket, Name: object, ETag: hex.EncodeToString(key.SealETag(digest[:])), UserDefined: metadata}
			want := plain
			if mode == "SSEC" {
				want = stored.ETag[len(stored.ETag)-32:]
			}
			visible, err := objectIfMatchETag(bucket, object, stored)
			if err != nil || visible != want || visible != getDecryptedETag(http.Header{}, stored, false) {
				t.Fatalf("validator=%q want %q error=%v", visible, want, err)
			}
			if mode != "SSEC" {
				saved := GlobalKMS
				GlobalKMS = nil
				_, err = objectIfMatchETag(bucket, object, stored)
				GlobalKMS = saved
				if err != errKMSNotConfigured {
					t.Fatalf("KMS failure fell back to stored ETag: %v", err)
				}
			}
			stored.UserDefined[crypto.MetaMultipart] = ""
			stored.ETag = plain + "-2"
			visible, err = objectIfMatchETag(bucket, object, stored)
			if err != nil || visible != stored.ETag {
				t.Fatalf("multipart validator changed: %q/%v", visible, err)
			}
		})
	}
}

func conditionalMatchRequest(t *testing.T, signature, method, target, data string, credentials auth.Credentials, headers http.Header) *http.Request {
	t.Helper()
	body := strings.NewReader(data)
	var request *http.Request
	var err error
	if signature == "streaming" {
		request, err = newTestStreamingRequest(method, target, int64(len(data)), 16, body)
	} else {
		request, err = newTestRequest(method, target, int64(len(data)), body)
	}
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range headers {
		request.Header[name] = append([]string(nil), values...)
	}
	switch signature {
	case "presigned":
		request = signer.PreSignV4(*request, credentials.AccessKey, credentials.SecretKey, "", globalServerRegion, 60)
	case "streaming":
		now := UTCNow()
		var seed string
		seed, err = signStreamingRequest(request, credentials.AccessKey, credentials.SecretKey, now)
		if err == nil {
			request, err = assembleStreamingChunks(request, body, 16, credentials.SecretKey, seed, now)
		}
	case "v2":
		err = signRequestV2(request, credentials.AccessKey, credentials.SecretKey)
	default:
		err = signRequestV4(request, credentials.AccessKey, credentials.SecretKey)
	}
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func conditionalMatchStored(t *testing.T, obj ObjectLayer, bucket, key, want string) {
	t.Helper()
	r, err := obj.GetObjectNInfo(context.Background(), bucket, key, nil, http.Header{}, readLock, ObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(data) != want {
		t.Fatalf("stored %q want %q, error=%v", data, want, err)
	}
}

func TestConditionalMatchHTTPStorage(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, backend, bucket string, router http.Handler, credentials auth.Credentials, t *testing.T) {
		ctx := context.Background()
		put := func(key, body, tags string) ObjectInfo {
			t.Helper()
			info, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, strings.NewReader(body), int64(len(body)), "", ""), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: tags}})
			if err != nil {
				t.Fatal(err)
			}
			return info
		}
		for _, signature := range []string{"v4", "presigned", "streaming"} {
			t.Run(backend+"/"+signature, func(t *testing.T) {
				key := "match-" + signature
				old := put(key, "old contents", "")
				url := "http://otterio.test/" + bucket + "/" + key
				for i, match := range []string{`"` + old.ETag + `"`, old.ETag, "*"} {
					response := httptest.NewRecorder()
					router.ServeHTTP(response, conditionalMatchRequest(t, signature, http.MethodPut, url, "new contents", credentials, http.Header{xhttp.IfMatch: {match}}))
					want := http.StatusOK
					if i == 1 {
						want = http.StatusPreconditionFailed
					}
					if response.Code != want {
						t.Fatalf("If-Match=%q got %d want %d: %s", match, response.Code, want, response.Body.String())
					}
					if want != http.StatusOK {
						conditionalMatchMetadataHeadersAbsent(t, response)
					}
				}
				conditionalMatchStored(t, obj, bucket, key, "new contents")
				missing := httptest.NewRecorder()
				router.ServeHTTP(missing, conditionalMatchRequest(t, signature, http.MethodPut, url+"-missing", "new", credentials, http.Header{xhttp.IfMatch: {"*"}}))
				if missing.Code != http.StatusNotFound {
					t.Fatalf("missing=%d: %s", missing.Code, missing.Body.String())
				}
				if _, err := obj.GetObjectInfo(ctx, bucket, key+"-missing", ObjectOptions{}); !isErrObjectNotFound(err) {
					t.Fatalf("missing match created object: %v", err)
				}
			})
		}

		t.Run(backend+"/signature-binding", func(t *testing.T) {
			key := "signature-binding"
			old := put(key, "keep", "")
			url := "http://otterio.test/" + bucket + "/" + key
			for _, mode := range []string{"unsigned", "changed", "removed", "v2", "duplicate", "both"} {
				headers := http.Header{xhttp.IfMatch: {old.ETag}}
				signature := "v4"
				if mode == "unsigned" {
					headers = nil
				}
				if mode == "v2" {
					signature = "v2"
				}
				if mode == "duplicate" {
					headers.Add(xhttp.IfMatch, old.ETag)
				}
				if mode == "both" {
					headers.Set(xhttp.IfNoneMatch, "*")
				}
				request := conditionalMatchRequest(t, signature, http.MethodPut, url, "reject", credentials, headers)
				switch mode {
				case "unsigned":
					request.Header.Set(xhttp.IfMatch, old.ETag)
				case "changed":
					request.Header.Set(xhttp.IfMatch, "different")
				case "removed":
					request.Header.Del(xhttp.IfMatch)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != http.StatusForbidden && response.Code != http.StatusBadRequest {
					t.Fatalf("%s accepted/incorrect rejection: %d %s", mode, response.Code, response.Body.String())
				}
				conditionalMatchStored(t, obj, bucket, key, "keep")
			}
			put("signature-copy-source", "source contents", "")
			for _, signature := range []string{"v4", "presigned"} {
				for _, name := range []string{xhttp.IfMatch, xhttp.IfNoneMatch} {
					value := old.ETag
					if name == xhttp.IfNoneMatch {
						value = "*"
					}
					// Both query and header are signed. Removing only the
					// header must not activate the verifier's query fallback
					// and then silently perform an unconditional overwrite.
					for _, operation := range []string{"put", "delete", "copy"} {
						method, body := http.MethodPut, "reject"
						headers := http.Header{name: {value}}
						if operation == "delete" {
							method, body = http.MethodDelete, ""
						} else if operation == "copy" {
							body = ""
							headers.Set(xhttp.AmzCopySource, "/"+bucket+"/signature-copy-source")
						}
						request := conditionalMatchRequest(t, signature, method, url+"?"+strings.ToLower(name)+"="+value, body, credentials, headers)
						request.Header.Del(name)
						response := httptest.NewRecorder()
						router.ServeHTTP(response, request)
						if response.Code != http.StatusForbidden {
							t.Fatalf("%s %s %s query fallback accepted: %d %s", signature, operation, name, response.Code, response.Body.String())
						}
						conditionalMatchStored(t, obj, bucket, key, "keep")
					}
				}
			}
		})

		t.Run(backend+"/tag-authorization", func(t *testing.T) {
			user := consoleReviewUser(t, obj, "conditional-match-tags", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::`+bucket+`/*"]},{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::`+bucket+`/*"],"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"prod"}}}]}`)
			for _, tag := range []string{"prod", "dev", ""} {
				key := "tag-" + tag
				info := put(key, "keep", "environment="+tag)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodPut, "http://otterio.test/"+bucket+"/"+key, "changed", user, http.Header{xhttp.IfMatch: {info.ETag}, xhttp.AmzObjectTagging: {"environment=prod"}}))
				if tag == "prod" {
					if response.Code != http.StatusOK {
						t.Fatalf("authorized stored tag rejected: %d %s", response.Code, response.Body.String())
					}
					conditionalMatchStored(t, obj, bucket, key, "changed")
				} else {
					if response.Code != http.StatusForbidden {
						t.Fatalf("forged/request tags authorized current object: %d %s", response.Code, response.Body.String())
					}
					conditionalMatchMetadataHeadersAbsent(t, response)
					conditionalMatchStored(t, obj, bucket, key, "keep")
				}
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodPut, "http://otterio.test/"+bucket+"/missing-forbidden", "new", user, http.Header{xhttp.IfMatch: {"*"}}))
			if response.Code != http.StatusForbidden {
				t.Fatalf("missing metadata leaked: %d %s", response.Code, response.Body.String())
			}
			conditionalMatchMetadataHeadersAbsent(t, response)
		})

		t.Run(backend+"/multipart", func(t *testing.T) {
			const key = "multipart-match"
			old := put(key, "current destination", "")
			upload, err := obj.NewMultipartUpload(ctx, bucket, key, ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			part, err := obj.PutObjectPart(ctx, bucket, key, upload, 1, mustGetPutObjReader(t, strings.NewReader("multipart new"), 13, "", ""), ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			payload := fmt.Sprintf(`<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>"%s"</ETag></Part></CompleteMultipartUpload>`, part.ETag)
			url := getCompleteMultipartUploadURL("http://otterio.test", bucket, key, upload)
			for _, match := range []string{part.ETag, old.ETag} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodPost, url, payload, credentials, http.Header{xhttp.IfMatch: {match}}))
				if match == part.ETag {
					if response.Code != http.StatusPreconditionFailed && !(response.Code == http.StatusOK && strings.Contains(response.Body.String(), "<Code>PreconditionFailed</Code>")) {
						t.Fatalf("completion compared the upload instead of destination: %d %s", response.Code, response.Body.String())
					}
					conditionalMatchStored(t, obj, bucket, key, "current destination")
					continue
				}
				if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "<Error>") {
					t.Fatalf("valid completion failed: %d %s", response.Code, response.Body.String())
				}
				conditionalMatchStored(t, obj, bucket, key, "multipart new")
			}
		})

		if obj.IsEncryptionSupported() {
			t.Run(backend+"/encrypted-current", func(t *testing.T) {
				oldKMS := GlobalKMS
				oldTLS := globalIsTLS
				GlobalKMS = conditionalMatchTestKMS(t)
				globalIsTLS = true
				defer func() { GlobalKMS, globalIsTLS = oldKMS, oldTLS }()
				for _, mode := range []string{"S3", "KMS", "SSEC"} {
					for _, multipart := range []bool{false, true} {
						key := fmt.Sprintf("encrypted-%s-%v", mode, multipart)
						url := "http://otterio.test/" + bucket + "/" + key
						headers, readHeaders := http.Header{}, http.Header{}
						switch mode {
						case "S3":
							headers.Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionAES)
						case "KMS":
							headers.Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionKMS)
							headers.Set(xhttp.AmzServerSideEncryptionKmsID, conditionalMatchTestKeyID)
						default:
							customer := bytes.Repeat([]byte{3}, 32)
							digest := md5.Sum(customer)
							headers.Set(xhttp.AmzServerSideEncryptionCustomerAlgorithm, xhttp.AmzEncryptionAES)
							headers.Set(xhttp.AmzServerSideEncryptionCustomerKey, base64.StdEncoding.EncodeToString(customer))
							headers.Set(xhttp.AmzServerSideEncryptionCustomerKeyMD5, base64.StdEncoding.EncodeToString(digest[:]))
							readHeaders = headers.Clone()
						}
						response := httptest.NewRecorder()
						if !multipart {
							router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodPut, url, "encrypted original", credentials, headers))
						} else {
							router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodPost, url+"?uploads=", "", credentials, headers))
							if response.Code != http.StatusOK {
								t.Fatalf("init %s: %d %s", key, response.Code, response.Body.String())
							}
							var created struct {
								UploadID string `xml:"UploadId"`
							}
							if err := xml.Unmarshal(response.Body.Bytes(), &created); err != nil || created.UploadID == "" {
								t.Fatalf("init upload: %s / %v", response.Body.String(), err)
							}
							response = httptest.NewRecorder()
							partURL := getPutObjectPartURL("http://otterio.test", bucket, key, created.UploadID, "1")
							router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodPut, partURL, "encrypted original", credentials, readHeaders))
							if response.Code != http.StatusOK {
								t.Fatalf("part %s: %d %s", key, response.Code, response.Body.String())
							}
							payload := `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>` + conditionalMatchResponseHeader(response.Header(), xhttp.ETag) + `</ETag></Part></CompleteMultipartUpload>`
							partETag := canonicalizeETag(conditionalMatchResponseHeader(response.Header(), xhttp.ETag))
							listed := httptest.NewRecorder()
							router.ServeHTTP(listed, conditionalMatchRequest(t, "v4", http.MethodGet, getCompleteMultipartUploadURL("http://otterio.test", bucket, key, created.UploadID), "", credentials, readHeaders))
							var listResult struct {
								Parts []struct {
									ETag string `xml:"ETag"`
								} `xml:"Part"`
							}
							if err := xml.Unmarshal(listed.Body.Bytes(), &listResult); err != nil || listed.Code != http.StatusOK || len(listResult.Parts) != 1 || canonicalizeETag(listResult.Parts[0].ETag) != partETag {
								t.Fatalf("list part validator %s: %d %s / %v", key, listed.Code, listed.Body.String(), err)
							}
							response = httptest.NewRecorder()
							router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodPost, getCompleteMultipartUploadURL("http://otterio.test", bucket, key, created.UploadID), payload, credentials, readHeaders))
						}
						if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "<Error>") {
							t.Fatalf("encrypted creation %s: %d %s", key, response.Code, response.Body.String())
						}
						head := httptest.NewRecorder()
						router.ServeHTTP(head, conditionalMatchRequest(t, "v4", http.MethodHead, url, "", credentials, readHeaders))
						if head.Code != http.StatusOK || conditionalMatchResponseHeader(head.Header(), xhttp.ETag) == "" {
							t.Fatalf("HEAD %s: %d %s", key, head.Code, head.Body.String())
						}
						response = httptest.NewRecorder()
						router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodPut, url, "plain replacement", credentials, http.Header{xhttp.IfMatch: {conditionalMatchResponseHeader(head.Header(), xhttp.ETag)}}))
						if response.Code != http.StatusOK {
							t.Fatalf("HEAD validator did not match %s: %d %s", key, response.Code, response.Body.String())
						}
						conditionalMatchStored(t, obj, bucket, key, "plain replacement")
					}
				}
			})
		}
	}, nil)
}
