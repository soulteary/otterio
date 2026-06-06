/*
 * OtterIO Cloud Storage, (C) 2026 soulteary, https://github.com/soulteary/otterio
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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/policy"
	"github.com/soulteary/otterio/pkg/bucket/policy/condition"
)

// SECURITY: GHSA-95fr-cm4m-q5p9 / CVE-2024-36107 regression coverage.
//
// These tests pin the contract that GetObjectHandler / HeadObjectHandler
// re-evaluate IAM authorization with the existing object's tags injected
// into r.Header[X-Amz-Tagging] *before* checkPreconditions runs. If the
// second authorization check denies the request, the handler must return
// 403 with no Last-Modified / ETag / X-Amz-Version-Id leaked.

// TestGetConditionValuesExpandsObjectTagsForPolicyEvaluation pins the
// contract that getConditionValues parses the X-Amz-Tagging header and
// surfaces each tag under the s3:-stripped lookup keys
// "ExistingObjectTag/<k>" and "RequestObjectTag/<k>" used by
// stringEqualsFunc.evaluate. This is the policy-side primitive that lets
// handler-injected tags participate in IAM evaluation.
func TestGetConditionValuesExpandsObjectTagsForPolicyEvaluation(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/bucket/object", nil)
	r.Header.Set(xhttp.AmzObjectTagging, "dept=finance&customer=alice")

	values := getConditionValues(r, "", "", nil)

	cases := map[string]string{
		"ExistingObjectTag/dept":     "finance",
		"ExistingObjectTag/customer": "alice",
		"RequestObjectTag/dept":      "finance",
		"RequestObjectTag/customer":  "alice",
	}
	for key, want := range cases {
		got := values[key]
		if len(got) != 1 || got[0] != want {
			t.Errorf("expected condition values[%q]=[%q], got %v", key, want, got)
		}
	}

	// And the policy evaluation primitive itself: a StringEquals on
	// s3:ExistingObjectTag/dept must hit the values populated above.
	fn, err := condition.NewStringEqualsFunc(
		condition.Key("s3:ExistingObjectTag/dept"), "finance")
	if err != nil {
		t.Fatalf("NewStringEqualsFunc: %v", err)
	}
	conds := condition.NewFunctions(fn)
	if !conds.Evaluate(values) {
		t.Fatalf("expected policy condition to match injected tags, values=%v", values)
	}

	// Negative control: deny when the tag does not match.
	fnDeny, err := condition.NewStringEqualsFunc(
		condition.Key("s3:ExistingObjectTag/dept"), "engineering")
	if err != nil {
		t.Fatalf("NewStringEqualsFunc: %v", err)
	}
	if condition.NewFunctions(fnDeny).Evaluate(values) {
		t.Fatal("expected mismatched policy condition to NOT evaluate true")
	}
}

// installLeakDenyPolicy writes a bucket policy that allows anonymous
// GetObject on bucket/object generally, but denies GetObject when the
// existing object tag dept == secretValue. This reproduces the upstream
// leak attack surface: an anonymous caller would otherwise pass the first
// IAM check (because no tags are in the request), and only the second
// check (after ObjectInfo loads) can reject the request.
func installLeakDenyPolicy(t *testing.T, bucketName, objectName, secretValue string) {
	t.Helper()
	allow := policy.NewStatement(
		policy.Allow,
		policy.NewPrincipal("*"),
		policy.NewActionSet(policy.GetObjectAction),
		policy.NewResourceSet(policy.NewResource(bucketName, objectName)),
		condition.NewFunctions(),
	)
	denyFn, err := condition.NewStringEqualsFunc(
		condition.Key("s3:ExistingObjectTag/dept"), secretValue)
	if err != nil {
		t.Fatalf("NewStringEqualsFunc: %v", err)
	}
	deny := policy.NewStatement(
		policy.Deny,
		policy.NewPrincipal("*"),
		policy.NewActionSet(policy.GetObjectAction),
		policy.NewResourceSet(policy.NewResource(bucketName, objectName)),
		condition.NewFunctions(denyFn),
	)
	bp := &policy.Policy{
		Version:    policy.DefaultVersion,
		Statements: []policy.Statement{allow, deny},
	}
	configData, err := json.Marshal(bp)
	if err != nil {
		t.Fatalf("marshal bucket policy: %v", err)
	}
	if err := globalBucketMetadataSys.Update(bucketName, bucketPolicyConfig, configData); err != nil {
		t.Fatalf("install bucket policy: %v", err)
	}
}

// installAllowPolicy writes a bucket policy that unconditionally allows
// anonymous GetObject. Used by the positive-control test.
func installAllowPolicy(t *testing.T, bucketName, objectName string) {
	t.Helper()
	allow := policy.NewStatement(
		policy.Allow,
		policy.NewPrincipal("*"),
		policy.NewActionSet(policy.GetObjectAction),
		policy.NewResourceSet(policy.NewResource(bucketName, objectName)),
		condition.NewFunctions(),
	)
	bp := &policy.Policy{
		Version:    policy.DefaultVersion,
		Statements: []policy.Statement{allow},
	}
	configData, err := json.Marshal(bp)
	if err != nil {
		t.Fatalf("marshal bucket policy: %v", err)
	}
	if err := globalBucketMetadataSys.Update(bucketName, bucketPolicyConfig, configData); err != nil {
		t.Fatalf("install bucket policy: %v", err)
	}
}

// putTaggedObject puts a small object whose X-Amz-Tagging header value is
// persisted as the object's UserTags. This is what the handler later
// injects into r.Header so per-tag policies can be evaluated.
func putTaggedObject(t *testing.T, obj ObjectLayer, bucket, object, tagging string, body []byte) ObjectInfo {
	t.Helper()
	meta := map[string]string{}
	if tagging != "" {
		meta[xhttp.AmzObjectTagging] = tagging
	}
	oi, err := obj.PutObject(context.Background(), bucket, object,
		mustGetPutObjReader(t, bytes.NewReader(body), int64(len(body)), "", ""),
		ObjectOptions{UserDefined: meta})
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	return oi
}

// objectMetadataHeadersAbsent asserts the response carried by rec does not
// expose any of the per-object metadata fields that GHSA-95fr-cm4m-q5p9
// flagged as leaks. Called when the handler is supposed to have stopped
// before checkPreconditions could write them.
func objectMetadataHeadersAbsent(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	leakHeaders := []string{
		xhttp.LastModified,
		xhttp.ETag,
		xhttp.AmzVersionID,
		"Expires",
		"Cache-Control",
	}
	for _, h := range leakHeaders {
		if v := rec.Header().Get(h); v != "" {
			t.Errorf("GHSA-95fr-cm4m-q5p9: response leaked header %q=%q despite auth deny", h, v)
		}
	}
}

func TestGetObjectPreconditionDoesNotLeakOnAuthDeny(t *testing.T) {
	globalPolicySys = NewPolicySys()
	defer func() { globalPolicySys = nil }()

	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, testGetObjectPreconditionDoesNotLeakOnAuthDeny, []string{"GetObject", "PutObject"})
}

func testGetObjectPreconditionDoesNotLeakOnAuthDeny(obj ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, _ auth.Credentials, t *testing.T) {

	const (
		objectName  = "secret-object"
		secretValue = "classified"
	)
	_ = putTaggedObject(t, obj, bucketName, objectName, "dept="+secretValue, []byte("hello world"))
	installLeakDenyPolicy(t, bucketName, objectName, secretValue)

	req, err := newTestRequest(http.MethodGet, getGetObjectURL("", bucketName, objectName), 0, nil)
	if err != nil {
		t.Fatalf("newTestRequest: %v", err)
	}
	// If-None-Match: * is the canonical way to probe metadata via 304.
	// On a leaky implementation this would surface ETag / Last-Modified
	// even though the second-stage policy denies GetObject.
	req.Header.Set(xhttp.IfNoneMatch, "*")

	rec := httptest.NewRecorder()
	apiRouter.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("%s: expected 403 on tag-gated deny, got %d", instanceType, rec.Code)
	}
	objectMetadataHeadersAbsent(t, rec)
}

func TestHeadObjectPreconditionDoesNotLeakOnAuthDeny(t *testing.T) {
	globalPolicySys = NewPolicySys()
	defer func() { globalPolicySys = nil }()

	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, testHeadObjectPreconditionDoesNotLeakOnAuthDeny, []string{"HeadObject", "PutObject"})
}

func testHeadObjectPreconditionDoesNotLeakOnAuthDeny(obj ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, _ auth.Credentials, t *testing.T) {

	const (
		objectName  = "secret-head-object"
		secretValue = "classified"
	)
	_ = putTaggedObject(t, obj, bucketName, objectName, "dept="+secretValue, []byte("hello world"))
	installLeakDenyPolicy(t, bucketName, objectName, secretValue)

	req, err := newTestRequest(http.MethodHead, getHeadObjectURL("", bucketName, objectName), 0, nil)
	if err != nil {
		t.Fatalf("newTestRequest: %v", err)
	}
	req.Header.Set(xhttp.IfNoneMatch, "*")

	rec := httptest.NewRecorder()
	apiRouter.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("%s: expected 403 on tag-gated deny, got %d", instanceType, rec.Code)
	}
	objectMetadataHeadersAbsent(t, rec)
}

func TestGetObjectPreconditionStillWorksOnAuthAllow(t *testing.T) {
	globalPolicySys = NewPolicySys()
	defer func() { globalPolicySys = nil }()

	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, testGetObjectPreconditionStillWorksOnAuthAllow, []string{"GetObject", "PutObject"})
}

func testGetObjectPreconditionStillWorksOnAuthAllow(obj ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, _ auth.Credentials, t *testing.T) {

	const objectName = "public-object"
	oi := putTaggedObject(t, obj, bucketName, objectName, "" /* no tag */, []byte("hello"))
	installAllowPolicy(t, bucketName, objectName)

	// Step 1: anonymous GET should succeed (positive control: the
	// second-stage authorization must not over-eagerly deny allowed
	// requests).
	req, err := newTestRequest(http.MethodGet, getGetObjectURL("", bucketName, objectName), 0, nil)
	if err != nil {
		t.Fatalf("newTestRequest: %v", err)
	}
	rec := httptest.NewRecorder()
	apiRouter.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("%s: expected 200 on allowed GET, got %d", instanceType, rec.Code)
	}

	// Step 2: same request with If-None-Match matching the object's etag
	// should 304 and still expose ETag (this is the legitimate
	// precondition-check flow that the security fix must NOT break).
	if oi.ETag == "" {
		t.Fatalf("%s: putObject returned empty ETag, cannot exercise If-None-Match", instanceType)
	}
	condReq, err := newTestRequest(http.MethodGet, getGetObjectURL("", bucketName, objectName), 0, nil)
	if err != nil {
		t.Fatalf("newTestRequest: %v", err)
	}
	condReq.Header.Set(xhttp.IfNoneMatch, "\""+oi.ETag+"\"")

	condRec := httptest.NewRecorder()
	apiRouter.ServeHTTP(condRec, condReq)

	if condRec.Code != http.StatusNotModified {
		t.Fatalf("%s: expected 304 on If-None-Match match, got %d (headers=%v)",
			instanceType, condRec.Code, condRec.Header())
	}
	// 304 must still expose ETag/Last-Modified - this is the legitimate
	// precondition path that the security fix must NOT disturb.
	if condRec.Header().Get(xhttp.ETag) == "" && condRec.Header().Get(xhttp.LastModified) == "" {
		t.Errorf("%s: 304 response missing both ETag and Last-Modified, headers=%v",
			instanceType, condRec.Header())
	}
}
