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
	"sync"
	"testing"
	"time"

	"github.com/soulteary/otterio/cmd/config/policy/opa"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

// The in-memory persistence stub isolates store-lock tests from background
// erasure workers; authorization/routing uses the real server testbed below.
type consoleIAMBindingMemoryStore struct {
	IAMStorageAPI
	sync.RWMutex
}

func (s *consoleIAMBindingMemoryStore) lock()    { s.Lock() }
func (s *consoleIAMBindingMemoryStore) unlock()  { s.Unlock() }
func (s *consoleIAMBindingMemoryStore) rlock()   { s.RLock() }
func (s *consoleIAMBindingMemoryStore) runlock() { s.RUnlock() }
func (*consoleIAMBindingMemoryStore) saveMappedPolicy(context.Context, string, IAMUserType, bool, MappedPolicy, ...options) error {
	return nil
}
func (*consoleIAMBindingMemoryStore) deleteMappedPolicy(context.Context, string, IAMUserType, bool) error {
	return nil
}
func (*consoleIAMBindingMemoryStore) savePolicyDoc(context.Context, string, iampolicy.Policy) error {
	return nil
}
func (*consoleIAMBindingMemoryStore) saveUserIdentity(context.Context, string, IAMUserType, UserIdentity, ...options) error {
	return nil
}
func (*consoleIAMBindingMemoryStore) deleteUserIdentity(context.Context, string, IAMUserType) error {
	return nil
}
func (*consoleIAMBindingMemoryStore) saveGroupInfo(context.Context, string, GroupInfo) error {
	return nil
}
func (*consoleIAMBindingMemoryStore) deleteGroupInfo(context.Context, string) error { return nil }

func TestConsoleIAMBindingsCASUsesLegacyStoreLock(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	if err := sys.CreateUser("binding-user", madmin.UserInfo{SecretKey: "binding-user-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	var policy iampolicy.Policy
	if err := json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::bucket/*"]}]}`), &policy); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"binding-one", "binding-two"} {
		if err := sys.SetPolicy(name, policy); err != nil {
			t.Fatal(err)
		}
	}
	initial, err := sys.nativeBindings("user", "binding-user")
	if err != nil {
		t.Fatal(err)
	}
	if err := sys.PolicyDBSet("binding-user", "binding-one", false); err != nil {
		t.Fatal(err)
	}
	if _, err := sys.replaceNativeBindings("user", "binding-user", initial.Revision, []string{"binding-two"}); !errors.Is(err, errIAMBindingConflict) {
		t.Fatalf("legacy writer bypassed revision: %v", err)
	}
	current, err := sys.nativeBindings("user", "binding-user")
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsCh := make(chan error, 2)
	for _, policies := range [][]string{{"binding-two"}, {}} {
		wait.Add(1)
		go func(policies []string) {
			defer wait.Done()
			_, err := sys.replaceNativeBindings("user", "binding-user", current.Revision, policies)
			errorsCh <- err
		}(policies)
	}
	wait.Wait()
	close(errorsCh)
	successes, conflicts := 0, 0
	for err := range errorsCh {
		if err == nil {
			successes++
		} else if errors.Is(err, errIAMBindingConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("CAS accepted competing replacements: success=%d conflicts=%d", successes, conflicts)
	}
	if _, err := sys.nativeBindings("user", globalActiveCred.AccessKey); err == nil {
		t.Fatal("root credentials became mutable IAM target")
	}
}

func TestConsoleIAMBindingsRequiresSignedRevisionAndNativeMode(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bed.TearDown()
	globalIAMSys.InitStore(bed.objLayer)
	if err := globalIAMSys.CreateUser("binding-user", madmin.UserInfo{SecretKey: "binding-user-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	query := url.Values{"kind": {"user"}, "target": {"binding-user"}}
	request, err := buildAdminRequest(query, http.MethodGet, "/iam-policy-bindings", 0, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	bed.router.ServeHTTP(recorder, request)
	if recorder.Code != 200 || recorder.Header().Get(iamBindingCapability) != "v1" {
		t.Fatalf("missing capability: %d %s", recorder.Code, recorder.Body.String())
	}
	var binding iamBindingSnapshot
	if json.Unmarshal(recorder.Body.Bytes(), &binding) != nil || binding.Kind != "user" || binding.Target != "binding-user" || !binding.Conditional || len(binding.Revision) != 64 {
		t.Fatal("binding reply is not the expected JSON protocol")
	}
	for _, capability := range []struct{ route, header, flag string }{{"/iam-create", "X-Otterio-IAM-Create", "createOnly"}, {"/iam-user-secret", "X-Otterio-IAM-User-Secret", "secretOnly"}} {
		capRequest, err := buildAdminRequest(query, http.MethodGet, capability.route, 0, bytes.NewReader(nil))
		if err != nil {
			t.Fatal(err)
		}
		capResponse := httptest.NewRecorder()
		bed.router.ServeHTTP(capResponse, capRequest)
		var fields map[string]json.RawMessage
		var flag bool
		if capResponse.Code != 200 || capResponse.Header().Get(capability.header) != "v1" || json.Unmarshal(capResponse.Body.Bytes(), &fields) != nil || json.Unmarshal(fields[capability.flag], &flag) != nil || !flag {
			t.Fatalf("%s reply is not the expected JSON capability: %d", capability.route, capResponse.Code)
		}
	}
	revision := recorder.Header().Get(iamBindingRevision)
	body := []byte(`{"policies":[]}`)
	request, err = buildAdminRequest(query, http.MethodPut, "/iam-policy-bindings", int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(iamBindingCondition, revision) // Intentionally added after signing.
	recorder = httptest.NewRecorder()
	bed.router.ServeHTTP(recorder, request)
	if recorder.Code != 400 && recorder.Code != 403 {
		t.Fatalf("unsigned condition accepted: %d %s", recorder.Code, recorder.Body.String())
	}
	// Check topology eligibility without changing the mode of the live erasure
	// backend: its background workers also consult globalIsDistErasure.
	if !iamBindingSupportedFor(globalIAMSys, false, false) {
		t.Fatal("native single-writer store did not advertise CAS")
	}
	for _, topology := range []struct {
		name                       string
		distributed, externalStore bool
	}{
		{"distributed", true, false},
		{"etcd", false, true},
		{"distributed-etcd", true, true},
	} {
		if iamBindingSupportedFor(globalIAMSys, topology.distributed, topology.externalStore) {
			t.Fatalf("%s process lock advertised as CAS", topology.name)
		}
	}
}

func TestConsoleIAMBindingsRevisionCoversIdentityAndMembershipChanges(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	if err := sys.CreateUser("binding-user", madmin.UserInfo{SecretKey: "original-user-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	previous, err := sys.nativeBindings("user", "binding-user")
	if err != nil {
		t.Fatal(err)
	}
	changes := []func() error{
		func() error { return sys.SetUserSecretKey("binding-user", "replacement-user-secret") },
		func() error { return sys.SetUserStatus("binding-user", madmin.AccountDisabled) },
		func() error { return sys.AddUsersToGroup("binding-group", []string{"binding-user"}) },
		func() error { return sys.SetGroupStatus("binding-group", false) },
		func() error {
			if err := sys.DeleteUser("binding-user"); err != nil {
				return err
			}
			return sys.CreateUser("binding-user", madmin.UserInfo{SecretKey: "recreated-user-secret", Status: madmin.AccountEnabled})
		},
	}
	for _, change := range changes {
		if err := change(); err != nil {
			t.Fatal(err)
		}
		current, err := sys.nativeBindings("user", "binding-user")
		if err != nil {
			t.Fatal(err)
		}
		if current.Revision == previous.Revision {
			t.Fatal("identity or membership change did not invalidate draft")
		}
		if _, err := sys.replaceNativeBindings("user", "binding-user", previous.Revision, []string{}); !errors.Is(err, errIAMBindingConflict) {
			t.Fatalf("old identity draft accepted: %v", err)
		}
		previous = current
	}
}

func TestConsoleIAMBindingsCreateOnlyNeverOverwritesConcurrentTargets(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, secret := range []string{"first-user-secret", "second-user-secret"} {
		wait.Add(1)
		go func(secret string) {
			defer wait.Done()
			results <- sys.createNativeIAM("user", "created-user", iamCreateRequest{SecretKey: secret})
		}(secret)
	}
	wait.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, errIAMCreateExists) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("create raced with rotation: wins=%d conflicts=%d", wins, conflicts)
	}
	credential, _ := sys.GetUser("created-user")
	if err := sys.createNativeIAM("user", "created-user", iamCreateRequest{SecretKey: "overwrite-secret"}); !errors.Is(err, errIAMCreateExists) {
		t.Fatalf("existing user accepted: %v", err)
	}
	after, _ := sys.GetUser("created-user")
	if after.SecretKey != credential.SecretKey {
		t.Fatal("rejected creation changed existing secret")
	}
	for _, member := range []string{"member-one", "member-two"} {
		if err := sys.CreateUser(member, madmin.UserInfo{SecretKey: "member-user-secret", Status: madmin.AccountEnabled}); err != nil {
			t.Fatal(err)
		}
	}
	results = make(chan error, 2)
	for _, member := range []string{"member-one", "member-two"} {
		wait.Add(1)
		go func(member string) {
			defer wait.Done()
			results <- sys.createNativeIAM("group", "created-group", iamCreateRequest{Members: []string{member}})
		}(member)
	}
	wait.Wait()
	close(results)
	wins, conflicts = 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, errIAMCreateExists) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	group, err := sys.GetGroupDescription("created-group")
	if err != nil {
		t.Fatal(err)
	}
	if wins != 1 || conflicts != 1 || len(group.Members) != 1 {
		t.Fatalf("creation merged concurrent group members: %v wins=%d conflicts=%d", group.Members, wins, conflicts)
	}
}

func TestConsoleIAMBindingsSecretRotationPreservesCurrentStatusAndExistingType(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	if err := sys.CreateUser("rotate-user", madmin.UserInfo{SecretKey: "original-user-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err := sys.AddUsersToGroup("rotate-group", []string{"rotate-user"}); err != nil {
		t.Fatal(err)
	}
	// This status change happens after the browser's user list was loaded.
	if err := sys.SetUserStatus("rotate-user", madmin.AccountDisabled); err != nil {
		t.Fatal(err)
	}
	if err := sys.rotateNativeIAMSecret("rotate-user", "replacement-user-secret"); err != nil {
		t.Fatal(err)
	}
	credential, _ := sys.GetUser("rotate-user")
	if credential.Status != auth.AccountOff || credential.SecretKey != "replacement-user-secret" || !sys.iamUserGroupMemberships["rotate-user"].Contains("rotate-group") {
		t.Fatal("rotation changed authoritative status/membership")
	}
	if err := sys.DeleteUser("rotate-user"); err != nil {
		t.Fatal(err)
	}
	if err := sys.rotateNativeIAMSecret("rotate-user", "recreate-secret"); !errors.Is(err, errNoSuchUser) {
		t.Fatalf("rotation recreated deleted target: %v", err)
	}
	sys.iamUsersMap["rotate-user"] = auth.Credentials{AccessKey: "rotate-user", SecretKey: "service-secret", ParentUser: "parent-user", SessionToken: "service-token"}
	if err := sys.rotateNativeIAMSecret("rotate-user", "convert-service-secret"); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatalf("rotation converted service account: %v", err)
	}
	sys.iamUsersMap["rotate-user"] = auth.Credentials{AccessKey: "rotate-user", SecretKey: "sts-secret", SessionToken: "sts-token", Expiration: time.Now().Add(time.Hour)}
	if err := sys.rotateNativeIAMSecret("rotate-user", "convert-temp-secret"); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatalf("rotation converted temporary credential: %v", err)
	}
}

func TestConsoleIAMMutationGuardRejectsCredentialAndPolicyChanges(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	if err := sys.CreateUser("guard-user", madmin.UserInfo{SecretKey: "guard-user-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	var policy iampolicy.Policy
	if err := json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:CreateUser"],"Resource":["arn:aws:s3:::*"]}]}`), &policy); err != nil {
		t.Fatal(err)
	}
	if err := sys.SetPolicy("guard-policy", policy); err != nil {
		t.Fatal(err)
	}
	if err := sys.PolicyDBSet("guard-user", "guard-policy", false); err != nil {
		t.Fatal(err)
	}
	guard := iamMutationGuard{credential: sys.iamUsersMap["guard-user"], args: iampolicy.Args{AccountName: "guard-user", Action: iampolicy.CreateUserAdminAction}}
	if err := sys.createNativeIAM("user", "guard-created", iamCreateRequest{SecretKey: "created-user-secret"}, guard); err != nil {
		t.Fatalf("authorized guard: %v", err)
	}
	// A policy revoked after HTTP authorization must prevent persistence.
	if err := sys.PolicyDBSet("guard-user", "", false); err != nil {
		t.Fatal(err)
	}
	if err := sys.createNativeIAM("user", "guard-denied", iamCreateRequest{SecretKey: "created-user-secret"}, guard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatalf("revoked policy accepted: %v", err)
	}
	if _, ok := sys.iamUsersMap["guard-denied"]; ok {
		t.Fatal("rejected mutation persisted")
	}
	if err := sys.PolicyDBSet("guard-user", "guard-policy", false); err != nil {
		t.Fatal(err)
	}
	cred := sys.iamUsersMap["guard-user"]
	cred.SecretKey = "changed-user-secret"
	sys.iamUsersMap["guard-user"] = cred
	if err := sys.rotateNativeIAMSecret("guard-created", "new-created-secret", guard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatalf("stale signed credential accepted: %v", err)
	}
	cred.SecretKey = guard.credential.SecretKey
	cred.Status = auth.AccountOff
	sys.iamUsersMap["guard-user"] = cred
	if err := sys.createNativeIAM("user", "disabled-denied", iamCreateRequest{SecretKey: "created-user-secret"}, guard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatalf("disabled caller accepted: %v", err)
	}
	guard.credential.SessionToken = "temporary-token"
	if err := sys.createNativeIAM("user", "temporary-denied", iamCreateRequest{SecretKey: "created-user-secret"}, guard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatalf("temporary caller accepted: %v", err)
	}
}

func TestConsoleIAMMutationGuardGroupDenyAndRoot(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	if err := sys.CreateUser("group-guard-user", madmin.UserInfo{SecretKey: "group-guard-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	for name, effect := range map[string]string{"group-allow": "Allow", "group-deny": "Deny"} {
		var policy iampolicy.Policy
		data := []byte(`{"Version":"2012-10-17","Statement":[{"Effect":"` + effect + `","Action":["admin:CreateUser"],"Resource":["arn:aws:s3:::*"]}]}`)
		if err := json.Unmarshal(data, &policy); err != nil {
			t.Fatal(err)
		}
		if err := sys.SetPolicy(name, policy); err != nil {
			t.Fatal(err)
		}
	}
	if err := sys.AddUsersToGroup("guard-group", []string{"group-guard-user"}); err != nil {
		t.Fatal(err)
	}
	if err := sys.PolicyDBSet("guard-group", "group-allow", true); err != nil {
		t.Fatal(err)
	}
	guard := iamMutationGuard{credential: sys.iamUsersMap["group-guard-user"], args: iampolicy.Args{AccountName: "group-guard-user", Action: iampolicy.CreateUserAdminAction}}
	if err := sys.validateIAMMutationGuard(guard); err != nil {
		t.Fatalf("group allow rejected: %v", err)
	}
	if err := sys.PolicyDBSet("group-guard-user", "group-deny", false); err != nil {
		t.Fatal(err)
	}
	if err := sys.validateIAMMutationGuard(guard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatalf("direct Deny lost to group Allow: %v", err)
	}
	oldRoot := globalActiveCred
	defer func() { globalActiveCred = oldRoot }()
	globalActiveCred = auth.Credentials{AccessKey: "guard-root", SecretKey: "guard-root-secret", Status: auth.AccountOn}
	root := iamMutationGuard{credential: globalActiveCred, args: iampolicy.Args{AccountName: "guard-root", Action: iampolicy.CreateUserAdminAction}}
	if err := sys.createNativeIAM("user", "root-created", iamCreateRequest{SecretKey: "root-created-secret"}, root); err != nil {
		t.Fatalf("root mutation rejected: %v", err)
	}
	oldOPA := globalPolicyOPA
	defer func() { globalPolicyOPA = oldOPA }()
	globalPolicyOPA = &opa.Opa{}
	if iamBindingSupported() {
		t.Fatal("OPA advertised process-local atomic capability")
	}
	if err := sys.validateIAMMutationGuard(root); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatalf("OPA guard accepted: %v", err)
	}
}
