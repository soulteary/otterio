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

	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

func TestConsoleIAMRelationshipsNativeGraphAndKnownEmpty(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	if err := sys.CreateUser("relation-parent", madmin.UserInfo{SecretKey: "relation-parent-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err := sys.AddUsersToGroup("empty-relations", nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	initial, err := sys.nativeRelationshipsLocked("group", "empty-relations", now)
	if err != nil || initial.AffectedAccessKeys == nil || len(initial.AffectedAccessKeys) != 0 || len(initial.Revision) != 64 {
		t.Fatal("known empty lost")
	}
	sys.iamUsersMap["relation-service"] = auth.Credentials{AccessKey: "relation-service", SecretKey: "relation-service-secret", ParentUser: "relation-parent", Status: auth.AccountOn}
	sys.iamUsersMap["relation-sts"] = auth.Credentials{AccessKey: "relation-sts", SecretKey: "relation-sts-secret", SessionToken: "token", ParentUser: "relation-parent", Expiration: now.Add(time.Hour), Status: auth.AccountOn}
	parent, err := sys.nativeRelationshipsLocked("user", "relation-parent", now)
	if err != nil || len(parent.AffectedAccessKeys) != 3 || parent.Revision == initial.Revision {
		t.Fatalf("child impact omitted: %+v %v", parent, err)
	}
	service, err := sys.nativeRelationshipsLocked("service-account", "relation-service", now)
	if err != nil || service.NativeParent != "relation-parent" || len(service.AffectedAccessKeys) != 1 {
		t.Fatal("native parent missing")
	}
	if err := sys.AddUsersToGroup("empty-relations", []string{"relation-parent"}); err != nil {
		t.Fatal(err)
	}
	group, err := sys.nativeRelationshipsLocked("group", "empty-relations", now)
	if err != nil || len(group.AffectedAccessKeys) != 3 || group.Revision == parent.Revision {
		t.Fatal("group/child relation omitted")
	}
	for _, change := range []func(){func() {
		cred := sys.iamUsersMap["relation-service"]
		cred.SecretKey = "new-child-secret"
		sys.iamUsersMap["relation-service"] = cred
	}, func() { sys.iamUserPolicyMap["relation-parent"] = newMappedPolicy("policy") }, func() { sys.iamPolicyDocsMap["policy"] = iampolicy.ReadOnly }, func() { sys.iamGroupPolicyMap["empty-relations"] = newMappedPolicy("policy") }} {
		before, _ := sys.nativeRelationshipsRevisionLocked()
		change()
		after, _ := sys.nativeRelationshipsRevisionLocked()
		if before == after {
			t.Fatal("IAM graph change did not invalidate revision")
		}
	}
	sys.iamUsersMap["unknown-service"] = auth.Credentials{AccessKey: "unknown-service", ParentUser: "external-subject", SecretKey: "unknown-service-secret"}
	if _, err := sys.nativeRelationshipsLocked("service-account", "unknown-service", now); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("unknown external parent inferred native")
	}
	for i := 0; i < 4097; i++ {
		sys.iamGroupsMap[time.Unix(int64(i), 0).String()] = GroupInfo{}
	}
	if _, err := sys.nativeRelationshipsLocked("user", "relation-parent", now); !errors.Is(err, errIAMActionNotAllowed) {
		t.Fatal("oversized graph accepted")
	}
}
func TestConsoleIAMRelationshipsConcurrentLegacyMembership(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	sys.CreateUser("concurrent-parent", madmin.UserInfo{SecretKey: "concurrent-secret", Status: madmin.AccountEnabled})
	sys.AddUsersToGroup("concurrent-group", nil)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			sys.AddUsersToGroup("concurrent-group", []string{"concurrent-parent"})
			sys.RemoveUsersFromGroup("concurrent-group", []string{"concurrent-parent"})
		}
	}()
	for i := 0; i < 100; i++ {
		sys.store.rlock()
		snapshot, err := sys.nativeRelationshipsLocked("group", "concurrent-group", time.Now())
		revision, revisionErr := sys.nativeRelationshipTokenLocked("group", "concurrent-group", snapshot.ExpiresAt)
		sys.store.runlock()
		if err != nil || revisionErr != nil || snapshot.Revision != revision || len(snapshot.AffectedAccessKeys) > 1 {
			t.Fatal("mixed relation/revision snapshot")
		}
	}
	wg.Wait()
}
func TestConsoleIAMRelationshipsSignedReadAuthorization(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bed.TearDown()
	globalIAMSys.InitStore(bed.objLayer)
	if err := globalIAMSys.CreateUser("relations-reader", madmin.UserInfo{SecretKey: "relations-reader-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err := globalIAMSys.AddUsersToGroup("relations-empty", nil); err != nil {
		t.Fatal(err)
	}
	request := func(credential auth.Credentials) *httptest.ResponseRecorder {
		path := adminPathPrefix + adminAPIVersionPrefix + "/iam-relationships?" + url.Values{"kind": {"group"}, "target": {"relations-empty"}}.Encode()
		r, err := newTestSignedRequestV4(http.MethodGet, path, 0, bytes.NewReader(nil), credential.AccessKey, credential.SecretKey, nil)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		bed.router.ServeHTTP(response, r)
		return response
	}
	reader := globalIAMSys.iamUsersMap["relations-reader"]
	denied := request(reader)
	if denied.Code != 403 || denied.Header().Get(iamRelationshipsCapability) != "" {
		t.Fatalf("unauthorized read advertised relations: %d", denied.Code)
	}
	var policy iampolicy.Policy
	json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:GetGroup"],"Resource":["arn:aws:s3:::*"]}]}`), &policy)
	globalIAMSys.SetPolicy("relations-read", policy)
	globalIAMSys.PolicyDBSet(reader.AccessKey, "relations-read", false)
	denied = request(reader)
	if denied.Code != 403 {
		t.Fatal("detail reader received child access keys without list permission")
	}
	json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:GetGroup","admin:ListUsers"],"Resource":["arn:aws:s3:::*"]}]}`), &policy)
	globalIAMSys.SetPolicy("relations-read", policy)
	allowed := request(reader)
	if allowed.Code != 200 || allowed.Header().Get(iamRelationshipsCapability) != "v1" {
		t.Fatalf("authorized signed relations rejected: %d %s", allowed.Code, allowed.Body.String())
	}
	var snapshot iamRelationshipsSnapshot
	if json.Unmarshal(allowed.Body.Bytes(), &snapshot) != nil || snapshot.AffectedAccessKeys == nil || len(snapshot.AffectedAccessKeys) != 0 {
		t.Fatal("server did not preserve known-empty")
	}
	globalIAMSys.PolicyDBSet(reader.AccessKey, "", false)
	denied = request(reader)
	if denied.Code != 403 || denied.Header().Get(iamRelationshipsCapability) != "" {
		t.Fatal("revoked reader retained relation capability")
	}
}
