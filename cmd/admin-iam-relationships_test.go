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

	"github.com/soulteary/otterio-sdk/v7/pkg/set"
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

func TestConsoleIAMRelationshipsRevisionCanonicalPolicySets(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	var policy iampolicy.Policy
	data := []byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:PutObject","s3:DeleteObject"],"Resource":["arn:aws:s3:::a/*","arn:aws:s3:::b/*"]}]}`)
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	sys.SetPolicy("unordered-policy", policy)
	first, err := sys.nativeRelationshipsRevisionLocked()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 100; index++ {
		revision, err := sys.nativeRelationshipsRevisionLocked()
		if err != nil || revision != first {
			t.Fatal("unchanged set order invalidated graph revision")
		}
	}
	// Different array ordering encodes the same IAM graph meaning; numbers must
	// retain precision rather than becoming float64 during canonicalization.
	a, err := canonicalIAMGraphJSON([]byte(`{"Users":{"u":{"groups":["z","a"]}},"n":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalIAMGraphJSON([]byte(`{"n":9007199254740993,"Users":{"u":{"groups":["a","z"]}}}`))
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("canonical graph order/number changed")
	}
	sys.SetPolicy("unordered-policy", iampolicy.ReadOnly)
	changed, err := sys.nativeRelationshipsRevisionLocked()
	if err != nil || changed == first {
		t.Fatal("actual policy modification failed to invalidate graph")
	}
}

func TestConsoleIAMRelationshipsCanonicalPreservesUnknownArrayOrder(t *testing.T) {
	first, _ := canonicalIAMGraphJSON([]byte(`{"unknown":["z","a"],"Policies":{"p":{"Statement":[{"Sid":"second"},{"Sid":"first"}]}}}`))
	var parsed struct {
		Unknown  []string `json:"unknown"`
		Policies map[string]struct {
			Statements []struct {
				SID string `json:"Sid"`
			} `json:"Statement"`
		} `json:"Policies"`
	}
	if json.Unmarshal(first, &parsed) != nil || parsed.Unknown[0] != "z" || parsed.Policies["p"].Statements[0].SID != "second" {
		t.Fatal("ordered/unknown arrays were reordered")
	}
}

func TestConsoleIAMRelationshipsCoversEffectiveMembershipIndex(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	sys.CreateUser("index-user", madmin.UserInfo{SecretKey: "index-user-secret", Status: madmin.AccountEnabled})
	sys.AddUsersToGroup("index-group", nil)
	before, _ := sys.nativeRelationshipsRevisionLocked()
	sys.iamUserGroupMemberships["index-user"] = set.CreateStringSet("index-group")
	after, _ := sys.nativeRelationshipsRevisionLocked()
	if before == after {
		t.Fatal("effective policy index missing from graph CAS")
	}
	relation, err := sys.nativeRelationshipsLocked("group", "index-group", time.Now())
	if err != nil || len(relation.AffectedAccessKeys) != 1 || relation.AffectedAccessKeys[0] != "index-user" {
		t.Fatal("effective inherited user omitted from group impact")
	}
}

func TestConsoleIAMServiceRelationshipsRequireServicePermission(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bed.TearDown()
	globalIAMSys.InitStore(bed.objLayer)
	for _, key := range []string{"service-reader", "service-parent"} {
		if err := globalIAMSys.CreateUser(key, madmin.UserInfo{SecretKey: key + "-secret", Status: madmin.AccountEnabled}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = globalIAMSys.NewServiceAccount(ctx, "service-parent", nil, newServiceAccountOpts{accessKey: "service-child", secretKey: "service-child-secret"})
	if err != nil {
		t.Fatal(err)
	}
	reader := globalIAMSys.iamUsersMap["service-reader"]
	for _, servicePermission := range []bool{false, true} {
		actions := `"admin:GetUser","admin:ListUsers"`
		if servicePermission {
			actions += `,"admin:ListServiceAccounts"`
		}
		policy, err := iampolicy.ParseConfig(strings.NewReader(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":[` + actions + `],"Resource":["arn:aws:s3:::*"]}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := globalIAMSys.SetPolicy("service-relations-read", *policy); err != nil {
			t.Fatal(err)
		}
		if err := globalIAMSys.PolicyDBSet(reader.AccessKey, "service-relations-read", false); err != nil {
			t.Fatal(err)
		}
		path := adminPathPrefix + adminAPIVersionPrefix + "/iam-relationships?" + url.Values{"kind": {"service-account"}, "target": {"service-child"}}.Encode()
		r, err := newTestSignedRequestV4(http.MethodGet, path, 0, bytes.NewReader(nil), reader.AccessKey, reader.SecretKey, nil)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		bed.router.ServeHTTP(response, r)
		if !servicePermission && (response.Code != 403 || response.Header().Get(iamRelationshipsCapability) != "") {
			t.Fatal("regular-user reader received service identity")
		}
		if servicePermission && response.Code != 200 {
			t.Fatalf("service reader rejected: %d %s", response.Code, response.Body.String())
		}
	}
}
