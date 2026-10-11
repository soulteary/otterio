// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"context"
	"errors"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/valyala/fasthttp"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func userRelation(t *testing.T, sys *IAMSys) iamRelationshipsSnapshot {
	t.Helper()
	sys.store.rlock()
	defer sys.store.runlock()
	r, err := sys.nativeRelationshipsLocked("user", "group-parent", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func userFixture(t *testing.T) (*IAMSys, iamMutationGuard) {
	sys, guard := relationshipServiceFixture(t)
	sys.iamUsersMap["parent-sts"] = auth.Credentials{AccessKey: "parent-sts", SecretKey: "parent-sts-secret", ParentUser: "group-parent", SessionToken: "sts-token", Expiration: time.Now().Add(time.Hour), Status: auth.AccountOn}
	return sys, guard
}
func TestConsoleIAMRelationshipUserStatusAndCascade(t *testing.T) {
	sys, guard := userFixture(t)
	original := sys.iamUsersMap["group-parent"]
	for _, action := range []string{"user.disable", "user.enable", "user.delete"} {
		r := userRelation(t, sys)
		result, err := sys.mutateNativeRelationshipUser(context.Background(), iamRelationshipUserRequest{Action: action, Target: "group-parent"}, r.Revision, r.ExpiresAt, guard)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Impact.AffectedAccessKeys) != 5 || result.Impact.AffectedAccessKeys[0] != "group-parent" {
			t.Fatalf("child impact lost: %+v", result.Impact)
		}
		if action != "user.delete" {
			current := sys.iamUsersMap["group-parent"]
			if current.SecretKey != original.SecretKey || current.SessionToken != original.SessionToken || len(sys.iamGroupsMap["mutable-group"].Members) != 1 {
				t.Fatal("status changed secret/token/group")
			}
		}
	}
	for _, key := range []string{"group-parent", "parent-service", "managed-service", "sibling-service", "parent-sts"} {
		if _, ok := sys.iamUsersMap[key]; ok {
			t.Fatal("cascade credential retained")
		}
	}
	if len(sys.iamGroupsMap["mutable-group"].Members) != 0 {
		t.Fatal("deleted user retained groupmembership")
	}
}

type userCascadeFailStore struct {
	consoleIAMBindingMemoryStore
	failKey string
}

func (s *userCascadeFailStore) deleteUserIdentity(_ context.Context, key string, _ IAMUserType) error {
	if key == s.failKey {
		return errors.New("injected cascade failure")
	}
	return nil
}
func TestConsoleIAMRelationshipUserPartialCascadeUnknown(t *testing.T) {
	sys, guard := userFixture(t)
	sys.store = &userCascadeFailStore{failKey: "parent-sts"}
	r := userRelation(t, sys)
	_, err := sys.mutateNativeRelationshipUser(context.Background(), iamRelationshipUserRequest{Action: "user.delete", Target: "group-parent"}, r.Revision, r.ExpiresAt, guard)
	if !errors.Is(err, errIAMRelationshipCommitted) {
		t.Fatal("partial cascade was definitive rejection")
	}
	if _, ok := sys.iamUsersMap["managed-service"]; ok {
		t.Fatal("already deleted child retained memory")
	}
	if _, ok := sys.iamUsersMap["parent-sts"]; !ok {
		t.Fatal("failed child deletion omitted memory")
	}
	if _, ok := sys.iamUsersMap["group-parent"]; !ok {
		t.Fatal("unreached parent deletion omitted")
	}
	if len(sys.iamGroupsMap["mutable-group"].Members) != 0 {
		t.Fatal("successful group step omitted")
	}
}
func TestConsoleIAMRelationshipUserCASAndFreshActor(t *testing.T) {
	sys, guard := userFixture(t)
	r := userRelation(t, sys)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sys.mutateNativeRelationshipUser(context.Background(), iamRelationshipUserRequest{Action: "user.disable", Target: "group-parent"}, r.Revision, r.ExpiresAt, guard)
			errs <- err
		}()
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
		t.Fatal("graphCAS allowed both mutations")
	}
	r = userRelation(t, sys)
	if _, err := sys.mutateNativeRelationshipUser(context.Background(), iamRelationshipUserRequest{Action: "user.enable", Target: "group-parent"}, r.Revision, r.ExpiresAt.Add(time.Second), guard); !errors.Is(err, errIAMBindingConflict) {
		t.Fatal("expiry extension accepted")
	}
	sys.CreateUser("status-manager", madmin.UserInfo{SecretKey: "status-manager-secret", Status: madmin.AccountEnabled})
	p, err := iampolicy.ParseConfig(strings.NewReader(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:EnableUser","admin:DeleteUser","admin:GetUser","admin:ListUsers"],"Resource":["arn:aws:s3:::*"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	sys.SetPolicy("status-manager-allow", *p)
	sys.PolicyDBSet("status-manager", "status-manager-allow", false)
	manager := iamMutationGuard{credential: sys.iamUsersMap["status-manager"], args: iampolicy.Args{AccountName: "status-manager"}}
	r = userRelation(t, sys)
	if _, err := sys.mutateNativeRelationshipUser(context.Background(), iamRelationshipUserRequest{Action: "user.enable", Target: "group-parent"}, r.Revision, r.ExpiresAt, manager); err != nil {
		t.Fatal(err)
	}
	r = userRelation(t, sys)
	for _, change := range []string{"secret", "status", "deny"} {
		old := sys.iamUsersMap["status-manager"]
		if change == "secret" {
			c := old
			c.SecretKey = "revoked-manager-secret"
			sys.iamUsersMap[c.AccessKey] = c
		}
		if change == "status" {
			c := old
			c.Status = auth.AccountOff
			sys.iamUsersMap[c.AccessKey] = c
		}
		if change == "deny" {
			p, _ := iampolicy.ParseConfig(strings.NewReader(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["admin:EnableUser"],"Resource":["arn:aws:s3:::*"]}]}`))
			sys.SetPolicy("status-manager-deny", *p)
			sys.AddUsersToGroup("status-deny", []string{"status-manager"})
			sys.PolicyDBSet("status-deny", "status-manager-deny", true)
		}
		if _, err := sys.mutateNativeRelationshipUser(context.Background(), iamRelationshipUserRequest{Action: "user.disable", Target: "group-parent"}, r.Revision, r.ExpiresAt, manager); !errors.Is(err, errIAMActionNotAllowed) {
			t.Fatalf("stale %s actor authorized: %v", change, err)
		}
		sys.iamUsersMap[old.AccessKey] = old
	}
}
func TestConsoleIAMRelationshipUserSignedHandler(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bed.TearDown()
	globalIAMSys.InitStore(bed.objLayer)
	globalIAMSys.CreateUser("group-parent", madmin.UserInfo{SecretKey: "signed-parent-secret", Status: madmin.AccountEnabled})
	path := adminPathPrefix + adminAPIVersionPrefix + "/iam-relationship-users?" + url.Values{"kind": {"user"}, "target": {"group-parent"}}.Encode()
	for _, test := range []struct{ action, status string }{{"user.disable", "disabled"}, {"user.enable", "enabled"}, {"user.delete", "deleted"}} {
		r := userRelation(t, globalIAMSys)
		body := []byte(`{"action":"` + test.action + `","target":"group-parent"}`)
		req, err := newTestSignedRequestV4(http.MethodPut, path, int64(len(body)), bytes.NewReader(body), globalActiveCred.AccessKey, globalActiveCred.SecretKey, map[string]string{iamImpactCondition: r.Revision, iamImpactExpires: r.ExpiresAt.Format(time.RFC3339Nano)})
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		adminAPIHandlers{}.IAMRelationshipUsers(response, req)
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"`+test.status+`"`) || strings.Contains(response.Body.String(), "signed-parent-secret") {
			t.Fatalf("signed handler rejected/leaked: %d %s", response.Code, response.Body.String())
		}
	}
	for _, body := range []string{`{"action":"user.disable","target":"x","policy":""}`, `{"action":"user.delete","target":"x","target":"y"}`, `{"action":"user.rotate","target":"x"}`} {
		if _, err := decodeRelationshipUserBody([]byte(body)); err == nil {
			t.Fatal("ambiguous/nonstatus body accepted")
		}
	}
	for _, version := range []string{adminAPIVersionPrefix, adminAPIVersionV2Prefix} {
		var h fasthttp.RequestHeader
		h.SetMethod("PUT")
		h.SetRequestURI(adminPathPrefix + version + "/iam-relationship-users?kind=user&target=native")
		cfg := configurationRequestConfig(&h)
		if cfg.ReadTimeout != 15*time.Second || cfg.MaxRequestBodySize != 4096 {
			t.Fatal("user status missing headerbound")
		}
	}
}
func TestConsoleIAMRelationshipUserRejectsUnknownCascadeAndNonNative(t *testing.T) {
	sys, guard := userFixture(t)
	sys.iamUsersMap["unrecognized-child"] = auth.Credentials{AccessKey: "unrecognized-child", SecretKey: "unrecognized-child-secret", ParentUser: "group-parent", Status: auth.AccountOn, Expiration: time.Now().Add(time.Hour)}
	// A linked temporary identity is valid; a malformed linked identity must not
	// be silently dropped or reclassified as a native user by the cascade.
	child := sys.iamUsersMap["unrecognized-child"]
	child.SessionToken = ""
	sys.iamUsersMap[child.AccessKey] = child
	r := userRelation(t, sys)
	if _, err := sys.mutateNativeRelationshipUser(context.Background(), iamRelationshipUserRequest{Action: "user.delete", Target: "group-parent"}, r.Revision, r.ExpiresAt, guard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("unknown linked credential cascaded")
	}
	if len(sys.iamGroupsMap["mutable-group"].Members) != 1 {
		t.Fatal("preflight rejection changed group")
	}
	delete(sys.iamUsersMap, child.AccessKey)
	for _, target := range []string{"managed-service", "parent-sts", globalActiveCred.AccessKey} {
		if target == globalActiveCred.AccessKey {
			sys.iamUsersMap[target] = globalActiveCred
		}
		// Exact token expiry must be retained; mutation still refuses target kind.
		expiry := time.Now().Add(20 * time.Second)
		sys.store.rlock()
		token, err := sys.nativeRelationshipTokenLocked("user", target, expiry)
		sys.store.runlock()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sys.mutateNativeRelationshipUser(context.Background(), iamRelationshipUserRequest{Action: "user.disable", Target: target}, token, expiry, guard); !errors.Is(err, errIAMActionNotAllowed) {
			t.Fatalf("non-native/root target accepted: %s %v", target, err)
		}
	}
}
