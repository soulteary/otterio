// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

func consoleReviewSignedRequest(t *testing.T, signature, method, target string, user auth.Credentials, headers map[string]string) *http.Request {
	t.Helper()
	var request *http.Request
	var err error
	switch signature {
	case "v2":
		request, err = newTestSignedRequestV2(method, target, 0, strings.NewReader(""), user.AccessKey, user.SecretKey, headers)
	case "presigned":
		request, err = newTestRequest(method, target, 0, strings.NewReader(""))
		if err == nil {
			for key, value := range headers {
				request.Header.Set(key, value)
			}
			request = signer.PreSignV4(*request, user.AccessKey, user.SecretKey, "", globalServerRegion, 60)
		}
	default:
		request, err = newTestSignedRequestV4(method, target, 0, strings.NewReader(""), user.AccessKey, user.SecretKey, headers)
	}
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func consoleReviewUser(t *testing.T, obj ObjectLayer, name, document string) auth.Credentials {
	t.Helper()
	globalIAMSys.InitStore(obj)
	user := auth.Credentials{AccessKey: name, SecretKey: name + "-secret"}
	if err := globalIAMSys.CreateUser(user.AccessKey, madmin.UserInfo{SecretKey: user.SecretKey, Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	parsed, err := iampolicy.ParseConfig(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.SetPolicy(name, *parsed); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet(user.AccessKey, name, false); err != nil {
		t.Fatal(err)
	}
	return user
}

func TestConsoleVersionAuthorizationCopyDestinationTagsHTTPStorage(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, backend, bucket string, router http.Handler, _ auth.Credentials, t *testing.T) {
		ctx := context.Background()
		user := consoleReviewUser(t, obj, "review-copy-tags", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::`+bucket+`/dest-*"],"Condition":{"StringEquals":{"s3:RequestObjectTag/environment":"prod"}}},{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::`+bucket+`/source-*"]}]}`)
		cases := []struct {
			name, sourceTag, headerTag, directive string
			want                                  int
		}{
			{"default-dev", "dev", "prod", "", 403},
			{"copy-dev", "dev", "prod", "COPY", 403},
			{"copy-prod", "prod", "dev", "COPY", 200},
			{"default-prod", "prod", "", "", 200},
			{"replace-prod", "dev", "prod", "REPLACE", 200},
			{"replace-dev", "prod", "dev", "REPLACE", 403},
			{"replace-empty", "prod", "", "REPLACE", 403},
		}
		for _, signature := range []string{"v4", "v2", "presigned"} {
			for index, test := range cases {
				t.Run(backend+"/"+signature+"/"+test.name, func(t *testing.T) {
					source, destination := fmt.Sprintf("source-%s-%d", signature, index), fmt.Sprintf("dest-%s-%d", signature, index)
					if _, err := obj.PutObject(ctx, bucket, source, mustGetPutObjReader(t, strings.NewReader("tag data"), 8, "", ""), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=" + test.sourceTag}}); err != nil {
						t.Fatal(err)
					}
					headers := map[string]string{xhttp.AmzCopySource: "/" + bucket + "/" + source}
					if test.headerTag != "" {
						headers[xhttp.AmzObjectTagging] = "environment=" + test.headerTag
					}
					if test.directive != "" {
						headers[xhttp.AmzTagDirective] = test.directive
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, consoleReviewSignedRequest(t, signature, http.MethodPut, "http://otterio.test/"+bucket+"/"+destination, user, headers))
					if response.Code != test.want {
						t.Fatalf("want %d got %d %s", test.want, response.Code, response.Body.String())
					}
					if test.want == 403 {
						if _, err := obj.GetObjectInfo(ctx, bucket, destination, ObjectOptions{}); !isErrObjectNotFound(err) {
							t.Fatalf("denied copy created object: %v", err)
						}
						return
					}
					stored, err := obj.GetObjectTags(ctx, bucket, destination, ObjectOptions{})
					if err != nil || stored.String() != "environment=prod" {
						t.Fatalf("authorized copy wrote tags=%v err=%v", stored, err)
					}
				})
			}
		}
	}, nil)
}

func TestConsoleVersionAuthorizationRangeHTTPStorage(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, backend, bucket string, router http.Handler, _ auth.Credentials, t *testing.T) {
		ctx := context.Background()
		user := consoleReviewUser(t, obj, "review-range-tags", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::`+bucket+`/*"],"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"prod"}}}]}`)
		for _, tag := range []string{"prod", "dev"} {
			if _, err := obj.PutObject(ctx, bucket, tag, mustGetPutObjReader(t, strings.NewReader("tag data"), 8, "", ""), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=" + tag}}); err != nil {
				t.Fatal(err)
			}
		}
		for _, signature := range []string{"v4", "v2", "presigned"} {
			for _, test := range []struct {
				name, object, rangeHeader string
				want                      int
			}{
				{"authorized-range", "prod", "bytes=0-2", 206},
				{"authorized-invalid-range", "prod", "bytes=999-", 416},
				{"denied-invalid-range", "dev", "bytes=999-", 403},
			} {
				t.Run(backend+"/"+signature+"/"+test.name, func(t *testing.T) {
					response := httptest.NewRecorder()
					router.ServeHTTP(response, consoleReviewSignedRequest(t, signature, http.MethodGet, "http://otterio.test/"+bucket+"/"+test.object, user, map[string]string{xhttp.Range: test.rangeHeader}))
					if response.Code != test.want {
						t.Fatalf("want %d got %d %s", test.want, response.Code, response.Body.String())
					}
					if test.want == 206 && response.Body.String() != "tag" {
						t.Fatalf("incorrect range: %s", response.Body.String())
					}
				})
			}
		}
	}, nil)
}

type consoleReviewUnknownObjectLayer struct{ ObjectLayer }
type consoleReviewUnknownCacheLayer struct{ CacheObjectLayer }

func TestConsoleVersionAuthorizationCapabilityScope(t *testing.T) {
	savedObject, savedCache, savedGateway := newObjectLayerFn(), newCachedObjectLayerFn(), globalIsGateway
	t.Cleanup(func() { setObjectLayer(savedObject); setCacheObjectLayer(savedCache); globalIsGateway = savedGateway })
	setCacheObjectLayer(nil)
	globalIsGateway = false
	setObjectLayer(&FSObjects{})
	setCacheObjectLayer(&consoleReviewUnknownCacheLayer{})
	if consoleVersionAuthorizationSupported() {
		t.Fatal("unknown cache advertised version authorization")
	}
	setCacheObjectLayer(&cacheObjects{})
	if !consoleVersionAuthorizationSupported() {
		t.Fatal("native cache lost version authorization capability")
	}
	setCacheObjectLayer(nil)
	for _, obj := range []ObjectLayer{&FSObjects{}, &erasureServerPools{}} {
		setObjectLayer(obj)
		if !consoleVersionAuthorizationSupported() {
			t.Fatal("native backend lost version authorization capability")
		}
	}
	globalIsGateway = true
	if consoleVersionAuthorizationSupported() {
		t.Fatal("gateway advertised unsupported version authorization")
	}
	globalIsGateway = false
	for _, obj := range []ObjectLayer{nil, &consoleReviewUnknownObjectLayer{}, (*FSObjects)(nil), (*erasureServerPools)(nil)} {
		setObjectLayer(obj)
		if consoleVersionAuthorizationSupported() {
			t.Fatal("unknown backend advertised version authorization")
		}
	}
}
