// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/soulteary/otterio-sdk/v7/pkg/set"
	"github.com/soulteary/otterio/cmd/logger"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
)

type iamRelationshipGroupRequest struct {
	Action  string   `json:"action"`
	Target  string   `json:"target"`
	Members []string `json:"members,omitempty"`
}
type iamRelationshipGroupInfo struct {
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Members  []string `json:"members"`
	Policies []string `json:"policies"`
}
type iamRelationshipGroupResult struct {
	Outcome string                   `json:"outcome"`
	Group   iamRelationshipGroupInfo `json:"group"`
	Impact  iamRelationshipsSnapshot `json:"impact"`
}

func iamRelationshipGroupAction(action string) (iampolicy.AdminAction, bool) {
	switch action {
	case "group.add-members":
		return iampolicy.AddUserToGroupAdminAction, true
	case "group.remove-members", "group.delete":
		return iampolicy.RemoveUserFromGroupAdminAction, true
	case "group.enable":
		return iampolicy.EnableGroupAdminAction, true
	case "group.disable":
		return iampolicy.DisableGroupAdminAction, true
	default:
		return "", false
	}
}
func decodeIAMRelationshipGroupBody(data []byte) (iamRelationshipGroupRequest, error) {
	var result iamRelationshipGroupRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	seen := map[string]bool{}
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return result, errInvalidArgument
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return result, errInvalidArgument
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return result, errInvalidArgument
		}
		seen[name] = true
		switch name {
		case "action":
			err = decoder.Decode(&result.Action)
		case "target":
			err = decoder.Decode(&result.Target)
		case "members":
			err = decoder.Decode(&result.Members)
		default:
			return result, errInvalidArgument
		}
		if err != nil {
			return result, errInvalidArgument
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || decoder.Decode(new(any)) != io.EOF || !seen["action"] || !seen["target"] {
		return result, errInvalidArgument
	}
	action, ok := iamRelationshipGroupAction(result.Action)
	if !ok || action == "" || !validIAMBindingName(result.Target) {
		return result, errInvalidArgument
	}
	membership := result.Action == "group.add-members" || result.Action == "group.remove-members"
	if membership && len(result.Members) == 0 {
		return result, errInvalidArgument
	}
	if !membership && len(result.Members) != 0 {
		return result, errInvalidArgument
	}
	members, err := canonicalIAMBindings(result.Members)
	if err != nil {
		return result, err
	}
	result.Members = members
	return result, nil
}
func (sys *IAMSys) mutateNativeRelationshipGroup(ctx context.Context, args iamRelationshipGroupRequest, revision string, expires time.Time, guard iamMutationGuard) (iamRelationshipGroupResult, error) {
	var result iamRelationshipGroupResult
	action, ok := iamRelationshipGroupAction(args.Action)
	if !ok || !validIAMBindingName(args.Target) {
		return result, errInvalidArgument
	}
	// Enforce the same body constraints for non-HTTP callers/tests.
	encoded, _ := json.Marshal(args)
	validated, err := decodeIAMRelationshipGroupBody(encoded)
	if err != nil {
		return result, err
	}
	args = validated
	if !sys.Initialized() {
		return result, errServerNotInitialized
	}
	sys.store.lock()
	defer sys.store.unlock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, permission := range []iampolicy.AdminAction{action, iampolicy.GetGroupAdminAction, iampolicy.ListUsersAdminAction} {
		checked := guard
		checked.args.Action = iampolicy.Action(permission)
		if err := sys.checkIAMMutationGuardsLocked([]iamMutationGuard{checked}); err != nil {
			return result, err
		}
	}
	now := time.Now().UTC()
	if !expires.After(now) || expires.After(now.Add(30*time.Second)) {
		return result, errIAMBindingConflict
	}
	token, err := sys.nativeRelationshipTokenLocked("group", args.Target, expires)
	if err != nil {
		return result, err
	}
	if token != revision {
		return result, errIAMBindingConflict
	}
	before, err := sys.nativeRelationshipsLocked("group", args.Target, now)
	if err != nil {
		return result, err
	}
	group := sys.iamGroupsMap[args.Target]
	group.Members = append([]string{}, group.Members...)
	for _, member := range args.Members {
		credential, ok := sys.iamUsersMap[member]
		if !ok {
			return result, errNoSuchUser
		}
		if credential.IsTemp() || credential.IsServiceAccount() {
			return result, errIAMActionNotAllowed
		}
	}
	policies := append([]string{}, sys.iamGroupPolicyMap[args.Target].toSlice()...)
	sort.Strings(policies)
	if args.Action == "group.delete" {
		if len(group.Members) != 0 {
			return result, errGroupNotEmpty
		}
		if err := sys.store.deleteMappedPolicy(ctx, args.Target, regularUser, true); err != nil && !errors.Is(err, errNoSuchPolicy) {
			return result, err
		}
		// Reflect the first persisted step even if group deletion later fails.
		delete(sys.iamGroupPolicyMap, args.Target)
		if err := sys.store.deleteGroupInfo(ctx, args.Target); err != nil && !errors.Is(err, errNoSuchGroup) {
			return result, errIAMRelationshipCommitted
		}
		delete(sys.iamGroupsMap, args.Target)
		for user, membership := range sys.iamUserGroupMemberships {
			if membership.Contains(args.Target) {
				membership.Remove(args.Target)
				sys.iamUserGroupMemberships[user] = membership
			}
		}
		result.Group = iamRelationshipGroupInfo{Name: args.Target, Status: "deleted", Members: []string{}, Policies: []string{}}
	} else {
		members := set.CreateStringSet(group.Members...)
		switch args.Action {
		case "group.add-members":
			for _, member := range args.Members {
				members.Add(member)
			}
		case "group.remove-members":
			for _, member := range args.Members {
				members.Remove(member)
			}
		case "group.enable":
			group.Status = statusEnabled
		case "group.disable":
			group.Status = statusDisabled
		}
		group.Members = members.ToSlice()
		sort.Strings(group.Members)
		if err := sys.store.saveGroupInfo(ctx, args.Target, group); err != nil {
			return result, err
		}
		sys.iamGroupsMap[args.Target] = group
		for _, member := range args.Members {
			membership := sys.iamUserGroupMemberships[member]
			if membership == nil {
				membership = set.CreateStringSet()
			}
			if args.Action == "group.add-members" {
				membership.Add(args.Target)
			} else {
				membership.Remove(args.Target)
			}
			sys.iamUserGroupMemberships[member] = membership
		}
		result.Group = iamRelationshipGroupInfo{Name: args.Target, Status: group.Status, Members: append([]string{}, group.Members...), Policies: policies}
	}
	impact := before
	if args.Action != "group.delete" {
		after, err := sys.nativeRelationshipsLocked("group", args.Target, now)
		if err != nil {
			return result, errIAMRelationshipCommitted
		}
		affected := set.CreateStringSet(before.AffectedAccessKeys...)
		for _, key := range after.AffectedAccessKeys {
			affected.Add(key)
		}
		impact.AffectedAccessKeys = affected.ToSlice()
		sort.Strings(impact.AffectedAccessKeys)
		if impact.AffectedAccessKeys == nil {
			impact.AffectedAccessKeys = []string{}
		}
	}
	impact.Revision, err = sys.nativeRelationshipTokenLocked("group", args.Target, impact.ExpiresAt)
	if err != nil {
		return result, errIAMRelationshipCommitted
	}
	result.Impact = impact
	result.Outcome = "confirmed"
	return result, nil
}
func signedGroupRelationshipCondition(r *http.Request) (string, time.Time, error) {
	values := r.Header.Values(iamImpactCondition)
	expiry := r.Header.Values(iamImpactExpires)
	_, signed, ok := strings.Cut(r.Header.Get("Authorization"), "SignedHeaders=")
	signed, _, _ = strings.Cut(signed, ",")
	if !ok || len(values) != 1 || len(values[0]) != 64 || len(expiry) != 1 {
		return "", time.Time{}, errInvalidArgument
	}
	for _, name := range []string{iamImpactCondition, iamImpactExpires} {
		if !strings.Contains(";"+signed+";", ";"+strings.ToLower(name)+";") {
			return "", time.Time{}, errInvalidArgument
		}
	}
	if _, err := hex.DecodeString(values[0]); err != nil {
		return "", time.Time{}, errInvalidArgument
	}
	expires, err := time.Parse(time.RFC3339Nano, expiry[0])
	if err != nil {
		return "", time.Time{}, errInvalidArgument
	}
	return values[0], expires, nil
}
func (a adminAPIHandlers) IAMRelationshipGroups(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "IAMRelationshipGroups")
	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))
	w.Header().Set("Cache-Control", "no-store")
	query, err := url.ParseQuery(r.URL.RawQuery)
	target := query.Get("target")
	if err != nil || len(query) != 2 || len(query["kind"]) != 1 || query.Get("kind") != "group" || len(query["target"]) != 1 || !validIAMBindingName(target) {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	if newObjectLayerFn() == nil || globalNotificationSys == nil || globalIAMSys == nil || !globalIAMSys.Initialized() {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}
	credential, _, _, code := validateAdminSignature(ctx, r, "")
	if code != ErrNone {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(code), r.URL)
		return
	}
	if !iamBindingSupported() {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrNotImplemented), r.URL)
		return
	}
	revision, expires, err := signedGroupRelationshipCondition(r)
	if err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	sha := r.Header.Values("X-Amz-Content-Sha256")
	if r.ContentLength <= 0 || r.ContentLength > 32*1024 || len(sha) != 1 || len(sha[0]) != 64 {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	if _, err := hex.DecodeString(sha[0]); err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrContentSHA256Mismatch), r.URL)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, err := readConfigurationBody(ctx, r, 32*1024)
	if err != nil {
		writeErrorResponseJSON(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	if int64(len(body)) != r.ContentLength {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	args, err := decodeIAMRelationshipGroupBody(body)
	if err != nil || args.Target != target {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	select {
	case iamRelationshipSlots <- struct{}{}:
		defer func() { <-iamRelationshipSlots }()
	default:
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrSlowDown), r.URL)
		return
	}
	action, _ := iamRelationshipGroupAction(args.Action)
	result, err := globalIAMSys.mutateNativeRelationshipGroup(ctx, args, revision, expires, newIAMMutationGuard(r, credential, action))
	if errors.Is(err, errIAMBindingConflict) {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrPreconditionFailed), r.URL)
		return
	}
	if errors.Is(err, errIAMRelationshipCommitted) {
		writeErrorResponseJSON(ctx, w, APIError{Code: "IAMPropagationIncomplete", Description: "The mutation may have committed but its acknowledgement could not be completed.", HTTPStatusCode: http.StatusServiceUnavailable}, r.URL)
		return
	}
	if err != nil {
		writeErrorResponseJSON(ctx, w, toAdminAPIErr(ctx, err), r.URL)
		return
	}
	w.Header().Set(iamRelationshipsCapability, "v1")
	w.Header().Set(iamRelationshipsRevision, result.Impact.Revision)
	data, _ := json.Marshal(result)
	writeSuccessResponseJSON(w, data)
}
