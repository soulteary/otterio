/*
 * OtterIO Cloud Storage, (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

// AccountInfo only reads the bucket list and the optional usage snapshot. Keep
// those storage inputs deterministic while exercising real SigV4, JWT and IAM.
type accountInfoTestObjectLayer struct {
	ObjectLayer
	buckets []BucketInfo
}

func (o accountInfoTestObjectLayer) ListBuckets(context.Context) ([]BucketInfo, error) {
	return o.buckets, nil
}

func (o accountInfoTestObjectLayer) GetObjectNInfo(_ context.Context, bucket, object string, _ *HTTPRangeSpec, _ http.Header, _ LockType, _ ObjectOptions) (*GetObjectReader, error) {
	return nil, ObjectNotFound{Bucket: bucket, Object: object}
}

func TestAccountInfoOwnerAndScopedPrincipals(t *testing.T) {
	// These server globals are shared with the other handler tests, so this
	// fixture must remain serial and restore every global it replaces.
	globalObjLayerMutex.Lock()
	savedObjectAPI := globalObjectAPI
	globalObjLayerMutex.Unlock()
	savedIAM, savedNotification := globalIAMSys, globalNotificationSys
	savedCred, savedOPA := globalActiveCred, globalPolicyOPA
	savedDNS, savedFederation := globalDNSConfig, globalBucketFederation
	t.Cleanup(func() {
		globalObjLayerMutex.Lock()
		globalObjectAPI = savedObjectAPI
		globalObjLayerMutex.Unlock()
		globalIAMSys, globalNotificationSys = savedIAM, savedNotification
		globalActiveCred, globalPolicyOPA = savedCred, savedOPA
		globalDNSConfig, globalBucketFederation = savedDNS, savedFederation
	})

	globalActiveCred = auth.Credentials{AccessKey: "account-info-root", SecretKey: "account-info-root-secret"}
	globalPolicyOPA = nil
	globalDNSConfig, globalBucketFederation = nil, false
	globalNotificationSys = &NotificationSys{}
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	objectAPI := accountInfoTestObjectLayer{buckets: []BucketInfo{
		{Name: "read-bucket", Created: created},
		{Name: "write-bucket", Created: created},
		{Name: "hidden-bucket", Created: created},
	}}
	globalObjLayerMutex.Lock()
	globalObjectAPI = objectAPI
	globalObjLayerMutex.Unlock()
	globalIAMSys = NewIAMSys()
	// No persistence or background goroutines are needed for this handler.
	globalIAMSys.store = newIAMObjectStore(objectAPI)
	close(globalIAMSys.configLoaded)

	const scopedDoc = `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::read-bucket"]},
		{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::write-bucket/*"]},
		{"Effect":"Deny","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::read-bucket/*"]},
		{"Effect":"Deny","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::write-bucket"]}
	]}`
	const sessionDoc = `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::read-bucket"]}
	]}`
	scopedPolicy := *parseTestPolicy(t, scopedDoc)
	globalIAMSys.iamPolicyDocsMap["account-info-scoped"] = scopedPolicy
	globalIAMSys.iamPolicyDocsMap["account-info-parent"] = iampolicy.ReadWrite
	// A mutable canned policy must not replace the root's built-in policy.
	globalIAMSys.iamPolicyDocsMap["consoleAdmin"] = scopedPolicy
	regular := auth.Credentials{AccessKey: "account-info-user", SecretKey: "account-info-user-secret"}
	parent := auth.Credentials{AccessKey: "account-info-parent", SecretKey: "account-info-parent-secret"}
	globalIAMSys.iamUsersMap[regular.AccessKey] = regular
	globalIAMSys.iamUserPolicyMap[regular.AccessKey] = newMappedPolicy("account-info-scoped")
	globalIAMSys.iamUsersMap[parent.AccessKey] = parent
	globalIAMSys.iamUserPolicyMap[parent.AccessKey] = newMappedPolicy("account-info-parent")

	request := func(t *testing.T, cred auth.Credentials, wantOwner bool) *httptest.ResponseRecorder {
		t.Helper()
		headers := map[string]string{}
		if cred.IsTemp() {
			headers[xhttp.AmzSecurityToken] = cred.SessionToken
		}
		req, err := newTestSignedRequestV4(http.MethodGet, adminPathPrefix+adminAPIVersionPrefix+"/accountinfo", 0, nil, cred.AccessKey, cred.SecretKey, headers)
		if err != nil {
			t.Fatal(err)
		}
		_, _, owner, authErr := validateAdminSignature(req.Context(), req, "")
		if authErr != ErrNone || owner != wantOwner {
			t.Fatalf("fixture authentication: code=%v owner=%v, want owner=%v", authErr, owner, wantOwner)
		}
		rec := httptest.NewRecorder()
		adminAPIHandlers{}.AccountInfoHandler(rec, req)
		return rec
	}
	readAccount := func(t *testing.T, cred auth.Credentials, owner bool) madmin.AccountInfo {
		t.Helper()
		rec := request(t, cred, owner)
		if rec.Code != http.StatusOK {
			t.Fatalf("AccountInfo status=%d body=%s", rec.Code, rec.Body.String())
		}
		var info madmin.AccountInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
		if info.AccountName != cred.AccessKey {
			t.Fatalf("account name=%q, want requesting principal %q", info.AccountName, cred.AccessKey)
		}
		return info
	}
	assertAccess := func(t *testing.T, info madmin.AccountInfo, want map[string]madmin.AccountAccess) {
		t.Helper()
		got := make(map[string]madmin.AccountAccess)
		for _, bucket := range info.Buckets {
			if _, exists := got[bucket.Name]; exists {
				t.Fatalf("duplicate bucket %q", bucket.Name)
			}
			if !bucket.Created.Equal(created) {
				t.Fatalf("bucket %q creation time=%v", bucket.Name, bucket.Created)
			}
			got[bucket.Name] = bucket.Access
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("bucket access=%v, want %v", got, want)
		}
	}
	assertPolicy := func(t *testing.T, got, want iampolicy.Policy) {
		t.Helper()
		gotJSON, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("policy=%s, want %s", gotJSON, wantJSON)
		}
	}

	for _, sysType := range []UsersSysType{OtterIOUsersSysType, LDAPUsersSysType} {
		t.Run("root/"+string(sysType), func(t *testing.T) {
			globalIAMSys.usersSysType = sysType
			defer func() { globalIAMSys.usersSysType = OtterIOUsersSysType }()
			info := readAccount(t, globalActiveCred, true)
			assertAccess(t, info, map[string]madmin.AccountAccess{
				"read-bucket":   {Read: true, Write: true},
				"write-bucket":  {Read: true, Write: true},
				"hidden-bucket": {Read: true, Write: true},
			})
			assertPolicy(t, info.Policy, iampolicy.Admin)
			if _, found := globalIAMSys.iamUsersMap[globalActiveCred.AccessKey]; found {
				t.Fatal("root credential was inserted into IAM")
			}
		})
	}
	// Pin the original cause: root is still deliberately absent from IAM.
	if _, err := globalIAMSys.PolicyDBGet(globalActiveCred.AccessKey, false); !errors.Is(err, errNoSuchUser) {
		t.Fatalf("root IAM lookup=%v, want errNoSuchUser", err)
	}

	t.Run("regular-user", func(t *testing.T) {
		info := readAccount(t, regular, false)
		assertAccess(t, info, map[string]madmin.AccountAccess{
			"read-bucket": {Read: true}, "write-bucket": {Write: true},
		})
		assertPolicy(t, info.Policy, scopedPolicy)
	})

	newSessionCred := func(t *testing.T, service bool, mismatchedClaim bool) auth.Credentials {
		t.Helper()
		claims := map[string]interface{}{
			iampolicy.SessionPolicyName: base64.StdEncoding.EncodeToString([]byte(sessionDoc)),
		}
		if service {
			claims[parentClaim] = parent.AccessKey
			if mismatchedClaim {
				claims[parentClaim] = globalActiveCred.AccessKey
			}
			claims[iamPolicyClaimNameSA()] = "embedded-policy"
		} else {
			claims[expClaim] = time.Now().Add(time.Hour).Unix()
			claims[iamPolicyClaimNameOpenID()] = "account-info-parent"
			if mismatchedClaim {
				claims[iamPolicyClaimNameOpenID()] = "account-info-scoped"
			}
		}
		cred, err := auth.GetNewCredentialsWithMetadata(claims, globalActiveCred.SecretKey)
		if err != nil {
			t.Fatal(err)
		}
		cred.ParentUser = parent.AccessKey
		globalIAMSys.iamUsersMap[cred.AccessKey] = cred
		if !service {
			globalIAMSys.iamUserPolicyMap[cred.AccessKey] = newMappedPolicy("account-info-parent")
		}
		return cred
	}
	for _, service := range []bool{false, true} {
		name := "sts"
		if service {
			name = "service-account"
		}
		t.Run(name+"/session-policy", func(t *testing.T) {
			cred := newSessionCred(t, service, false)
			info := readAccount(t, cred, false)
			assertAccess(t, info, map[string]madmin.AccountAccess{"read-bucket": {Read: true}})
		})
		t.Run(name+"/mismatched-subject-claim", func(t *testing.T) {
			cred := newSessionCred(t, service, true)
			info := readAccount(t, cred, false)
			assertAccess(t, info, map[string]madmin.AccountAccess{})
		})
	}

	t.Run("root-invalid-signature", func(t *testing.T) {
		req, err := newTestSignedRequestV4(http.MethodGet, adminPathPrefix+adminAPIVersionPrefix+"/accountinfo", 0, nil, globalActiveCred.AccessKey, "wrong-account-info-secret", nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		adminAPIHandlers{}.AccountInfoHandler(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("invalid root signature status=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}
