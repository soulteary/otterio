// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/soulteary/otterio/cmd/logger"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

// Replace only the authoritative current secret. The binding fingerprint also
// covers target secret/status and membership; graph condition covers children.
func (sys *IAMSys) rotateNativeRelationshipUserSecret(ctx context.Context, target, bindingRevision, impactRevision string, expires time.Time, secret string, guard iamMutationGuard) (iamRelationshipMutationResult, error) {
	var result iamRelationshipMutationResult
	if !sys.Initialized() {
		return result, errServerNotInitialized
	}
	if !validIAMBindingName(target) || !validSelfSecret(secret) {
		return result, errInvalidArgument
	}
	sys.store.lock()
	defer sys.store.unlock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, action := range []iampolicy.AdminAction{iampolicy.CreateUserAdminAction, iampolicy.ListUsersAdminAction, iampolicy.GetUserAdminAction} {
		checked := guard
		checked.args.Action = iampolicy.Action(action)
		if err := sys.checkIAMMutationGuardsLocked([]iamMutationGuard{checked}); err != nil {
			return result, err
		}
	}
	now := time.Now().UTC()
	if !expires.After(now) || expires.After(now.Add(30*time.Second)) {
		return result, errIAMBindingConflict
	}
	token, err := sys.nativeRelationshipTokenLocked("user", target, expires)
	if err != nil {
		return result, err
	}
	if token != impactRevision {
		return result, errIAMBindingConflict
	}
	binding, err := sys.nativeBindingsLocked("user", target)
	if err != nil {
		return result, err
	}
	if binding.Revision != bindingRevision {
		return result, errIAMBindingConflict
	}
	impact, err := sys.nativeRelationshipsLocked("user", target, now)
	if err != nil {
		return result, err
	}
	credential := sys.iamUsersMap[target]
	if credential.SecretKey == secret {
		return result, errInvalidArgument
	}
	credential.SecretKey = secret
	if err := sys.store.saveUserIdentity(ctx, target, regularUser, newUserIdentity(credential)); err != nil {
		return result, err
	}
	sys.iamUsersMap[target] = credential
	result.Bindings, err = sys.nativeBindingsLocked("user", target)
	if err != nil {
		return result, errIAMRelationshipCommitted
	}
	impact.Revision, err = sys.nativeRelationshipTokenLocked("user", target, impact.ExpiresAt)
	if err != nil {
		return result, errIAMRelationshipCommitted
	}
	result.Impact = impact
	result.Outcome = "confirmed"
	return result, nil
}

func (a adminAPIHandlers) IAMRelationshipUserSecret(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "IAMRelationshipUserSecret")
	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))
	w.Header().Set("Cache-Control", "no-store")
	query, err := url.ParseQuery(r.URL.RawQuery)
	target := query.Get("target")
	if err != nil || len(query) != 2 || len(query["kind"]) != 1 || query.Get("kind") != "user" || len(query["target"]) != 1 || !validIAMBindingName(target) {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	objectAPI, credential := validateAdminUsersReq(ctx, w, r, iampolicy.CreateUserAdminAction)
	if objectAPI == nil {
		return
	}
	if !iamBindingSupported() {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrNotImplemented), r.URL)
		return
	}
	bindingRevision, impactRevision, expires, err := signedRelationshipCondition(r)
	if err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	if r.ContentLength <= 0 || r.ContentLength > 4096 {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	shaValues := r.Header.Values("X-Amz-Content-Sha256")
	if len(shaValues) != 1 || len(shaValues[0]) != 64 {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrContentSHA256Mismatch), r.URL)
		return
	}
	if _, err := hex.DecodeString(shaValues[0]); err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrContentSHA256Mismatch), r.URL)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	encrypted, err := readConfigurationBody(ctx, r, 4096)
	if err != nil {
		writeErrorResponseJSON(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	if int64(len(encrypted)) != r.ContentLength {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	body, err := madmin.DecryptData(credential.SecretKey, bytes.NewReader(encrypted))
	if err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminConfigBadJSON), r.URL)
		return
	}
	secret, err := decodeSelfCredentialSecret(body)
	if err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	select {
	case iamRelationshipSlots <- struct{}{}:
		defer func() { <-iamRelationshipSlots }()
	default:
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrSlowDown), r.URL)
		return
	}
	result, err := globalIAMSys.rotateNativeRelationshipUserSecret(ctx, target, bindingRevision, impactRevision, expires, secret, newIAMMutationGuard(r, credential, iampolicy.CreateUserAdminAction))
	secret = ""
	if errors.Is(err, errIAMBindingConflict) {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrPreconditionFailed), r.URL)
		return
	}
	if errors.Is(err, errIAMRelationshipCommitted) {
		writeErrorResponseJSON(ctx, w, APIError{Code: "IAMPropagationIncomplete", Description: "The mutation committed but its acknowledgement could not be completed.", HTTPStatusCode: http.StatusServiceUnavailable}, r.URL)
		return
	}
	if err != nil {
		writeErrorResponseJSON(ctx, w, toAdminAPIErr(ctx, err), r.URL)
		return
	}
	w.Header().Set(iamRelationshipsCapability, "v1")
	w.Header().Set(iamRelationshipsRevision, result.Impact.Revision)
	w.Header().Set(iamBindingCapability, "v1")
	w.Header().Set(iamBindingRevision, result.Bindings.Revision)
	data, _ := json.Marshal(result)
	writeSuccessResponseJSON(w, data)
}
