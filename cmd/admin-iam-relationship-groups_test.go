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

	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

type groupPartialDeleteStore struct {
	consoleIAMBindingMemoryStore
	failDeleteGroup bool
	mappedDeleted   bool
}

func (s *groupPartialDeleteStore) deleteMappedPolicy(context.Context, string, IAMUserType, bool) error {
	s.mappedDeleted = true
	return nil
}
func (s *groupPartialDeleteStore) deleteGroupInfo(context.Context, string) error {
	if s.failDeleteGroup {
		return errors.New("injected group persistence failure")
	}
	return nil
}
func relationshipGroupFixture(t *testing.T) (*IAMSys, iamMutationGuard) {
	t.Helper()
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	for _, user := range []string{"group-parent", "group-added"} {
		if err := sys.CreateUser(user, madmin.UserInfo{SecretKey: user + "-secret", Status: madmin.AccountEnabled}); err != nil {
			t.Fatal(err)
		}
	}
	sys.iamUsersMap["parent-service"] = auth.Credentials{AccessKey: "parent-service", SecretKey: "parent-service-secret", ParentUser: "group-parent", Status: auth.AccountOn}
	sys.iamUsersMap["added-sts"] = auth.Credentials{AccessKey: "added-sts", SecretKey: "added-sts-secret", ParentUser: "group-added", SessionToken: "token", Expiration: time.Now().Add(time.Hour), Status: auth.AccountOn}
	sys.AddUsersToGroup("mutable-group", []string{"group-parent"})
	old := globalActiveCred
	t.Cleanup(func() { globalActiveCred = old })
	globalActiveCred = auth.Credentials{AccessKey: "group-root", SecretKey: "group-root-secret", Status: auth.AccountOn}
	return sys, iamMutationGuard{credential: globalActiveCred, args: iampolicy.Args{AccountName: globalActiveCred.AccessKey}}
}
func groupRelation(t *testing.T, sys *IAMSys, name string) iamRelationshipsSnapshot {
	t.Helper()
	sys.store.rlock()
	defer sys.store.runlock()
	snapshot, err := sys.nativeRelationshipsLocked("group", name, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func TestConsoleIAMRelationshipGroupsUnionImpactAndStatus(t *testing.T) {
	sys, guard := relationshipGroupFixture(t)
	relation := groupRelation(t, sys, "mutable-group")
	result, err := sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.add-members", Target: "mutable-group", Members: []string{"group-added"}}, relation.Revision, relation.ExpiresAt, guard)
	if err != nil || len(result.Impact.AffectedAccessKeys) != 4 || len(result.Group.Members) != 2 || result.Group.Status != statusEnabled {
		t.Fatalf("add parent/child impact: %v %+v", err, result)
	}
	relation = groupRelation(t, sys, "mutable-group")
	result, err = sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.remove-members", Target: "mutable-group", Members: []string{"group-parent"}}, relation.Revision, relation.ExpiresAt, guard)
	if err != nil || len(result.Impact.AffectedAccessKeys) != 4 || len(result.Group.Members) != 1 || sys.iamUserGroupMemberships["group-parent"].Contains("mutable-group") {
		t.Fatal("removal dropped pre-change parent/child impact")
	}
	relation = groupRelation(t, sys, "mutable-group")
	result, err = sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.disable", Target: "mutable-group"}, relation.Revision, relation.ExpiresAt, guard)
	if err != nil || result.Group.Status != statusDisabled || len(result.Impact.AffectedAccessKeys) != 2 {
		t.Fatal("disable inheritance impact missing")
	}
	relation = groupRelation(t, sys, "mutable-group")
	if _, err := sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.delete", Target: "mutable-group"}, relation.Revision, relation.ExpiresAt, guard); !errors.Is(err, errGroupNotEmpty) {
		t.Fatal("nonempty delete accepted")
	}
	relation = groupRelation(t, sys, "mutable-group")
	result, err = sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.enable", Target: "mutable-group"}, relation.Revision, relation.ExpiresAt, guard)
	if err != nil || result.Group.Status != statusEnabled {
		t.Fatal("enable rejected")
	}
	sys.AddUsersToGroup("empty-group", nil)
	relation = groupRelation(t, sys, "empty-group")
	result, err = sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.delete", Target: "empty-group"}, relation.Revision, relation.ExpiresAt, guard)
	if err != nil || result.Group.Status != "deleted" || result.Impact.AffectedAccessKeys == nil || len(result.Impact.AffectedAccessKeys) != 0 || result.Group.Members == nil {
		t.Fatal("known-empty deletion acknowledgement lost")
	}
	if _, ok := sys.iamGroupsMap["empty-group"]; ok {
		t.Fatal("deleted group retained")
	}
}
func TestConsoleIAMRelationshipGroupsGraphRaceAndPartialDelete(t *testing.T) {
	sys, guard := relationshipGroupFixture(t)
	relation := groupRelation(t, sys, "mutable-group")
	sys.AddUsersToGroup("mutable-group", []string{"group-added"})
	if _, err := sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.disable", Target: "mutable-group"}, relation.Revision, relation.ExpiresAt, guard); !errors.Is(err, errIAMBindingConflict) {
		t.Fatal("legacy member change bypassed graph CAS")
	}
	relation = groupRelation(t, sys, "mutable-group")
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.disable", Target: "mutable-group"}, relation.Revision, relation.ExpiresAt, guard)
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
		t.Fatal("concurrent group writes both committed")
	}
	store := &groupPartialDeleteStore{failDeleteGroup: true}
	sys.store = store
	sys.AddUsersToGroup("partial-empty", nil)
	sys.SetPolicy("group-policy", iampolicy.ReadOnly)
	sys.PolicyDBSet("partial-empty", "group-policy", true)
	relation = groupRelation(t, sys, "partial-empty")
	_, err := sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.delete", Target: "partial-empty"}, relation.Revision, relation.ExpiresAt, guard)
	if !errors.Is(err, errIAMRelationshipCommitted) || !store.mappedDeleted {
		t.Fatal("partial commit did not become unknown")
	}
	if _, ok := sys.iamGroupPolicyMap["partial-empty"]; ok {
		t.Fatal("memory retained already-deleted policy mapping")
	}
	if _, ok := sys.iamGroupsMap["partial-empty"]; !ok {
		t.Fatal("failed group deletion disappeared from memory")
	}
}
func TestConsoleIAMRelationshipGroupsDenyAndUnknownNativeMembers(t *testing.T) {
	sys, guard := relationshipGroupFixture(t)
	sys.CreateUser("group-manager", madmin.UserInfo{SecretKey: "group-manager-secret", Status: madmin.AccountEnabled})
	var policy iampolicy.Policy
	json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:DisableGroup","admin:GetGroup","admin:ListUsers"],"Resource":["arn:aws:s3:::*"]}]}`), &policy)
	sys.SetPolicy("group-manager-allow", policy)
	sys.PolicyDBSet("group-manager", "group-manager-allow", false)
	nativeGuard := iamMutationGuard{credential: sys.iamUsersMap["group-manager"], args: iampolicy.Args{AccountName: "group-manager"}}
	relation := groupRelation(t, sys, "mutable-group")
	if _, err := sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.disable", Target: "mutable-group"}, relation.Revision, relation.ExpiresAt, nativeGuard); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["admin:DisableGroup"],"Resource":["arn:aws:s3:::*"]}]}`), &policy)
	sys.SetPolicy("group-manager-deny", policy)
	sys.AddUsersToGroup("manager-deny-group", []string{"group-manager"})
	sys.PolicyDBSet("manager-deny-group", "group-manager-deny", true)
	relation = groupRelation(t, sys, "mutable-group")
	if _, err := sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.disable", Target: "mutable-group"}, relation.Revision, relation.ExpiresAt, nativeGuard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("group deny ignored")
	}
	relation = groupRelation(t, sys, "mutable-group")
	if _, err := sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.add-members", Target: "mutable-group", Members: []string{"parent-service"}}, relation.Revision, relation.ExpiresAt, guard); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("service child became native group member")
	}
	bad := guard
	bad.credential = sys.iamUsersMap["parent-service"]
	if _, err := sys.mutateNativeRelationshipGroup(context.Background(), iamRelationshipGroupRequest{Action: "group.disable", Target: "mutable-group"}, relation.Revision, relation.ExpiresAt, bad); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("service child inherited manager role")
	}
}
func TestConsoleIAMRelationshipGroupsSignedHandlerAndBodyValidation(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bed.TearDown()
	globalIAMSys.InitStore(bed.objLayer)
	globalIAMSys.AddUsersToGroup("signed-group", nil)
	relation := groupRelation(t, globalIAMSys, "signed-group")
	path := adminPathPrefix + adminAPIVersionPrefix + "/iam-relationship-groups?" + url.Values{"kind": {"group"}, "target": {"signed-group"}}.Encode()
	body := []byte(`{"action":"group.disable","target":"signed-group","members":[]}`)
	request, err := newTestSignedRequestV4(http.MethodPut, path, int64(len(body)), bytes.NewReader(body), globalActiveCred.AccessKey, globalActiveCred.SecretKey, map[string]string{iamImpactCondition: relation.Revision, iamImpactExpires: relation.ExpiresAt.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	adminAPIHandlers{}.IAMRelationshipGroups(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"disabled"`) {
		t.Fatalf("signed group route contract failed: %d %s", response.Code, response.Body.String())
	}
	for _, body := range []string{`{"action":"group.disable","action":"group.enable","target":"g"}`, `{"action":"group.delete","target":"g","members":["native-user"]}`, `{"action":"group.create","target":"g"}`, `{"action":"group.add-members","target":"g","members":null}`} {
		if _, err := decodeIAMRelationshipGroupBody([]byte(body)); err == nil {
			t.Fatal("ambiguous or unsupported group body accepted")
		}
	}
}
