// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

func TestConsoleIAMRelationshipUserSecretAtomicStatusAndCaller(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	for _, name := range []string{"secret-manager", "secret-target"} {
		if err := sys.CreateUser(name, madmin.UserInfo{SecretKey: name + "-old-secret", Status: madmin.AccountEnabled}); err != nil {
			t.Fatal(err)
		}
	}
	var allowed iampolicy.Policy
	json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:CreateUser","admin:GetUser","admin:ListUsers"],"Resource":["arn:aws:s3:::*"]}]}`), &allowed)
	sys.SetPolicy("secret-admin", allowed)
	sys.PolicyDBSet("secret-manager", "secret-admin", false)
	sys.SetUserStatus("secret-target", madmin.AccountDisabled)
	sys.iamUsersMap["secret-child"] = auth.Credentials{AccessKey: "secret-child", SecretKey: "child-old-secret", ParentUser: "secret-target", Status: auth.AccountOn}
	guard := iamMutationGuard{credential: sys.iamUsersMap["secret-manager"], args: iampolicy.Args{AccountName: "secret-manager", Action: iampolicy.CreateUserAdminAction}}
	snapshots := func(target string) (iamBindingSnapshot, iamRelationshipsSnapshot) {
		binding, err := sys.nativeBindings("user", target)
		if err != nil {
			t.Fatal(err)
		}
		sys.store.rlock()
		relation, err := sys.nativeRelationshipsLocked("user", target, time.Now())
		sys.store.runlock()
		if err != nil {
			t.Fatal(err)
		}
		return binding, relation
	}
	binding, relation := snapshots("secret-target")
	result, err := sys.rotateNativeRelationshipUserSecret(context.Background(), "secret-target", binding.Revision, relation.Revision, relation.ExpiresAt, "target-new-secret", guard)
	if err != nil || result.Outcome != "confirmed" || len(result.Impact.AffectedAccessKeys) != 2 || sys.iamUsersMap["secret-target"].Status != auth.AccountOff || sys.iamUsersMap["secret-target"].SecretKey != "target-new-secret" {
		t.Fatalf("disabled target not preserved: %v", err)
	}
	serialized, _ := json.Marshal(result)
	if strings.Contains(string(serialized), "target-new-secret") || strings.Contains(string(serialized), "child-old-secret") {
		t.Fatal("secret leaked in acknowledgement")
	}
	if _, err := sys.rotateNativeRelationshipUserSecret(context.Background(), "secret-target", binding.Revision, relation.Revision, relation.ExpiresAt, "another-secret", guard); !errors.Is(err, errIAMBindingConflict) {
		t.Fatal("stale target secret revision accepted")
	}
	// Caller secret rotation after HTTP authentication is a conclusive rejection.
	binding, relation = snapshots("secret-target")
	caller := sys.iamUsersMap["secret-manager"]
	caller.SecretKey = "manager-changed-secret"
	sys.iamUsersMap[caller.AccessKey] = caller
	if _, err := sys.rotateNativeRelationshipUserSecret(context.Background(), "secret-target", binding.Revision, relation.Revision, relation.ExpiresAt, "rejected-secret", guard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("stale caller accepted")
	}
	guard.credential = caller
	binding, relation = snapshots("secret-target")
	// A new child invalidates the graph even if target bindings did not change.
	sys.iamUsersMap["new-child"] = auth.Credentials{AccessKey: "new-child", SecretKey: "new-child-secret", ParentUser: "secret-target", Status: auth.AccountOn}
	if _, err := sys.rotateNativeRelationshipUserSecret(context.Background(), "secret-target", binding.Revision, relation.Revision, relation.ExpiresAt, "rejected-secret", guard); !errors.Is(err, errIAMBindingConflict) {
		t.Fatal("child creation raced through target CAS")
	}
	if sys.iamUsersMap["secret-target"].SecretKey != "target-new-secret" {
		t.Fatal("rejected rotation persisted")
	}
	// Two simultaneous rotations using one authoritative plan cannot both commit.
	binding, relation = snapshots("secret-target")
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sys.rotateNativeRelationshipUserSecret(context.Background(), "secret-target", binding.Revision, relation.Revision, relation.ExpiresAt, "concurrent-new-secret", guard)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else if errors.Is(err, errIAMBindingConflict) {
				conflict++
			} else {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if success != 1 || conflict != 1 {
		t.Fatal("secret mutation not atomic")
	}
	// Native manager may rotate its own credential without changing its policies.
	binding, relation = snapshots("secret-manager")
	if _, err := sys.rotateNativeRelationshipUserSecret(context.Background(), "secret-manager", binding.Revision, relation.Revision, relation.ExpiresAt, "manager-self-secret", guard); err != nil {
		t.Fatalf("self rotation: %v", err)
	}
	guard.credential = sys.iamUsersMap["secret-manager"]
	var deny iampolicy.Policy
	json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["admin:CreateUser"],"Resource":["arn:aws:s3:::*"]}]}`), &deny)
	sys.SetPolicy("secret-deny", deny)
	sys.AddUsersToGroup("secret-deny-group", []string{"secret-manager"})
	sys.PolicyDBSet("secret-deny-group", "secret-deny", true)
	binding, relation = snapshots("secret-target")
	if _, err := sys.rotateNativeRelationshipUserSecret(context.Background(), "secret-target", binding.Revision, relation.Revision, relation.ExpiresAt, "deny-secret", guard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("group deny lost to direct allow")
	}
	service := guard
	service.credential = sys.iamUsersMap["secret-child"]
	if _, err := sys.rotateNativeRelationshipUserSecret(context.Background(), "secret-target", binding.Revision, relation.Revision, relation.ExpiresAt, "service-secret", service); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("service child inherited manager capability")
	}
}
func TestConsoleIAMRelationshipUserSecretSignedHandler(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bed.TearDown()
	globalIAMSys.InitStore(bed.objLayer)
	// The Fiber test adapter has a virtual socket without deadline methods.
	// Verify route registration before exercising the real signed Handler directly.
	smoke := httptest.NewRecorder()
	routeApp := fiber.New()
	registerAdminRouterFiber(routeApp, false, true)
	fiberHTTPTestHandler(routeApp).ServeHTTP(smoke, httptest.NewRequest(http.MethodPut, adminPathPrefix+adminAPIVersionPrefix+"/iam-relationship-user-secret?kind=unsupported&target=route-target", nil))
	if smoke.Code != 400 {
		t.Fatalf("secret route absent or bypassed validation: %d", smoke.Code)
	}

	globalIAMSys.CreateUser("secret-router-target", madmin.UserInfo{SecretKey: "router-original-secret", Status: madmin.AccountDisabled})
	binding, _ := globalIAMSys.nativeBindings("user", "secret-router-target")
	globalIAMSys.store.rlock()
	relation, _ := globalIAMSys.nativeRelationshipsLocked("user", "secret-router-target", time.Now())
	globalIAMSys.store.runlock()
	path := adminPathPrefix + adminAPIVersionPrefix + "/iam-relationship-user-secret?" + url.Values{"kind": {"user"}, "target": {"secret-router-target"}}.Encode()
	encrypted, err := madmin.EncryptData(globalActiveCred.SecretKey, []byte(`{"newSecretKey":"router-new-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{iamBindingCondition: binding.Revision, iamImpactCondition: relation.Revision, iamImpactExpires: relation.ExpiresAt.UTC().Format(time.RFC3339Nano)}
	request, err := newTestSignedRequestV4(http.MethodPut, path, int64(len(encrypted)), bytes.NewReader(encrypted), globalActiveCred.AccessKey, globalActiveCred.SecretKey, headers)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	adminAPIHandlers{}.IAMRelationshipUserSecret(response, request)
	if response.Code != 200 || strings.Contains(response.Body.String(), "router-new-secret") {
		t.Fatalf("encrypted signed rotation rejected/leaked: %d %s", response.Code, response.Body.String())
	}
	credential := globalIAMSys.iamUsersMap["secret-router-target"]
	if credential.Status != auth.AccountOff || credential.SecretKey != "router-new-secret" {
		t.Fatal("rotation changed disabled status")
	}
}
