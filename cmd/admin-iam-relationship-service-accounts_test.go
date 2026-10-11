// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

func relationshipServiceFixture(t *testing.T) (*IAMSys, iamMutationGuard) {
	t.Helper()
	sys, guard := relationshipGroupFixture(t)
	for _, key := range []string{"managed-service", "sibling-service"} {
		if _, err := sys.NewServiceAccount(context.Background(), "group-parent", nil, newServiceAccountOpts{accessKey: key, secretKey: key + "-secret"}); err != nil {
			t.Fatal(err)
		}
	}
	return sys, guard
}
func serviceRelation(t *testing.T, sys *IAMSys) iamRelationshipsSnapshot {
	t.Helper()
	sys.store.rlock()
	defer sys.store.runlock()
	result, err := sys.nativeRelationshipsLocked("service-account", "managed-service", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestConsoleIAMRelationshipServiceStatusSecretDelete(t *testing.T) {
	sys, guard := relationshipServiceFixture(t)
	original := sys.iamUsersMap["managed-service"]
	for _, action := range []string{"service-account.disable", "service-account.rotate", "service-account.enable", "service-account.delete"} {
		relation := serviceRelation(t, sys)
		args := iamRelationshipServiceRequest{Action: action, Target: "managed-service"}
		if action == "service-account.rotate" {
			args.NewSecretKey = "replacement-service-secret"
		}
		result, err := sys.mutateNativeRelationshipServiceAccount(context.Background(), args, relation.Revision, relation.ExpiresAt, guard)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if len(result.Impact.AffectedAccessKeys) != 1 || result.Impact.AffectedAccessKeys[0] != "managed-service" || result.Impact.NativeParent != "group-parent" {
			t.Fatal("scope expanded or native parent lost")
		}
		if _, ok := sys.iamUsersMap["sibling-service"]; !ok {
			t.Fatal("sibling changed")
		}
		if action == "service-account.rotate" {
			current := sys.iamUsersMap["managed-service"]
			if current.Status != auth.AccountOff || current.SessionToken != original.SessionToken || current.SecretKey != args.NewSecretKey {
				t.Fatal("rotation changed status/restriction")
			}
		}
		if action == "service-account.delete" && result.ServiceAccount.Status != "deleted" {
			t.Fatal("delete ack missing")
		}
	}
	if _, ok := sys.iamUsersMap["managed-service"]; ok {
		t.Fatal("deleted credential retained")
	}
}
func TestConsoleIAMRelationshipServiceCASAndRevokedActor(t *testing.T) {
	sys, guard := relationshipServiceFixture(t)
	relation := serviceRelation(t, sys)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, action := range []string{"concurrent-secret-one", "concurrent-secret-two"} {
		wg.Add(1)
		go func(action string) {
			defer wg.Done()
			_, err := sys.mutateNativeRelationshipServiceAccount(context.Background(), iamRelationshipServiceRequest{Action: "service-account.rotate", Target: "managed-service", NewSecretKey: action}, relation.Revision, relation.ExpiresAt, guard)
			errs <- err
		}(action)
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, errIAMBindingConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("unexpected outcomes")
	}
	relation = serviceRelation(t, sys)
	revoked := guard
	revoked.credential.SecretKey = "retired-root-secret"
	if _, err := sys.mutateNativeRelationshipServiceAccount(context.Background(), iamRelationshipServiceRequest{Action: "service-account.disable", Target: "managed-service"}, relation.Revision, relation.ExpiresAt, revoked); err == nil {
		t.Fatal("retired signing credential accepted")
	}
	sys.store.lock()
	c := sys.iamUsersMap["group-parent"]
	c.SecretKey = "changed-parent-secret"
	sys.iamUsersMap["group-parent"] = c
	sys.store.unlock()
	if _, err := sys.mutateNativeRelationshipServiceAccount(context.Background(), iamRelationshipServiceRequest{Action: "service-account.disable", Target: "managed-service"}, relation.Revision, relation.ExpiresAt, guard); !errors.Is(err, errIAMBindingConflict) {
		t.Fatal("parent graph change not coupled")
	}
}
func TestConsoleIAMRelationshipServicePolicyRestriction(t *testing.T) {
	sys, guard := relationshipServiceFixture(t)
	allowed := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::allowed/*"]}]}`
	policy, err := iampolicy.ParseConfig(strings.NewReader(allowed))
	if err != nil {
		t.Fatal(err)
	}
	sys.iamPolicyDocsMap["parent-data"] = *policy
	sys.iamUserPolicyMap["group-parent"] = newMappedPolicy("parent-data")
	relation := serviceRelation(t, sys)
	result, err := sys.mutateNativeRelationshipServiceAccount(context.Background(), iamRelationshipServiceRequest{Action: "service-account.policy", Target: "managed-service", Policy: allowed}, relation.Revision, relation.ExpiresAt, guard)
	if err != nil || result.ServiceAccount.ImpliedPolicy || result.ServiceAccount.Policy == "" {
		t.Fatalf("valid restriction failed: %v", err)
	}
	if sys.iamUserPolicyMap["group-parent"].Policies != "parent-data" {
		t.Fatal("parent policy changed")
	}
	token := sys.iamUsersMap["managed-service"].SessionToken
	relation = serviceRelation(t, sys)
	widened := strings.ReplaceAll(allowed, "allowed/*", "forbidden/*")
	if _, err := sys.mutateNativeRelationshipServiceAccount(context.Background(), iamRelationshipServiceRequest{Action: "service-account.policy", Target: "managed-service", Policy: widened}, relation.Revision, relation.ExpiresAt, guard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("parent escalation accepted")
	}
	if sys.iamUsersMap["managed-service"].SessionToken != token {
		t.Fatal("rejected restriction changed credential")
	}
	sys.store.lock()
	c := sys.iamUsersMap["managed-service"]
	c.ParentUser = "external-subject"
	sys.iamUsersMap[c.AccessKey] = c
	sys.store.unlock()
	sys.store.rlock()
	_, err = sys.nativeRelationshipsLocked("service-account", "managed-service", time.Now())
	sys.store.runlock()
	if err == nil {
		t.Fatal("external parent inferred")
	}
}
func TestConsoleIAMRelationshipServiceSignedEncryptedHandler(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bed.TearDown()
	globalIAMSys.InitStore(bed.objLayer)
	if err := globalIAMSys.CreateUser("group-parent", madmin.UserInfo{SecretKey: "signed-parent-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := globalIAMSys.NewServiceAccount(ctx, "group-parent", nil, newServiceAccountOpts{accessKey: "managed-service", secretKey: "signed-child-secret"}); err != nil {
		t.Fatal(err)
	}
	relation := serviceRelation(t, globalIAMSys)
	body, err := madmin.EncryptData(globalActiveCred.SecretKey, []byte(`{"action":"service-account.rotate","target":"managed-service","newSecretKey":"signed-new-child-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	path := adminPathPrefix + adminAPIVersionPrefix + "/iam-relationship-service-accounts?" + url.Values{"kind": {"service-account"}, "target": {"managed-service"}}.Encode()
	request, err := newTestSignedRequestV4(http.MethodPut, path, int64(len(body)), bytes.NewReader(body), globalActiveCred.AccessKey, globalActiveCred.SecretKey, map[string]string{iamImpactCondition: relation.Revision, iamImpactExpires: relation.ExpiresAt.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	adminAPIHandlers{}.IAMRelationshipServiceAccounts(response, request)
	if response.Code != 200 || strings.Contains(response.Body.String(), "signed-new-child-secret") || strings.Contains(response.Body.String(), "sessionToken") {
		t.Fatalf("signed secret mutation rejected/leaked: %d %s", response.Code, response.Body.String())
	}
	for _, body := range []string{`{"action":"service-account.enable","target":"x","policy":""}`, `{"action":"service-account.rotate","target":"x","newSecretKey":null}`, `{"action":"service-account.policy","target":"x","policy":""}`, `{"action":"service-account.delete","target":"x","target":"y"}`} {
		if _, err := decodeIAMRelationshipServiceBody([]byte(body)); err == nil {
			t.Fatal("ambiguous/action-inappropriate body accepted")
		}
	}
}
func TestConsoleIAMRelationshipServiceNativeManagerDeny(t *testing.T) {
	sys, guard := relationshipServiceFixture(t)
	sys.CreateUser("service-manager", madmin.UserInfo{SecretKey: "service-manager-secret", Status: madmin.AccountEnabled})
	policy, err := iampolicy.ParseConfig(strings.NewReader(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:UpdateServiceAccount","admin:GetUser","admin:ListUsers","admin:ListServiceAccounts"],"Resource":["arn:aws:s3:::*"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	sys.SetPolicy("service-manager-allow", *policy)
	sys.PolicyDBSet("service-manager", "service-manager-allow", false)
	manager := iamMutationGuard{credential: sys.iamUsersMap["service-manager"], args: iampolicy.Args{AccountName: "service-manager"}}
	relation := serviceRelation(t, sys)
	args := iamRelationshipServiceRequest{Action: "service-account.disable", Target: "managed-service"}
	if _, err := sys.mutateNativeRelationshipServiceAccount(context.Background(), args, relation.Revision, relation.ExpiresAt, manager); err != nil {
		t.Fatal(err)
	}
	deny, err := iampolicy.ParseConfig(strings.NewReader(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["admin:UpdateServiceAccount"],"Resource":["arn:aws:s3:::*"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	sys.SetPolicy("service-manager-deny", *deny)
	sys.AddUsersToGroup("service-manager-deny-group", []string{"service-manager"})
	sys.PolicyDBSet("service-manager-deny-group", "service-manager-deny", true)
	relation = serviceRelation(t, sys)
	if _, err := sys.mutateNativeRelationshipServiceAccount(context.Background(), args, relation.Revision, relation.ExpiresAt, manager); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("inherited native deny ignored")
	}
	child := guard
	child.credential = sys.iamUsersMap["sibling-service"]
	child.args.AccountName = child.credential.AccessKey
	if _, err := sys.mutateNativeRelationshipServiceAccount(context.Background(), args, relation.Revision, relation.ExpiresAt, child); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("service actor gained management")
	}
}
