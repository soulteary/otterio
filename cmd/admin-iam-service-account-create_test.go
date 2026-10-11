// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/valyala/fasthttp"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNativeServiceCreationRejectsStaleParentAndActor(t *testing.T) {
	for _, change := range []string{"parent-disable", "parent-rotate", "parent-delete", "parent-recreate", "parent-disable-enable", "actor-rotate", "actor-disable", "collision"} {
		t.Run(change, func(t *testing.T) {
			sys, _ := relationshipGroupFixture(t)
			actor := sys.iamUsersMap["group-parent"]
			proof, err := sys.captureNativeServiceCreation(actor)
			if err != nil {
				t.Fatal(err)
			}
			proof, err = sys.captureNativeServiceParent(proof, "group-parent")
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "parent-disable", "actor-disable":
				err = sys.SetUserStatus("group-parent", madmin.AccountDisabled)
			case "parent-rotate", "actor-rotate":
				err = sys.SetUserSecretKey("group-parent", "replacement-secret")
			case "parent-delete":
				err = sys.DeleteUser("group-parent")
			case "parent-recreate":
				err = sys.DeleteUser("group-parent")
				if err == nil {
					err = sys.CreateUser("group-parent", madmin.UserInfo{SecretKey: actor.SecretKey, Status: madmin.AccountEnabled})
				}
			case "parent-disable-enable":
				err = sys.SetUserStatus("group-parent", madmin.AccountDisabled)
				if err == nil {
					err = sys.SetUserStatus("group-parent", madmin.AccountEnabled)
				}
			case "collision":
				sys.iamUsersMap["new-service-key"] = auth.Credentials{AccessKey: "new-service-key", SecretKey: "existing-secret", Status: auth.AccountOn}
			}
			if err != nil {
				t.Fatal(err)
			}
			args := iampolicy.Args{AccountName: actor.AccessKey, Action: iampolicy.CreateServiceAccountAdminAction, DenyOnly: true}
			_, err = sys.createNativeServiceAccount(context.Background(), proof, nil, newServiceAccountOpts{accessKey: "new-service-key", secretKey: "new-service-secret"}, iamMutationGuard{credential: actor, args: args}, args)
			if err == nil {
				t.Fatal("stale identity or existing key accepted")
			}
			if c, ok := sys.iamUsersMap["new-service-key"]; ok && (change != "collision" || c.SecretKey != "existing-secret") {
				t.Fatal("failed creation changed identity")
			}
		})
	}
}
func TestNativeServiceCreationSelfDenyOnlyAndMissingParent(t *testing.T) {
	sys, _ := relationshipGroupFixture(t)
	actor := sys.iamUsersMap["group-parent"]
	proof, err := sys.captureNativeServiceCreation(actor)
	if err != nil {
		t.Fatal(err)
	}
	proof, err = sys.captureNativeServiceParent(proof, actor.AccessKey)
	if err != nil {
		t.Fatal(err)
	}
	args := iampolicy.Args{AccountName: actor.AccessKey, Action: iampolicy.CreateServiceAccountAdminAction, DenyOnly: true}
	credential, err := sys.createNativeServiceAccount(context.Background(), proof, nil, newServiceAccountOpts{accessKey: "self-service-key", secretKey: "self-service-secret"}, iamMutationGuard{credential: actor, args: args}, args)
	if err != nil || credential.ParentUser != actor.AccessKey {
		t.Fatalf("own account default: %v", err)
	}
	if _, err = sys.NewServiceAccount(context.Background(), "missing-native", nil, newServiceAccountOpts{}); err == nil {
		t.Fatal("orphan accepted")
	}
}
func TestNativeRelationshipRevisionRejectsIdenticalRecreate(t *testing.T) {
	sys, _ := relationshipGroupFixture(t)
	// Empty native parent removes group/child differences from the ABA proof.
	if err := sys.CreateUser("aba-native", madmin.UserInfo{SecretKey: "same-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	sys.store.rlock()
	before, err := sys.nativeRelationshipsLocked("user", "aba-native", UTCNow())
	sys.store.runlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := sys.DeleteUser("aba-native"); err != nil {
		t.Fatal(err)
	}
	if err := sys.CreateUser("aba-native", madmin.UserInfo{SecretKey: "same-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	sys.store.rlock()
	after, err := sys.nativeRelationshipTokenLocked("user", "aba-native", before.ExpiresAt)
	sys.store.runlock()
	if err != nil || after == before.Revision {
		t.Fatalf("identical reincarnation reused graph proof: %v", err)
	}
}

type serviceCreationGate struct {
	reader           io.Reader
	entered, release chan struct{}
	started          bool
}

func (g *serviceCreationGate) Read(p []byte) (int, error) {
	if !g.started {
		g.started = true
		close(g.entered)
		<-g.release
	}
	return g.reader.Read(p)
}
func (g *serviceCreationGate) Close() error { return nil }
func TestNativeServiceCreationSignedInFlightFreshness(t *testing.T) {
	for _, change := range []string{"none", "root-native", "parent-disable", "parent-delete", "parent-recreate", "actor-secret", "actor-status", "group-deny"} {
		t.Run(change, func(t *testing.T) {
			skipIfWindowsAdminErasureTestBed(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bed, err := prepareAdminErasureTestBed(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer bed.TearDown()
			globalIAMSys.InitStore(bed.objLayer)
			if err = globalIAMSys.CreateUser("signed-native", madmin.UserInfo{SecretKey: "signed-native-secret", Status: madmin.AccountEnabled}); err != nil {
				t.Fatal(err)
			}
			var base iampolicy.Policy
			if err = json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::*/*"]}]}`), &base); err != nil {
				t.Fatal(err)
			}
			if err = globalIAMSys.SetPolicy("create-base", base); err != nil {
				t.Fatal(err)
			}
			if err = globalIAMSys.PolicyDBSet("signed-native", "create-base", false); err != nil {
				t.Fatal(err)
			}
			actor := globalIAMSys.iamUsersMap["signed-native"]
			if strings.HasPrefix(change, "parent-") || change == "root-native" {
				actor = globalActiveCred
			}
			data, err := json.Marshal(madmin.AddServiceAccountReq{TargetUser: "signed-native", AccessKey: "new-signed-service", SecretKey: "new-signed-secret"})
			if err != nil {
				t.Fatal(err)
			}
			body, err := madmin.EncryptData(actor.SecretKey, data)
			if err != nil {
				t.Fatal(err)
			}
			globalIAMSys.store.rlock()
			relation, err := globalIAMSys.nativeRelationshipsLocked("user", "signed-native", time.Now())
			globalIAMSys.store.runlock()
			if err != nil {
				t.Fatal(err)
			}
			req, err := newTestSignedRequestV4(http.MethodPut, adminPathPrefix+adminAPIVersionPrefix+"/add-service-account", int64(len(body)), bytes.NewReader(body), actor.AccessKey, actor.SecretKey, map[string]string{iamServiceCreationCapability: "v1", iamImpactCondition: relation.Revision, iamImpactExpires: relation.ExpiresAt.Format(time.RFC3339Nano)})
			if err != nil {
				t.Fatal(err)
			}
			gate := &serviceCreationGate{reader: req.Body, entered: make(chan struct{}), release: make(chan struct{})}
			req.Body = gate
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { defer close(done); adminAPIHandlers{}.AddServiceAccount(response, req) }()
			select {
			case <-gate.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not reach authenticated body")
			}
			switch change {
			case "parent-disable", "actor-status":
				err = globalIAMSys.SetUserStatus("signed-native", madmin.AccountDisabled)
			case "parent-delete":
				err = globalIAMSys.DeleteUser("signed-native")
			case "parent-recreate":
				err = globalIAMSys.DeleteUser("signed-native")
				if err == nil {
					err = globalIAMSys.CreateUser("signed-native", madmin.UserInfo{SecretKey: "signed-native-secret", Status: madmin.AccountEnabled})
				}
			case "actor-secret":
				err = globalIAMSys.SetUserSecretKey("signed-native", "changed-secret")
			case "group-deny":
				var p *iampolicy.Policy
				p, err = iampolicy.ParseConfig(strings.NewReader(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["admin:CreateServiceAccount"],"Resource":["arn:aws:s3:::*"]}]}`))
				if err == nil {
					err = globalIAMSys.SetPolicy("create-deny", *p)
				}
				if err == nil {
					err = globalIAMSys.AddUsersToGroup("create-denied-group", []string{"signed-native"})
				}
				if err == nil {
					err = globalIAMSys.PolicyDBSet("create-denied-group", "create-deny", true)
				}
			}
			close(gate.release)
			<-done
			if err != nil {
				t.Fatal(err)
			}
			if change == "none" || change == "root-native" {
				if response.Code != 200 || response.Header().Get(iamServiceCreationCapability) != "v1" {
					t.Fatalf("fresh signed self create: %d %s", response.Code, response.Body.String())
				}
			} else {
				if response.Code < 400 {
					t.Fatalf("stale signed create accepted %s: %d", change, response.Code)
				}
				if _, exists := globalIAMSys.iamUsersMap["new-signed-service"]; exists {
					t.Fatal("rejected request persisted child")
				}
			}
		})
	}
}

func TestNativeServiceCreationLockedCurrentGroupDeny(t *testing.T) {
	sys, _ := relationshipGroupFixture(t)
	actor := sys.iamUsersMap["group-parent"]
	proof, err := sys.captureNativeServiceCreation(actor)
	if err != nil {
		t.Fatal(err)
	}
	proof, err = sys.captureNativeServiceParent(proof, actor.AccessKey)
	if err != nil {
		t.Fatal(err)
	}
	var deny iampolicy.Policy
	if err = json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["admin:CreateServiceAccount"],"Resource":["arn:aws:s3:::*"]}]}`), &deny); err != nil {
		t.Fatal(err)
	}
	if err = sys.SetPolicy("late-create-deny", deny); err != nil {
		t.Fatal(err)
	}
	if err = sys.PolicyDBSet("mutable-group", "late-create-deny", true); err != nil {
		t.Fatal(err)
	}
	args := iampolicy.Args{AccountName: actor.AccessKey, Action: iampolicy.CreateServiceAccountAdminAction, DenyOnly: true}
	_, err = sys.createNativeServiceAccount(context.Background(), proof, nil, newServiceAccountOpts{accessKey: "late-deny-child", secretKey: "late-deny-secret"}, iamMutationGuard{credential: actor, args: args}, args)
	if err == nil {
		t.Fatal("current group explicit deny lost to own-account default")
	}
}

func TestNativeServiceCreationLegacyTemporarySelfBoundary(t *testing.T) {
	sys, _ := relationshipGroupFixture(t)
	temporary := auth.Credentials{AccessKey: "external-temp", SecretKey: "external-temp-secret", SessionToken: "token", Expiration: time.Now().Add(time.Hour), Status: auth.AccountOn}
	sys.iamUsersMap[temporary.AccessKey] = temporary
	if _, err := sys.NewServiceAccount(context.Background(), temporary.AccessKey, nil, newServiceAccountOpts{}); err == nil {
		t.Fatal("arbitrary temporary parent accepted")
	}
	if _, err := sys.NewServiceAccount(context.Background(), temporary.AccessKey, nil, newServiceAccountOpts{allowExternalParent: true}); err != nil {
		t.Fatalf("legacy temporary self rejected: %v", err)
	}
	temporary.Expiration = time.Now().Add(-time.Hour)
	sys.iamUsersMap[temporary.AccessKey] = temporary
	if _, err := sys.NewServiceAccount(context.Background(), temporary.AccessKey, nil, newServiceAccountOpts{allowExternalParent: true}); err == nil {
		t.Fatal("expired temporary self accepted")
	}
}

func TestNativeServiceCreationPreservesRootParentRejection(t *testing.T) {
	sys, guard := relationshipGroupFixture(t)
	proof, err := sys.captureNativeServiceCreation(guard.credential)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sys.captureNativeServiceParent(proof, globalActiveCred.AccessKey); err == nil {
		t.Fatal("root parent accepted by native capture")
	}
	if _, err = sys.NewServiceAccount(context.Background(), globalActiveCred.AccessKey, nil, newServiceAccountOpts{}); err == nil {
		t.Fatal("legacy root parent rejection changed")
	}
}

func TestNativeServiceCreationMarkerRequiresSignatureAndSingleVersion(t *testing.T) {
	for _, test := range []struct {
		values []string
		signed string
		ok     bool
	}{{[]string{"v1"}, "host;x-otterio-iam-service-creation", true}, {[]string{"v1"}, "host", false}, {[]string{"v2"}, "host;x-otterio-iam-service-creation", false}, {[]string{"v1", "v1"}, "host;x-otterio-iam-service-creation", false}} {
		req := httptest.NewRequest(http.MethodPut, "/", nil)
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=test, SignedHeaders="+test.signed+", Signature=test")
		for _, value := range test.values {
			req.Header.Add(iamServiceCreationCapability, value)
		}
		if (signedNativeServiceCreation(req) == nil) != test.ok {
			t.Fatal("marker signature/version ambiguity accepted")
		}
	}
}

func TestNativeServiceCreationSignedCapabilityRejectsUnsupportedMode(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bed.TearDown()
	globalIAMSys.InitStore(bed.objLayer)
	oldLDAP := globalLDAPConfig.Enabled
	defer func() { globalLDAPConfig.Enabled = oldLDAP }()
	globalLDAPConfig.Enabled = true
	body, err := madmin.EncryptData(globalActiveCred.SecretKey, []byte(`{"targetUser":"native-target"}`))
	if err != nil {
		t.Fatal(err)
	}
	req, err := newTestSignedRequestV4(http.MethodPut, adminPathPrefix+adminAPIVersionPrefix+"/add-service-account", int64(len(body)), bytes.NewReader(body), globalActiveCred.AccessKey, globalActiveCred.SecretKey, map[string]string{iamServiceCreationCapability: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	adminAPIHandlers{}.AddServiceAccount(response, req)
	if response.Code != 501 || response.Header().Get(iamServiceCreationCapability) != "" {
		t.Fatalf("unsupported mode claimed protected creation: %d", response.Code)
	}
}

func TestNativeServiceCreationReviewCASRejectsRecreatedParent(t *testing.T) {
	sys, guard := relationshipGroupFixture(t)
	if err := sys.CreateUser("review-parent", madmin.UserInfo{SecretKey: "review-parent-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	sys.store.rlock()
	relation, err := sys.nativeRelationshipsLocked("user", "review-parent", time.Now())
	sys.store.runlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = sys.DeleteUser("review-parent"); err != nil {
		t.Fatal(err)
	}
	if err = sys.CreateUser("review-parent", madmin.UserInfo{SecretKey: "review-parent-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	// HTTP authentication observes the NEW identity; browser review is OLD.
	proof, err := sys.captureNativeServiceCreation(guard.credential)
	if err != nil {
		t.Fatal(err)
	}
	proof, err = sys.captureNativeServiceParent(proof, "review-parent")
	if err != nil {
		t.Fatal(err)
	}
	proof.revision = relation.Revision
	proof.expires = relation.ExpiresAt
	args := iampolicy.Args{AccountName: guard.credential.AccessKey, Action: iampolicy.CreateServiceAccountAdminAction}
	_, err = sys.createNativeServiceAccount(context.Background(), proof, nil, newServiceAccountOpts{accessKey: "review-child", secretKey: "review-child-secret"}, iamMutationGuard{credential: guard.credential, args: args}, args)
	if err != errIAMBindingConflict {
		t.Fatalf("old review reused new parent: %v", err)
	}
	if _, ok := sys.iamUsersMap["review-child"]; ok {
		t.Fatal("CAS rejection wrote child")
	}
}

func TestNativeServiceCreationHeaderTransportLimits(t *testing.T) {
	for _, version := range []string{adminAPIVersionPrefix, adminAPIVersionV2Prefix} {
		var h fasthttp.RequestHeader
		h.SetMethod("PUT")
		h.SetRequestURI(adminPathPrefix + version + "/add-service-account")
		if configurationRequestLimit(&h) != 0 {
			t.Fatal("legacy creation transport changed")
		}
		h.Set(iamServiceCreationCapability, "v1")
		config := configurationRequestConfig(&h)
		if config.ReadTimeout != 15*time.Second || config.MaxRequestBodySize != 32*1024 {
			t.Fatal("protected creation missing header bound")
		}
	}
	req := httptest.NewRequest(http.MethodPut, "/", nil)
	req.ContentLength = 32*1024 + 1
	if _, err := readProtectedServiceCreation(context.Background(), req, "secret"); err == nil {
		t.Fatal("oversize protected body accepted")
	}
}

func TestNativeServiceCreationPartialParentDeleteRejectsOldRequest(t *testing.T) {
	sys, guard := userFixture(t)
	sys.store = &userCascadeFailStore{failKey: "managed-service"}
	proof, err := sys.captureNativeServiceCreation(guard.credential)
	if err != nil {
		t.Fatal(err)
	}
	proof, err = sys.captureNativeServiceParent(proof, "group-parent")
	if err != nil {
		t.Fatal(err)
	}
	relation := userRelation(t, sys)
	_, err = sys.mutateNativeRelationshipUser(context.Background(), iamRelationshipUserRequest{Action: "user.delete", Target: "group-parent"}, relation.Revision, relation.ExpiresAt, guard)
	if err != errIAMRelationshipCommitted {
		t.Fatalf("partial cascade classification: %v", err)
	}
	if len(sys.iamGroupsMap["mutable-group"].Members) != 0 {
		t.Fatal("successful unlink not reflected")
	}
	if _, ok := sys.iamUsersMap["managed-service"]; !ok {
		t.Fatal("first failed child was deleted")
	}
	args := iampolicy.Args{AccountName: guard.credential.AccessKey, Action: iampolicy.CreateServiceAccountAdminAction}
	_, err = sys.createNativeServiceAccount(context.Background(), proof, nil, newServiceAccountOpts{accessKey: "partial-new-child", secretKey: "partial-new-secret"}, iamMutationGuard{credential: guard.credential, args: args}, args)
	if err == nil {
		t.Fatal("partially deleted parent accepted old request")
	}
}
