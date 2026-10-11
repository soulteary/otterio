// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/soulteary/otterio-sdk/v7/pkg/set"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
)

const iamRelationshipsCapability = "X-Otterio-IAM-Relationships"
const iamRelationshipsRevision = "X-Otterio-IAM-Impact-Revision"

// Bound parallel graph serialization even for clients with list permissions.
var iamRelationshipSlots = make(chan struct{}, 4)

type iamRelationshipsSnapshot struct {
	CoupledServiceAccounts bool      `json:"coupledServiceAccounts"`
	CoupledGroups          bool      `json:"coupledGroups"`
	CoupledUserSecret      bool      `json:"coupledUserSecret"`
	Kind                   string    `json:"kind"`
	Target                 string    `json:"target"`
	State                  string    `json:"state"`
	CoupledBindings        bool      `json:"coupledBindings"`
	Revision               string    `json:"revision"`
	ExpiresAt              time.Time `json:"expiresAt"`
	NativeParent           string    `json:"nativeParent,omitempty"`
	AffectedAccessKeys     []string  `json:"affectedAccessKeys"`
}

// A process-keyed fingerprint binds the entire native IAM graph, including
// policy documents, inherited group permissions and child credential bytes.
// Any legacy mutation invalidates a draft. This deliberately prefers conflicts
// over missing an indirect permission/identity change. Caller holds store lock.
func (sys *IAMSys) nativeRelationshipsRevisionLocked() (string, error) {
	if len(sys.iamUsersMap)+len(sys.iamGroupsMap)+len(sys.iamUserPolicyMap)+len(sys.iamGroupPolicyMap)+len(sys.iamPolicyDocsMap)+len(sys.iamUserGroupMemberships) > 4096 {
		return "", errIAMActionNotAllowed
	}
	data, err := json.Marshal(struct {
		Users         map[string]auth.Credentials
		Groups        map[string]GroupInfo
		UserPolicies  map[string]MappedPolicy
		GroupPolicies map[string]MappedPolicy
		Policies      map[string]iampolicy.Policy
		Memberships   map[string]set.StringSet
	}{sys.iamUsersMap, sys.iamGroupsMap, sys.iamUserPolicyMap, sys.iamGroupPolicyMap, sys.iamPolicyDocsMap, sys.iamUserGroupMemberships})
	if err != nil || len(data) > 4<<20 {
		return "", errIAMActionNotAllowed
	}
	data, err = canonicalIAMGraphJSON(data)
	if err != nil {
		return "", errIAMActionNotAllowed
	}
	digest := hmac.New(sha256.New, iamBindingRevisionKey[:])
	digest.Write([]byte("native-relationships:v1\x00"))
	digest.Write(data)
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// Caller holds store lock. Returned arrays are copied; no credential material
// leaves this function. Empty group impact is authoritative, absent is an error.
func (sys *IAMSys) nativeRelationshipsLocked(kind, target string, now time.Time) (iamRelationshipsSnapshot, error) {
	result := iamRelationshipsSnapshot{Kind: kind, Target: target, State: "known", CoupledBindings: true, CoupledGroups: true, CoupledServiceAccounts: true, CoupledUserSecret: true, ExpiresAt: now.Add(30 * time.Second), AffectedAccessKeys: []string{}}
	if sys.usersSysType != OtterIOUsersSysType || !validIAMBindingName(target) {
		return result, errIAMActionNotAllowed
	}
	if len(sys.iamUsersMap)+len(sys.iamGroupsMap)+len(sys.iamPolicyDocsMap)+len(sys.iamUserGroupMemberships) > 4096 {
		return result, errIAMActionNotAllowed
	}
	parents := map[string]bool{}
	switch kind {
	case "user":
		credential, ok := sys.iamUsersMap[target]
		if !ok {
			return result, errNoSuchUser
		}
		if credential.IsTemp() || credential.IsServiceAccount() || target == globalActiveCred.AccessKey {
			return result, errIAMActionNotAllowed
		}
		parents[target] = true
	case "group":
		group, ok := sys.iamGroupsMap[target]
		if !ok {
			return result, errNoSuchGroup
		}
		for _, member := range group.Members {
			credential, ok := sys.iamUsersMap[member]
			if !ok || credential.IsTemp() || credential.IsServiceAccount() {
				return result, errIAMActionNotAllowed
			}
			parents[member] = true
		}
		// Effective policy evaluation also consults this index. Include its
		// members conservatively even if a stale group description omitted one.
		for user, memberships := range sys.iamUserGroupMemberships {
			if memberships.Contains(target) {
				credential, ok := sys.iamUsersMap[user]
				if !ok || credential.IsTemp() || credential.IsServiceAccount() {
					return result, errIAMActionNotAllowed
				}
				parents[user] = true
			}
		}
	case "service-account":
		credential, ok := sys.iamUsersMap[target]
		if !ok {
			return result, errNoSuchUser
		}
		if !credential.IsServiceAccount() || credential.ParentUser == "" {
			return result, errIAMActionNotAllowed
		}
		parent, ok := sys.iamUsersMap[credential.ParentUser]
		if !ok || parent.IsTemp() || parent.IsServiceAccount() {
			return result, errIAMActionNotAllowed
		}
		result.NativeParent = credential.ParentUser
		result.AffectedAccessKeys = append(result.AffectedAccessKeys, target)
	default:
		return result, errInvalidArgument
	}
	for key, credential := range sys.iamUsersMap {
		if parents[key] || (credential.ParentUser != "" && parents[credential.ParentUser]) {
			result.AffectedAccessKeys = append(result.AffectedAccessKeys, key)
		}
	}
	sort.Strings(result.AffectedAccessKeys)
	revision, err := sys.nativeRelationshipTokenLocked(kind, target, result.ExpiresAt)
	if err != nil {
		return result, err
	}
	result.Revision = revision
	return result, nil
}

func (a adminAPIHandlers) IAMRelationships(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "IAMRelationships")
	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))
	w.Header().Set("Cache-Control", "no-store")
	query, err := url.ParseQuery(r.URL.RawQuery)
	kind, target := query.Get("kind"), query.Get("target")
	if err != nil || len(query) != 2 || len(query["kind"]) != 1 || len(query["target"]) != 1 || !validIAMBindingName(target) || (kind != "user" && kind != "group" && kind != "service-account") {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	action := iampolicy.AdminAction(iampolicy.GetUserAdminAction)
	if kind == "group" {
		action = iampolicy.GetGroupAdminAction
	}
	// Relations expose child keys; require list authorization in addition to
	// target detail authorization, rather than silently elevating a detail reader.
	objectAPI, credential := validateAdminUsersReq(ctx, w, r, action)
	if objectAPI == nil {
		return
	}
	if !iamBindingSupported() {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrNotImplemented), r.URL)
		return
	}
	select {
	case iamRelationshipSlots <- struct{}{}:
		defer func() { <-iamRelationshipSlots }()
	default:
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrSlowDown), r.URL)
		return
	}
	globalIAMSys.store.rlock()
	guards := []iamMutationGuard{newIAMMutationGuard(r, credential, action)}
	err = globalIAMSys.checkIAMMutationGuardsLocked(guards)
	if err == nil {
		listGuard := newIAMMutationGuard(r, credential, iampolicy.ListUsersAdminAction)
		err = globalIAMSys.checkIAMMutationGuardsLocked([]iamMutationGuard{listGuard})
	}
	var snapshot iamRelationshipsSnapshot
	if err == nil {
		snapshot, err = globalIAMSys.nativeRelationshipsLocked(kind, target, time.Now().UTC())
	}
	globalIAMSys.store.runlock()
	if err != nil {
		writeErrorResponseJSON(ctx, w, toAdminAPIErr(ctx, err), r.URL)
		return
	}
	w.Header().Set(iamRelationshipsCapability, "v1")
	w.Header().Set(iamRelationshipsRevision, snapshot.Revision)
	data, _ := json.Marshal(snapshot)
	writeSuccessResponseJSON(w, data)
}

// Bind query identity and server-issued expiry to the graph fingerprint. A
// requester cannot extend a draft lifetime by editing its signed expiry header.
func (sys *IAMSys) nativeRelationshipTokenLocked(kind, target string, expires time.Time) (string, error) {
	graph, err := sys.nativeRelationshipsRevisionLocked()
	if err != nil {
		return "", err
	}
	return iamBindingsRevision("native-relationships:"+kind, target, []string{graph, expires.UTC().Format(time.RFC3339Nano)}, nil), nil
}

// IAM policy ActionSet/ResourceSet marshal map-backed sets as arrays in random
// order. Normalize only fields with known set semantics: credential groups,
// group members and policy action/resource/condition alternatives. Statement
// ordering and any unknown arrays retain their original ordering.
func canonicalIAMGraphJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var canonicalize func(any, []string) error
	canonicalize = func(value any, path []string) error {
		switch typed := value.(type) {
		case map[string]any:
			for key, item := range typed {
				if err := canonicalize(item, append(append([]string{}, path...), key)); err != nil {
					return err
				}
			}
		case []any:
			type encodedValue struct {
				value   any
				encoded string
			}
			encoded := make([]encodedValue, len(typed))
			for index, item := range typed {
				if err := canonicalize(item, append(append([]string{}, path...), "[]")); err != nil {
					return err
				}
				data, err := json.Marshal(item)
				if err != nil {
					return err
				}
				encoded[index] = encodedValue{item, string(data)}
			}
			if iamGraphArrayIsSet(path) {
				sort.Slice(encoded, func(i, j int) bool { return encoded[i].encoded < encoded[j].encoded })
			}
			for index, item := range encoded {
				typed[index] = item.value
			}
		}
		return nil
	}
	if err := canonicalize(value, nil); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func iamGraphArrayIsSet(path []string) bool {
	if len(path) == 3 && (path[0] == "Users" && path[2] == "groups" || path[0] == "Groups" && path[2] == "members") {
		return true
	}
	if len(path) == 5 && path[0] == "Policies" && path[2] == "Statement" && path[3] == "[]" && (path[4] == "Action" || path[4] == "Resource") {
		return true
	}
	return len(path) == 7 && path[0] == "Policies" && path[2] == "Statement" && path[3] == "[]" && path[4] == "Condition"
}
