// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestConsoleIAMRelationshipMutationCouplesGraphAndBindingRevision(t *testing.T) {
	sys := NewIAMSys()
	sys.store = &consoleIAMBindingMemoryStore{}
	sys.CreateUser("impact-parent", madmin.UserInfo{SecretKey: "impact-parent-secret", Status: madmin.AccountEnabled})
	sys.AddUsersToGroup("impact-group", []string{"impact-parent"})
	sys.iamUsersMap["impact-service"] = auth.Credentials{AccessKey: "impact-service", SecretKey: "impact-service-secret", ParentUser: "impact-parent", Status: auth.AccountOn}
	sys.SetPolicy("impact-policy", iampolicy.ReadOnly)
	oldRoot := globalActiveCred
	defer func() { globalActiveCred = oldRoot }()
	globalActiveCred = auth.Credentials{AccessKey: "impact-root", SecretKey: "impact-root-secret", Status: auth.AccountOn}
	guard := iamMutationGuard{credential: globalActiveCred, args: iampolicy.Args{AccountName: globalActiveCred.AccessKey, Action: iampolicy.AttachPolicyAdminAction}}
	bindings, _ := sys.nativeBindings("group", "impact-group")
	sys.store.rlock()
	relation, _ := sys.nativeRelationshipsLocked("group", "impact-group", time.Now())
	sys.store.runlock()
	// Child credential change is invisible to the old group bindings fingerprint,
	// but must conflict with the coupled graph snapshot and leave bindings intact.
	sys.store.lock()
	cred := sys.iamUsersMap["impact-service"]
	cred.SecretKey = "changed-impact-secret"
	sys.iamUsersMap[cred.AccessKey] = cred
	sys.store.unlock()
	if _, err := sys.replaceNativeRelationshipBindings("group", "impact-group", bindings.Revision, relation.Revision, relation.ExpiresAt, []string{"impact-policy"}, guard); !errors.Is(err, errIAMBindingConflict) {
		t.Fatalf("child race accepted: %v", err)
	}
	current, _ := sys.nativeBindings("group", "impact-group")
	if len(current.Policies) != 0 {
		t.Fatal("conflict persisted mutation")
	}
	sys.store.rlock()
	relation, _ = sys.nativeRelationshipsLocked("group", "impact-group", time.Now())
	sys.store.runlock()
	if _, err := sys.replaceNativeRelationshipBindings("group", "impact-group", bindings.Revision, relation.Revision, relation.ExpiresAt.Add(time.Second), []string{"impact-policy"}, guard); !errors.Is(err, errIAMBindingConflict) {
		t.Fatal("expiry extension accepted")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := sys.replaceNativeRelationshipBindings("group", "impact-group", bindings.Revision, relation.Revision, relation.ExpiresAt, []string{"impact-policy"}, guard)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
				if result.Outcome != "confirmed" || len(result.Impact.AffectedAccessKeys) != 2 {
					t.Error("actual parent/child impact missing")
				}
				data, _ := json.Marshal(result)
				if strings.Contains(string(data), "secret") {
					t.Error("credential data leaked")
				}
			} else if errors.Is(err, errIAMBindingConflict) {
				conflict++
			} else {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if success != 1 || conflict != 1 {
		t.Fatalf("atomic concurrency: %d successes, %d conflicts", success, conflict)
	}
	expired := relation
	expired.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := sys.replaceNativeRelationshipBindings("group", "impact-group", bindings.Revision, expired.Revision, expired.ExpiresAt, nil, guard); !errors.Is(err, errIAMBindingConflict) {
		t.Fatal("expired condition accepted")
	}
}
func TestConsoleIAMRelationshipConditionRequiresAllSignedHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "/", nil)
	for _, name := range []string{iamBindingCondition, iamImpactCondition} {
		r.Header.Set(name, strings.Repeat("a", 64))
	}
	r.Header.Set(iamImpactExpires, time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano))
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 SignedHeaders=host;"+strings.ToLower(iamBindingCondition)+", Signature=fake")
	if _, _, _, err := signedRelationshipCondition(r); err == nil {
		t.Fatal("unsigned impact accepted")
	}
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 SignedHeaders=host;"+strings.ToLower(iamBindingCondition)+";"+strings.ToLower(iamImpactCondition)+";"+strings.ToLower(iamImpactExpires)+", Signature=fake")
	if _, _, _, err := signedRelationshipCondition(r); err != nil {
		t.Fatal(err)
	}
	r.Header.Add(iamImpactCondition, strings.Repeat("b", 64))
	if _, _, _, err := signedRelationshipCondition(r); err == nil {
		t.Fatal("ambiguous impact condition accepted")
	}
}

func TestConsoleIAMRelationshipMutationSignedRouter(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bed.TearDown()
	globalIAMSys.InitStore(bed.objLayer)
	if err := globalIAMSys.CreateUser("router-parent", madmin.UserInfo{SecretKey: "router-parent-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err := globalIAMSys.SetPolicy("router-policy", iampolicy.ReadOnly); err != nil {
		t.Fatal(err)
	}
	binding, _ := globalIAMSys.nativeBindings("user", "router-parent")
	globalIAMSys.store.rlock()
	relation, _ := globalIAMSys.nativeRelationshipsLocked("user", "router-parent", time.Now())
	globalIAMSys.store.runlock()
	path := adminPathPrefix + adminAPIVersionPrefix + "/iam-relationship-bindings?" + url.Values{"kind": {"user"}, "target": {"router-parent"}}.Encode()
	body := []byte(`{"policies":["router-policy"]}`)
	headers := map[string]string{iamBindingCondition: binding.Revision, iamImpactCondition: relation.Revision, iamImpactExpires: relation.ExpiresAt.UTC().Format(time.RFC3339Nano)}
	request, err := newTestSignedRequestV4(http.MethodPut, path, int64(len(body)), bytes.NewReader(body), globalActiveCred.AccessKey, globalActiveCred.SecretKey, headers)
	if err != nil {
		t.Fatal(err)
	}
	// Verify rejection before persistence for malformed transfer declarations
	// and a signed payload whose bytes were altered in transit.
	for _, invalid := range []string{"unknown-length", "oversize", "truncated", "tampered-body", "unsigned-payload"} {
		bad, signErr := newTestSignedRequestV4(http.MethodPut, path, int64(len(body)), bytes.NewReader(body), globalActiveCred.AccessKey, globalActiveCred.SecretKey, headers)
		if signErr != nil {
			t.Fatal(signErr)
		}
		switch invalid {
		case "unknown-length":
			bad.ContentLength = -1
		case "oversize":
			bad.ContentLength = 32*1024 + 1
		case "truncated":
			bad.ContentLength++
		case "tampered-body":
			bad.Body = io.NopCloser(bytes.NewReader([]byte(`{"policies":[]}`)))
			bad.ContentLength = int64(len(`{"policies":[]}`))
		case "unsigned-payload":
			bad.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
		}
		rejected := httptest.NewRecorder()
		adminAPIHandlers{}.IAMRelationshipBindings(rejected, bad)
		if rejected.Code < 400 {
			t.Fatalf("%s accepted: %d", invalid, rejected.Code)
		}
		current, _ := globalIAMSys.nativeBindings("user", "router-parent")
		if current.Revision != binding.Revision {
			t.Fatal("invalid body mutated binding")
		}
	}
	response := httptest.NewRecorder()
	adminAPIHandlers{}.IAMRelationshipBindings(response, request)
	if response.Code != 200 {
		t.Fatalf("coupled signed route failed: %d %s", response.Code, response.Body.String())
	}
	var result iamRelationshipMutationResult
	if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Outcome != "confirmed" || len(result.Impact.AffectedAccessKeys) != 1 || result.Impact.AffectedAccessKeys[0] != "router-parent" || len(result.Bindings.Policies) != 1 {
		t.Fatal("confirmed actual impact missing")
	}
	// Replay the stale conditions through the actual signature/router boundary.
	request, err = newTestSignedRequestV4(http.MethodPut, path, int64(len(body)), bytes.NewReader(body), globalActiveCred.AccessKey, globalActiveCred.SecretKey, headers)
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	adminAPIHandlers{}.IAMRelationshipBindings(response, request)
	if response.Code != 412 {
		t.Fatalf("stale coupled route condition accepted: %d", response.Code)
	}
}
