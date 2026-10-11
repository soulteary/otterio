// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

type iamRelationshipServiceRequest struct {
	Action       string `json:"action"`
	Target       string `json:"target"`
	NewSecretKey string `json:"newSecretKey,omitempty"`
	Policy       string `json:"policy,omitempty"`
}
type iamRelationshipServiceInfo struct {
	AccessKey     string `json:"accessKey"`
	ParentUser    string `json:"parentUser"`
	Status        string `json:"status"`
	ImpliedPolicy bool   `json:"impliedPolicy"`
	Policy        string `json:"policy"`
}
type iamRelationshipServiceResult struct {
	Outcome        string                     `json:"outcome"`
	ServiceAccount iamRelationshipServiceInfo `json:"serviceAccount"`
	Impact         iamRelationshipsSnapshot   `json:"impact"`
}

func relationshipServiceAction(action string) (iampolicy.AdminAction, bool) {
	switch action {
	case "service-account.enable", "service-account.disable", "service-account.rotate", "service-account.policy":
		return iampolicy.UpdateServiceAccountAdminAction, true
	case "service-account.delete":
		return iampolicy.RemoveServiceAccountAdminAction, true
	default:
		return "", false
	}
}
func decodeIAMRelationshipServiceBody(data []byte) (iamRelationshipServiceRequest, error) {
	var result iamRelationshipServiceRequest
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
		case "newSecretKey":
			err = decoder.Decode(&result.NewSecretKey)
		case "policy":
			err = decoder.Decode(&result.Policy)
		default:
			return result, errInvalidArgument
		}
		if err != nil {
			return result, errInvalidArgument
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || decoder.Decode(new(any)) != io.EOF || !seen["action"] || !seen["target"] || !validIAMBindingName(result.Target) {
		return result, errInvalidArgument
	}
	if _, ok := relationshipServiceAction(result.Action); !ok {
		return result, errInvalidArgument
	}
	switch result.Action {
	case "service-account.rotate":
		if !validSelfSecret(result.NewSecretKey) || !seen["newSecretKey"] || seen["policy"] {
			return result, errInvalidArgument
		}
	case "service-account.policy":
		if result.Policy == "" || len(result.Policy) > 16*1024 || !seen["policy"] || seen["newSecretKey"] {
			return result, errInvalidArgument
		}
	default:
		if seen["newSecretKey"] || seen["policy"] {
			return result, errInvalidArgument
		}
	}
	return result, nil
}
func (sys *IAMSys) nativePolicyAllowsLocked(args iampolicy.Args) bool {
	if args.AccountName == globalActiveCred.AccessKey {
		return globalActiveCred.IsValid()
	}
	credential, ok := sys.iamUsersMap[args.AccountName]
	if !ok || !credential.IsValid() || credential.IsTemp() || credential.IsServiceAccount() {
		return false
	}
	policies, err := sys.policyDBGet(args.AccountName, false)
	if err != nil {
		return false
	}
	combined := iampolicy.Policy{Version: iampolicy.DefaultVersion}
	for _, name := range policies {
		if policy, ok := sys.iamPolicyDocsMap[name]; ok {
			combined.Statements = append(combined.Statements, policy.Statements...)
		}
	}
	args.IsOwner = false
	args.DenyOnly = false
	return combined.IsAllowed(args)
}
func serviceInfoFromCredential(credential auth.Credentials) (iamRelationshipServiceInfo, error) {
	result := iamRelationshipServiceInfo{AccessKey: credential.AccessKey, ParentUser: credential.ParentUser, Status: credential.Status, ImpliedPolicy: true, Policy: ""}
	// Existing inherited/embedded restriction must remain visible and unmodified
	// for status/secret changes; reject malformed tokens instead of implying full
	// inherited permissions. Neither token nor secret is returned.
	claims, err := auth.ExtractClaims(credential.SessionToken, globalActiveCred.SecretKey)
	if err != nil {
		return result, errIAMActionNotAllowed
	}
	parent, ok := claims.Lookup(parentClaim)
	if !ok || parent != credential.ParentUser {
		return result, errIAMActionNotAllowed
	}
	kind, ok := claims.Lookup(iamPolicyClaimNameSA())
	if !ok {
		return result, errIAMActionNotAllowed
	}
	if kind == "inherited-policy" {
		return result, nil
	}
	if kind != "embedded-policy" {
		return result, errIAMActionNotAllowed
	}
	encoded, ok := claims.Lookup(iampolicy.SessionPolicyName)
	if !ok {
		return result, errIAMActionNotAllowed
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) > 16*1024 {
		return result, errIAMActionNotAllowed
	}
	policy, err := iampolicy.ParseConfig(bytes.NewReader(data))
	if err != nil || policy.Version == "" {
		return result, errIAMActionNotAllowed
	}
	canonical, err := json.Marshal(policy)
	if err != nil {
		return result, errIAMActionNotAllowed
	}
	result.Policy = string(canonical)
	result.ImpliedPolicy = false
	return result, nil
}
func (sys *IAMSys) mutateNativeRelationshipServiceAccount(ctx context.Context, args iamRelationshipServiceRequest, revision string, expires time.Time, guard iamMutationGuard) (iamRelationshipServiceResult, error) {
	var result iamRelationshipServiceResult
	data, _ := json.Marshal(args)
	args, err := decodeIAMRelationshipServiceBody(data)
	if err != nil {
		return result, err
	}
	action, _ := relationshipServiceAction(args.Action)
	if !sys.Initialized() {
		return result, errServerNotInitialized
	}
	sys.store.lock()
	defer sys.store.unlock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, permission := range []iampolicy.AdminAction{action, iampolicy.GetUserAdminAction, iampolicy.ListUsersAdminAction, iampolicy.ListServiceAccountsAdminAction} {
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
	token, err := sys.nativeRelationshipTokenLocked("service-account", args.Target, expires)
	if err != nil {
		return result, err
	}
	if token != revision {
		return result, errIAMBindingConflict
	}
	impact, err := sys.nativeRelationshipsLocked("service-account", args.Target, now)
	if err != nil {
		return result, err
	}
	credential := sys.iamUsersMap[args.Target]
	info, err := serviceInfoFromCredential(credential)
	if err != nil {
		return result, err
	}
	if args.Action == "service-account.policy" {
		// The legacy escalation helper probes literal action/resource examples;
		// it cannot prove subset containment for arbitrary wildcards/conditions.
		// Only root or the authoritative parent can change this restriction. Other
		// native administrators retain status/secret/delete capabilities, but cannot
		// delegate the parent's data authority through a child policy.
		if guard.credential.AccessKey != globalActiveCred.AccessKey && guard.credential.AccessKey != credential.ParentUser {
			return result, errIAMActionNotAllowed
		}
		policy, err := iampolicy.ParseConfig(strings.NewReader(args.Policy))
		if err != nil || policy.Version == "" || policy.Validate() != nil {
			return result, errInvalidArgument
		}
		callerArgs := guard.args
		callerArgs.IsOwner = guard.credential.AccessKey == globalActiveCred.AccessKey
		if err := validateSubPolicyEscalationWith(callerArgs, policy, sys.nativePolicyAllowsLocked); err != nil {
			return result, errIAMActionNotAllowed
		}
		parentArgs := guard.args
		parentArgs.AccountName = credential.ParentUser
		parentArgs.IsOwner = false
		if err := validateSubPolicyEscalationWith(parentArgs, policy, sys.nativePolicyAllowsLocked); err != nil {
			return result, errIAMActionNotAllowed
		}
		policyBytes, err := json.Marshal(policy)
		if err != nil || len(policyBytes) > 16*1024 {
			return result, errInvalidArgument
		}
		metadata := map[string]interface{}{parentClaim: credential.ParentUser, iamPolicyClaimNameSA(): "embedded-policy", iampolicy.SessionPolicyName: base64.StdEncoding.EncodeToString(policyBytes)}
		credential.SessionToken, err = auth.JWTSignWithAccessKey(args.Target, metadata, globalActiveCred.SecretKey)
		if err != nil {
			return result, err
		}
	} else if args.Action == "service-account.rotate" {
		if args.NewSecretKey == credential.SecretKey {
			return result, errInvalidArgument
		}
		credential.SecretKey = args.NewSecretKey
	} else if args.Action == "service-account.enable" {
		credential.Status = auth.AccountOn
	} else if args.Action == "service-account.disable" {
		credential.Status = auth.AccountOff
	}
	if args.Action == "service-account.delete" {
		// Legacy PolicyDBSet persists direct service mappings in regularUser.
		// Remove them before freeing the key for reuse by another identity.
		if err := sys.store.deleteMappedPolicy(ctx, args.Target, regularUser, false); err != nil && !errors.Is(err, errNoSuchPolicy) {
			return result, errIAMRelationshipCommitted
		}
		delete(sys.iamUserPolicyMap, args.Target)
		sys.nativeIdentityEpoch++
		if err := sys.store.deleteUserIdentity(ctx, args.Target, srvAccUser); err != nil && !errors.Is(err, errNoSuchUser) {
			return result, errIAMRelationshipCommitted
		}
		delete(sys.iamUsersMap, args.Target)
		sys.nativeIdentityEpoch++
		delete(sys.iamUserPolicyMap, args.Target)
		delete(sys.iamUserGroupMemberships, args.Target)
		info.Status = "deleted"
	} else {
		if err := sys.store.saveUserIdentity(ctx, args.Target, srvAccUser, newUserIdentity(credential)); err != nil {
			return result, err
		}
		sys.iamUsersMap[args.Target] = credential
		info, err = serviceInfoFromCredential(credential)
		if err != nil {
			return result, errIAMRelationshipCommitted
		}
	}
	impact.Revision, err = sys.nativeRelationshipTokenLocked("service-account", args.Target, impact.ExpiresAt)
	if err != nil {
		return result, errIAMRelationshipCommitted
	}
	result.ServiceAccount = info
	result.Impact = impact
	result.Outcome = "confirmed"
	return result, nil
}
func (a adminAPIHandlers) IAMRelationshipServiceAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "IAMRelationshipServiceAccounts")
	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))
	w.Header().Set("Cache-Control", "no-store")
	query, err := url.ParseQuery(r.URL.RawQuery)
	target := query.Get("target")
	if err != nil || len(query) != 2 || len(query["kind"]) != 1 || query.Get("kind") != "service-account" || len(query["target"]) != 1 || !validIAMBindingName(target) {
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
	encrypted, err := readConfigurationBody(ctx, r, 32*1024)
	if err != nil {
		writeErrorResponseJSON(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	if int64(len(encrypted)) != r.ContentLength {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	body, err := madmin.DecryptData(credential.SecretKey, bytes.NewReader(encrypted))
	if err != nil || len(body) > 20*1024 {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminConfigBadJSON), r.URL)
		return
	}
	args, err := decodeIAMRelationshipServiceBody(body)
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
	action, _ := relationshipServiceAction(args.Action)
	result, err := globalIAMSys.mutateNativeRelationshipServiceAccount(ctx, args, revision, expires, newIAMMutationGuard(r, credential, action))
	args.NewSecretKey = ""
	args.Policy = ""
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
