// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"
)

type iamRelationshipUserRequest struct {
	Action string `json:"action"`
	Target string `json:"target"`
}
type iamRelationshipUserInfo struct {
	AccessKey string `json:"accessKey"`
	Status    string `json:"status"`
}
type iamRelationshipUserResult struct {
	Outcome string                   `json:"outcome"`
	User    iamRelationshipUserInfo  `json:"user"`
	Impact  iamRelationshipsSnapshot `json:"impact"`
}

func relationshipUserAction(action string) (iampolicy.AdminAction, bool) {
	switch action {
	case "user.enable", "user.disable":
		return iampolicy.EnableUserAdminAction, true
	case "user.delete":
		return iampolicy.DeleteUserAdminAction, true
	}
	return "", false
}
func decodeRelationshipUserBody(data []byte) (iamRelationshipUserRequest, error) {
	var result iamRelationshipUserRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return result, errInvalidArgument
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return result, errInvalidArgument
		}
		seen[name] = true
		switch name {
		case "action":
			err = decoder.Decode(&result.Action)
		case "target":
			err = decoder.Decode(&result.Target)
		default:
			return result, errInvalidArgument
		}
		if err != nil {
			return result, errInvalidArgument
		}
	}
	end, err := decoder.Token()
	_, valid := relationshipUserAction(result.Action)
	if err != nil || end != json.Delim('}') || decoder.Decode(new(any)) != io.EOF || !valid || !validIAMBindingName(result.Target) || !seen["action"] || !seen["target"] {
		return result, errInvalidArgument
	}
	return result, nil
}
func (sys *IAMSys) mutateNativeRelationshipUser(ctx context.Context, args iamRelationshipUserRequest, revision string, expires time.Time, guard iamMutationGuard) (iamRelationshipUserResult, error) {
	var result iamRelationshipUserResult
	data, _ := json.Marshal(args)
	args, err := decodeRelationshipUserBody(data)
	if err != nil {
		return result, err
	}
	if !sys.Initialized() {
		return result, errServerNotInitialized
	}
	sys.store.lock()
	defer sys.store.unlock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	action, _ := relationshipUserAction(args.Action)
	for _, permission := range []iampolicy.AdminAction{action, iampolicy.GetUserAdminAction, iampolicy.ListUsersAdminAction} {
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
	token, err := sys.nativeRelationshipTokenLocked("user", args.Target, expires)
	if err != nil {
		return result, err
	}
	if token != revision {
		return result, errIAMBindingConflict
	}
	before, err := sys.nativeRelationshipsLocked("user", args.Target, now)
	if err != nil {
		return result, err
	}
	credential := sys.iamUsersMap[args.Target]
	if args.Action == "user.delete" {
		// Preflight the full cascade before the first persistence call. Group detail
		// and effective membership index may diverge; remove both representations.
		groups := []string{}
		for name, group := range sys.iamGroupsMap {
			present := sys.iamUserGroupMemberships[args.Target].Contains(name)
			for _, member := range group.Members {
				present = present || member == args.Target
			}
			if present {
				groups = append(groups, name)
			}
		}
		sort.Strings(groups)
		children := []string{}
		for key, child := range sys.iamUsersMap {
			if child.ParentUser == args.Target {
				if !child.IsTemp() && !child.IsServiceAccount() {
					return result, errIAMActionNotAllowed
				}
				children = append(children, key)
			}
		}
		sort.Strings(children)
		for _, name := range groups {
			group := sys.iamGroupsMap[name]
			members := []string{}
			for _, member := range group.Members {
				if member != args.Target {
					members = append(members, member)
				}
			}
			group.Members = members
			if err := sys.store.saveGroupInfo(ctx, name, group); err != nil {
				return result, errIAMRelationshipCommitted
			}
			sys.iamGroupsMap[name] = group
			sys.iamUserGroupMemberships[args.Target].Remove(name)
		}
		for _, key := range children {
			kind := srvAccUser
			if sys.iamUsersMap[key].IsTemp() {
				kind = stsUser
			}
			if _, mapped := sys.iamUserPolicyMap[key]; mapped {
				if err := sys.store.deleteMappedPolicy(ctx, key, kind, false); err != nil && !errors.Is(err, errNoSuchPolicy) {
					return result, errIAMRelationshipCommitted
				}
				delete(sys.iamUserPolicyMap, key)
			}
			if err := sys.store.deleteUserIdentity(ctx, key, kind); err != nil && !errors.Is(err, errNoSuchUser) {
				return result, errIAMRelationshipCommitted
			}
			delete(sys.iamUsersMap, key)
			delete(sys.iamUserPolicyMap, key)
			delete(sys.iamUserGroupMemberships, key)
		}
		if err := sys.store.deleteMappedPolicy(ctx, args.Target, regularUser, false); err != nil && !errors.Is(err, errNoSuchPolicy) {
			return result, errIAMRelationshipCommitted
		}
		delete(sys.iamUserPolicyMap, args.Target)
		if err := sys.store.deleteUserIdentity(ctx, args.Target, regularUser); err != nil && !errors.Is(err, errNoSuchUser) {
			return result, errIAMRelationshipCommitted
		}
		delete(sys.iamUsersMap, args.Target)
		delete(sys.iamUserGroupMemberships, args.Target)
		result.User = iamRelationshipUserInfo{AccessKey: args.Target, Status: "deleted"}
	} else {
		credential.Status = auth.AccountOn
		status := "enabled"
		if args.Action == "user.disable" {
			credential.Status = auth.AccountOff
			status = "disabled"
		}
		// Preserve all credential fields; status is the only change.
		if err := sys.store.saveUserIdentity(ctx, args.Target, regularUser, newUserIdentity(credential)); err != nil {
			return result, errIAMRelationshipCommitted
		}
		sys.iamUsersMap[args.Target] = credential
		result.User = iamRelationshipUserInfo{AccessKey: args.Target, Status: status}
		after, err := sys.nativeRelationshipsLocked("user", args.Target, now)
		if err != nil {
			return result, errIAMRelationshipCommitted
		}
		seen := map[string]bool{}
		for _, key := range before.AffectedAccessKeys {
			seen[key] = true
		}
		for _, key := range after.AffectedAccessKeys {
			seen[key] = true
		}
		before.AffectedAccessKeys = []string{}
		for key := range seen {
			before.AffectedAccessKeys = append(before.AffectedAccessKeys, key)
		}
		sort.Strings(before.AffectedAccessKeys)
	}
	before.Revision, err = sys.nativeRelationshipTokenLocked("user", args.Target, before.ExpiresAt)
	if err != nil {
		return result, errIAMRelationshipCommitted
	}
	result.Impact = before
	result.Outcome = "confirmed"
	return result, nil
}
func (a adminAPIHandlers) IAMRelationshipUsers(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "IAMRelationshipUsers")
	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))
	w.Header().Set("Cache-Control", "no-store")
	query, err := url.ParseQuery(r.URL.RawQuery)
	target := query.Get("target")
	if err != nil || len(query) != 2 || len(query["kind"]) != 1 || query.Get("kind") != "user" || len(query["target"]) != 1 || !validIAMBindingName(target) {
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
	if r.ContentLength <= 0 || r.ContentLength > 4096 || len(sha) != 1 || len(sha[0]) != 64 {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	if _, err := hex.DecodeString(sha[0]); err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrContentSHA256Mismatch), r.URL)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	data, err := readConfigurationBody(ctx, r, 4096)
	if err != nil {
		writeErrorResponseJSON(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	if int64(len(data)) != r.ContentLength {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	args, err := decodeRelationshipUserBody(data)
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
	action, _ := relationshipUserAction(args.Action)
	result, err := globalIAMSys.mutateNativeRelationshipUser(ctx, args, revision, expires, newIAMMutationGuard(r, credential, action))
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
	data, _ = json.Marshal(result)
	writeSuccessResponseJSON(w, data)
}
