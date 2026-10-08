// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/otterio/cmd/config/policy/opa"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
	etcd "go.etcd.io/etcd/client/v3"
)

func selfCredentialsFixture(t *testing.T) auth.Credentials {
	t.Helper()
	obj, root, err := prepareFS(t)
	if err != nil {
		t.Fatal(err)
	}
	savedObj, savedIAM, savedNotify, savedCred, savedOPA := newObjectLayerFn(), globalIAMSys, globalNotificationSys, globalActiveCred, globalPolicyOPA
	t.Cleanup(func() {
		_ = obj.Shutdown(context.Background())
		setObjectLayer(savedObj)
		globalIAMSys, globalNotificationSys, globalActiveCred, globalPolicyOPA = savedIAM, savedNotify, savedCred, savedOPA
		removeRoots([]string{root})
	})
	setObjectLayer(obj)
	globalNotificationSys = &NotificationSys{}
	globalPolicyOPA = nil
	globalActiveCred = auth.Credentials{AccessKey: "self-root", SecretKey: "self-root-secret"}
	globalIAMSys = NewIAMSys()
	globalIAMSys.store = newIAMObjectStore(obj)
	close(globalIAMSys.configLoaded)
	user := auth.Credentials{AccessKey: "self-user", SecretKey: "self-original-secret", Status: auth.AccountOn, Groups: []string{"retained-group"}}
	globalIAMSys.iamUsersMap[user.AccessKey] = user
	globalIAMSys.iamGroupsMap["retained-group"] = GroupInfo{Status: statusEnabled}
	globalIAMSys.iamPolicyDocsMap["self-readonly"] = iampolicy.ReadOnly
	globalIAMSys.iamUserPolicyMap[user.AccessKey] = newMappedPolicy("self-readonly")
	return user
}

func selfCredentialsRequest(t *testing.T, method string, cred auth.Credentials, document string) *http.Request {
	t.Helper()
	var data []byte
	var err error
	if document != "" {
		data, err = madmin.EncryptData(cred.SecretKey, []byte(document))
		if err != nil {
			t.Fatal(err)
		}
	}
	headers := map[string]string{}
	if cred.IsTemp() || cred.IsServiceAccount() {
		headers[xhttp.AmzSecurityToken] = cred.SessionToken
	}
	r, err := newTestSignedRequestV4(method, adminPathPrefix+adminAPIVersionPrefix+"/self-credentials", int64(len(data)), bytes.NewReader(data), cred.AccessKey, cred.SecretKey, headers)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSelfCredentialsHTTPContract(t *testing.T) {
	user := selfCredentialsFixture(t)
	api := adminAPIHandlers{}
	get := func(cred auth.Credentials) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodGet, cred, ""))
		return rec
	}
	info := get(user)
	if info.Code != 200 || info.Header().Get(selfCredentialsHeader) != "v1" {
		t.Fatalf("GET=%d %s", info.Code, info.Body.String())
	}
	var dto selfCredentialsInfo
	if err := json.Unmarshal(info.Body.Bytes(), &dto); err != nil {
		t.Fatal(err)
	}
	if dto.Kind != "iam" || dto.Status != "enabled" || !dto.CanRotateSecret {
		t.Fatalf("GET info=%+v", dto)
	}
	for _, secret := range []string{user.AccessKey, user.SecretKey, "Policy", "sessionToken"} {
		if strings.Contains(info.Body.String(), secret) {
			t.Fatalf("response exposed private field %q", secret)
		}
	}
	for _, doc := range []string{`{"newSecretKey":"valid-replacement","status":"enabled"}`, `{"NewSecretKey":"valid-replacement"}`, `{"newSecretKey":"one-secret","newSecretKey":"two-secret"}`, `{"newSecretKey":"short"}`, `{"newSecretKey":"valid-replacement"} {}`, `{"newSecretKey":"nul-secret\u0000"}`, `{"newSecretKey":"line-secret\r\n"}`, `{"newSecretKey":"orphan-secret\ud800"}`, `{"newSecretKey":"orphan-secret\udc00"}`, "{\"newSecretKey\":\"raw-secret-\xff\"}", `{"newSecretKey":"` + strings.Repeat("界", 43) + `"}`} {
		rec := httptest.NewRecorder()
		api.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodPut, user, doc))
		if rec.Code != 400 {
			t.Fatalf("invalid secret document accepted: status=%d", rec.Code)
		}
		if globalIAMSys.iamUsersMap[user.AccessKey].SecretKey != user.SecretKey {
			t.Fatal("invalid secret encoding changed the credential")
		}
	}
	// A valid encrypted envelope with a valid signature over the wrong body
	// digest must not rotate the key. Admin authentication only wraps a reader.
	for _, digest := range []string{strings.Repeat("0", 64), "duplicate"} {
		r := selfCredentialsRequest(t, http.MethodPut, user, `{"newSecretKey":"digest-must-not-save"}`)
		r.Header.Del("Content-Md5")
		if digest == "duplicate" {
			r.Header.Add("X-Amz-Content-Sha256", r.Header.Get("X-Amz-Content-Sha256"))
		} else {
			r.Header.Set("X-Amz-Content-Sha256", digest)
		}
		if err := signRequestV4(r, user.AccessKey, user.SecretKey); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		api.SelfCredentials(rec, r)
		if rec.Code != 400 || get(user).Code != 200 || globalIAMSys.iamUsersMap[user.AccessKey].SecretKey != user.SecretKey {
			t.Fatalf("invalid encrypted-body digest accepted: %d", rec.Code)
		}
	}
	root := get(globalActiveCred)
	if root.Code != 200 || !strings.Contains(root.Body.String(), `"kind":"root"`) || strings.Contains(root.Body.String(), `"canRotateSecret":true`) {
		t.Fatalf("root contract: %d %s", root.Code, root.Body.String())
	}
	rec := httptest.NewRecorder()
	api.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodPut, globalActiveCred, `{"newSecretKey":"root-new-secret"}`))
	if rec.Code != 403 {
		t.Fatalf("root rotation=%d", rec.Code)
	}
	// The deny applies even though regular users have implicit self permission.
	globalIAMSys.iamPolicyDocsMap["self-denied"] = *parseTestPolicy(t, `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["admin:CreateUser"]}]}`)
	globalIAMSys.iamUserPolicyMap[user.AccessKey] = newMappedPolicy("self-denied")
	denied := get(user)
	if denied.Code != 200 || strings.Contains(denied.Body.String(), `"canRotateSecret":true`) {
		t.Fatal("explicit deny not reflected")
	}
	rec = httptest.NewRecorder()
	api.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodPut, user, `{"newSecretKey":"self-replacement-secret"}`))
	if rec.Code != 403 {
		t.Fatalf("denied rotation=%d", rec.Code)
	}
	globalIAMSys.iamUserPolicyMap[user.AccessKey] = newMappedPolicy("self-readonly")
	// Successful rotation has no credentials in its response; old signature dies.
	rec = httptest.NewRecorder()
	api.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodPut, user, `{"newSecretKey":"self-replacement-secret"}`))
	if rec.Code != 204 || rec.Body.Len() != 0 {
		t.Fatalf("rotation=%d %s", rec.Code, rec.Body.String())
	}
	if stale := get(user); stale.Code != 403 {
		t.Fatalf("old secret still authenticates: %d", stale.Code)
	}
	updated := user
	updated.SecretKey = "self-replacement-secret"
	if fresh := get(updated); fresh.Code != 200 {
		t.Fatalf("new secret GET=%d %s", fresh.Code, fresh.Body.String())
	}
	globalIAMSys.store.rlock()
	actual := globalIAMSys.iamUsersMap[user.AccessKey]
	mapped := globalIAMSys.iamUserPolicyMap[user.AccessKey]
	globalIAMSys.store.runlock()
	if !reflect.DeepEqual(actual, updated) || !reflect.DeepEqual(mapped, newMappedPolicy("self-readonly")) {
		t.Fatal("rotation changed status, groups or policy")
	}
}

func TestSelfCredentialsIdentityClasses(t *testing.T) {
	user := selfCredentialsFixture(t)
	for _, service := range []bool{false, true} {
		claims := map[string]interface{}{}
		if service {
			claims[parentClaim] = user.AccessKey
			claims[iamPolicyClaimNameSA()] = "inherited-policy"
		} else {
			claims[expClaim] = time.Now().Add(time.Hour).Unix()
			claims[iamPolicyClaimNameOpenID()] = "self-readonly"
		}
		cred, err := auth.GetNewCredentialsWithMetadata(claims, globalActiveCred.SecretKey)
		if err != nil {
			t.Fatal(err)
		}
		cred.ParentUser = user.AccessKey
		globalIAMSys.iamUsersMap[cred.AccessKey] = cred
		if !service {
			globalIAMSys.iamUserPolicyMap[cred.AccessKey] = newMappedPolicy("self-readonly")
		}
		name := "sts"
		if service {
			name = "service"
		}
		rec := httptest.NewRecorder()
		adminAPIHandlers{}.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodGet, cred, ""))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"kind":"`+name+`"`) || strings.Contains(rec.Body.String(), `"canRotateSecret":true`) {
			t.Fatalf("%s GET=%d %s", name, rec.Code, rec.Body.String())
		}
		rec = httptest.NewRecorder()
		adminAPIHandlers{}.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodPut, cred, `{"newSecretKey":"self-replacement-secret"}`))
		if rec.Code != 403 {
			t.Fatalf("%s rotated secret: %d", name, rec.Code)
		}
	}
	globalIAMSys.usersSysType = LDAPUsersSysType
	rec := httptest.NewRecorder()
	adminAPIHandlers{}.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodGet, user, ""))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"kind":"directory"`) {
		t.Fatalf("directory GET=%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	adminAPIHandlers{}.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodPut, user, `{"newSecretKey":"self-replacement-secret"}`))
	if rec.Code != 403 {
		t.Fatal("directory rotated secret")
	}
}

func TestSelfCredentialsAtomicRotation(t *testing.T) {
	user := selfCredentialsFixture(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = globalIAMSys.RotateSelfSecret(context.Background(), user.AccessKey, user.SecretKey, "parallel-new-secret", iampolicy.Args{})
		}(i)
	}
	wg.Wait()
	success := 0
	for _, err := range errs {
		if err == nil {
			success++
		} else if !errors.Is(err, errSelfSecretConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("parallel winners=%d", success)
	}
	// A concurrently committed disable must never be overwritten by rotation.
	for range 10 {
		globalIAMSys.store.lock()
		globalIAMSys.iamUsersMap[user.AccessKey] = user
		globalIAMSys.store.unlock()
		var rotate, disable error
		wg.Add(2)
		go func() {
			defer wg.Done()
			rotate = globalIAMSys.RotateSelfSecret(context.Background(), user.AccessKey, user.SecretKey, "race-new-secret", iampolicy.Args{})
		}()
		go func() { defer wg.Done(); disable = globalIAMSys.SetUserStatus(user.AccessKey, madmin.AccountDisabled) }()
		wg.Wait()
		if disable != nil || (rotate != nil && !errors.Is(rotate, errIAMActionNotAllowed)) {
			t.Fatalf("disable=%v rotate=%v", disable, rotate)
		}
		globalIAMSys.store.rlock()
		status := globalIAMSys.iamUsersMap[user.AccessKey].Status
		globalIAMSys.store.runlock()
		if status != auth.AccountOff {
			t.Fatal("rotation re-enabled concurrently disabled user")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := globalIAMSys.RotateSelfSecret(ctx, user.AccessKey, user.SecretKey, "after-cancel-secret", iampolicy.Args{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled mutation=%v", err)
	}
	globalIAMSys.store.lock()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	start := time.Now()
	err := globalIAMSys.RotateSelfSecret(waitCtx, user.AccessKey, user.SecretKey, "while-locked-secret", iampolicy.Args{})
	waitCancel()
	globalIAMSys.store.unlock()
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("IAM lock ignored cancellation: %v duration=%v", err, time.Since(start))
	}
}

func TestSelfCredentialsRevocationAndUnsupportedTopology(t *testing.T) {
	user := selfCredentialsFixture(t)
	if !globalIAMSys.IsAllowed(iampolicy.Args{AccountName: user.AccessKey, Action: iampolicy.CreateUserAdminAction, DenyOnly: true}) {
		t.Fatal("fixture self permission missing")
	}
	globalIAMSys.store.lock()
	globalIAMSys.iamPolicyDocsMap["revoked"] = *parseTestPolicy(t, `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["admin:CreateUser"]}]}`)
	globalIAMSys.iamUserPolicyMap[user.AccessKey] = newMappedPolicy("revoked")
	globalIAMSys.store.unlock()
	if err := globalIAMSys.RotateSelfSecret(context.Background(), user.AccessKey, user.SecretKey, "revocation-new-secret", iampolicy.Args{}); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatalf("revoked policy mutation=%v", err)
	}
	globalIAMSys.store.lock()
	globalIAMSys.iamUserPolicyMap[user.AccessKey] = newMappedPolicy("self-readonly")
	globalIAMSys.store.unlock()
	savedDist, savedEtcd, savedOPA := globalIsDistErasure, globalEtcdClient, globalPolicyOPA
	t.Cleanup(func() { globalIsDistErasure, globalEtcdClient, globalPolicyOPA = savedDist, savedEtcd, savedOPA })
	for _, topology := range []string{"distributed", "etcd", "opa"} {
		globalIsDistErasure, globalEtcdClient, globalPolicyOPA = false, nil, nil
		switch topology {
		case "distributed":
			globalIsDistErasure = true
		case "etcd":
			globalEtcdClient = &etcd.Client{}
		case "opa":
			globalPolicyOPA = &opa.Opa{}
		}
		rec := httptest.NewRecorder()
		adminAPIHandlers{}.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodGet, user, ""))
		if rec.Code != 200 || strings.Contains(rec.Body.String(), `"canRotateSecret":true`) {
			t.Fatalf("%s GET rotation hint=%d %s", topology, rec.Code, rec.Body.String())
		}
		rec = httptest.NewRecorder()
		adminAPIHandlers{}.SelfCredentials(rec, selfCredentialsRequest(t, http.MethodPut, user, `{"newSecretKey":"topology-new-secret"}`))
		if rec.Code != 403 {
			t.Fatalf("%s PUT=%d", topology, rec.Code)
		}
		if err := globalIAMSys.RotateSelfSecret(context.Background(), user.AccessKey, user.SecretKey, "topology-new-secret", iampolicy.Args{}); !errors.Is(err, errIAMActionNotAllowed) {
			t.Fatalf("%s IAM mutation=%v", topology, err)
		}
	}
	globalIsDistErasure, globalEtcdClient, globalPolicyOPA = false, nil, nil
	globalIAMSys.store.rlock()
	actual := globalIAMSys.iamUsersMap[user.AccessKey]
	globalIAMSys.store.runlock()
	if actual.SecretKey != user.SecretKey {
		t.Fatal("rejected operation changed persisted secret")
	}
}
