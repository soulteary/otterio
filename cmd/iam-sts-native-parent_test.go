// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

func TestSTSNativeParentCurrentAuthority(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	parent := auth.Credentials{AccessKey: "sts-parent", SecretKey: "parent-secret", Status: auth.AccountOn}
	child := auth.Credentials{AccessKey: "sts-child", SecretKey: "child-secret", Status: auth.AccountOn, ParentUser: parent.AccessKey, SessionToken: "test-token", Expiration: time.Now().Add(time.Hour)}
	sys.iamUsersMap[parent.AccessKey] = parent
	sys.iamUsersMap[child.AccessKey] = child
	sys.iamPolicyDocsMap["issued"] = iampolicy.ReadOnly
	sys.iamPolicyDocsMap["current"] = iampolicy.ReadOnly
	sys.iamUserPolicyMap[child.AccessKey] = newMappedPolicy("issued")
	sys.iamUserPolicyMap[parent.AccessKey] = newMappedPolicy("current")
	args := iampolicy.Args{AccountName: child.AccessKey, Action: iampolicy.GetObjectAction, BucketName: "native-bucket", ObjectName: "plain.txt", Claims: map[string]interface{}{iamPolicyClaimNameOpenID(): "issued"}}
	check := func(want bool) {
		t.Helper()
		if got := sys.IsAllowedSTS(args, parent.AccessKey); got != want {
			t.Fatalf("native STS allow=%v expected=%v", got, want)
		}
	}
	check(true)
	child.Expiration = time.Now().Add(-time.Hour)
	sys.iamUsersMap[child.AccessKey] = child
	check(false)
	child.Expiration = time.Now().Add(time.Hour)
	sys.iamUsersMap[child.AccessKey] = child
	parent.Status = auth.AccountOff
	sys.iamUsersMap[parent.AccessKey] = parent
	check(false)
	parent.Status = auth.AccountOn
	sys.iamUsersMap[parent.AccessKey] = parent
	check(true)
	parent.Expiration = time.Now().Add(-time.Hour)
	sys.iamUsersMap[parent.AccessKey] = parent
	check(false)
	parent.Expiration = time.Time{}
	sys.iamUsersMap[parent.AccessKey] = parent
	delete(sys.iamUsersMap, parent.AccessKey)
	check(false)
	sys.iamUsersMap[parent.AccessKey] = parent
	check(true)
	delete(sys.iamUserPolicyMap, parent.AccessKey)
	args.DenyOnly = true
	check(false)
	args.DenyOnly = false
	sys.iamUserPolicyMap[parent.AccessKey] = newMappedPolicy("current")
	var deny iampolicy.Policy
	if err := json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::native-bucket/*"]}]}`), &deny); err != nil {
		t.Fatal(err)
	}
	sys.iamPolicyDocsMap["deny"] = deny
	if err := sys.AddUsersToGroup("current-deny", []string{parent.AccessKey}); err != nil {
		t.Fatal(err)
	}
	sys.iamGroupPolicyMap["current-deny"] = newMappedPolicy("deny")
	check(false)
	delete(sys.iamGroupPolicyMap, "current-deny")
	check(true)
	var conditioned iampolicy.Policy
	if err := json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::native-bucket/*"],"Condition":{"StringEquals":{"aws:username":"sts-parent"}}}]}`), &conditioned); err != nil {
		t.Fatal(err)
	}
	sys.iamPolicyDocsMap["current"] = conditioned
	args.ConditionValues = map[string][]string{"username": {child.AccessKey}, "userid": {child.AccessKey}, "principaltype": {"AssumedRole"}}
	check(true)
	if args.ConditionValues["username"][0] != child.AccessKey {
		t.Fatal("parent check mutated child request conditions")
	}
	if err := json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::native-bucket/*"],"Condition":{"StringEquals":{"aws:principaltype":"AssumedRole"}}}]}`), &deny); err != nil {
		t.Fatal(err)
	}
	sys.iamPolicyDocsMap["deny"] = deny
	sys.iamGroupPolicyMap["current-deny"] = newMappedPolicy("deny")
	check(false)
	delete(sys.iamGroupPolicyMap, "current-deny")
	check(true)
	// Deleting/recreating the parent cannot revive a removed child, even with
	// identical parent credentials and an old authenticated request context.
	delete(sys.iamUsersMap, child.AccessKey)
	delete(sys.iamUsersMap, parent.AccessKey)
	sys.iamUsersMap[parent.AccessKey] = parent
	check(false)
	sys.iamUsersMap[child.AccessKey] = child
	parent.ParentUser = "other-parent"
	sys.iamUsersMap[parent.AccessKey] = parent
	check(false)
	parent.ParentUser = ""
	parent.SessionToken = "parent-token"
	parent.Expiration = time.Now().Add(time.Hour)
	sys.iamUsersMap[parent.AccessKey] = parent
	check(false)
	// WebIdentity has no authoritative native parent; retain its original
	// claim + stored policy checks without inferring a native mapping.
	child.ParentUser = ""
	sys.iamUsersMap[child.AccessKey] = child
	if !sys.IsAllowedSTS(args, "") {
		t.Fatal("external empty-parent authority changed")
	}
	args.Claims[iamPolicyClaimNameOpenID()] = "unknown"
	if sys.IsAllowedSTS(args, "") {
		t.Fatal("external policy mismatch accepted")
	}
}

func TestSTSNativeParentSignedRouteStatusAndPolicy(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bed.TearDown()
	globalIAMSys.InitStore(bed.objLayer)
	s3App := newFiberApp()
	registerAPIRouterFiber(s3App)
	s3Router := fiberHTTPTestHandler(s3App)
	const parent = "signed-sts-parent"
	if err = globalIAMSys.CreateUser(parent, madmin.UserInfo{SecretKey: "signed-parent-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.SetPolicy("signed-sts-policy", iampolicy.ReadOnly); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet(parent, "signed-sts-policy", false); err != nil {
		t.Fatal(err)
	}
	child, err := auth.GetNewCredentialsWithMetadata(map[string]interface{}{iamPolicyClaimNameOpenID(): "signed-sts-policy", expClaim: time.Now().Add(time.Hour).Unix()}, globalActiveCred.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	child.ParentUser = parent
	if err = globalIAMSys.SetTempUser(child.AccessKey, child, "signed-sts-policy"); err != nil {
		t.Fatal(err)
	}
	const bucket = "signed-sts-native"
	if err = bed.objLayer.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	request := func(want int) {
		t.Helper()
		req, e := newTestSignedRequestV4(http.MethodGet, "/"+bucket+"?location", 0, bytes.NewReader(nil), child.AccessKey, child.SecretKey, map[string]string{"X-Amz-Security-Token": child.SessionToken})
		if e != nil {
			t.Fatal(e)
		}
		recorder := httptest.NewRecorder()
		s3Router.ServeHTTP(recorder, req)
		if recorder.Code != want {
			t.Fatalf("signed native STS status=%d expected=%d", recorder.Code, want)
		}
	}
	request(200)
	if err = globalIAMSys.SetUserStatus(parent, madmin.AccountDisabled); err != nil {
		t.Fatal(err)
	}
	request(403)
	if err = globalIAMSys.SetUserStatus(parent, madmin.AccountEnabled); err != nil {
		t.Fatal(err)
	}
	request(200)
	if err = globalIAMSys.PolicyDBSet(parent, "", false); err != nil {
		t.Fatal(err)
	}
	request(403)
	if err = globalIAMSys.PolicyDBSet(parent, "signed-sts-policy", false); err != nil {
		t.Fatal(err)
	}
	request(200)
	if err = globalIAMSys.DeleteUser(parent); err != nil {
		t.Fatal(err)
	}
	request(403)
	if err = globalIAMSys.CreateUser(parent, madmin.UserInfo{SecretKey: "signed-parent-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet(parent, "signed-sts-policy", false); err != nil {
		t.Fatal(err)
	}
	request(403)
}
