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
	"strings"
	"time"

	"github.com/soulteary/otterio/cmd/logger"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
)

const iamImpactCondition = "X-Otterio-IAM-Impact-If-Match"
const iamImpactExpires = "X-Otterio-IAM-Impact-Expires"

var errIAMRelationshipCommitted = errors.New("IAM mutation committed but acknowledgement incomplete")

type iamRelationshipMutationResult struct {
	Outcome  string                   `json:"outcome"`
	Bindings iamBindingSnapshot       `json:"bindings"`
	Impact   iamRelationshipsSnapshot `json:"impact"`
}

// Caller may only replace direct named bindings. Both the legacy binding CAS
// and authoritative identity graph token are checked under the same write lock.
func (sys *IAMSys) replaceNativeRelationshipBindings(kind, target, bindingRevision, impactRevision string, expires time.Time, policies []string, guard iamMutationGuard) (iamRelationshipMutationResult, error) {
	var result iamRelationshipMutationResult
	policies, err := canonicalIAMBindings(policies)
	if err != nil {
		return result, err
	}
	if !sys.Initialized() {
		return result, errServerNotInitialized
	}
	sys.store.lock()
	defer sys.store.unlock()
	if err := sys.checkIAMMutationGuardsLocked([]iamMutationGuard{guard}); err != nil {
		return result, err
	}
	list := guard
	list.args.Action = iampolicy.ListUsersAdminAction
	if err := sys.checkIAMMutationGuardsLocked([]iamMutationGuard{list}); err != nil {
		return result, err
	}
	detail := guard
	detail.args.Action = iampolicy.GetUserAdminAction
	if kind == "group" {
		detail.args.Action = iampolicy.GetGroupAdminAction
	}
	if err := sys.checkIAMMutationGuardsLocked([]iamMutationGuard{detail}); err != nil {
		return result, err
	}
	now := time.Now().UTC()
	if !expires.After(now) || expires.After(now.Add(30*time.Second)) {
		return result, errIAMBindingConflict
	}
	currentToken, err := sys.nativeRelationshipTokenLocked(kind, target, expires)
	if err != nil {
		return result, err
	}
	if currentToken != impactRevision {
		return result, errIAMBindingConflict
	}
	current, err := sys.nativeBindingsLocked(kind, target)
	if err != nil {
		return result, err
	}
	if current.Revision != bindingRevision {
		return result, errIAMBindingConflict
	}
	impact, err := sys.nativeRelationshipsLocked(kind, target, now)
	if err != nil {
		return result, err
	}
	if err := sys.policyDBSet(target, strings.Join(policies, ","), regularUser, kind == "group"); err != nil {
		return result, err
	}
	result.Bindings, err = sys.nativeBindingsLocked(kind, target)
	if err != nil {
		return result, errIAMRelationshipCommitted
	}
	// Binding changes do not alter relationship membership. The impact contains
	// the exact pre-commit affected principals, with a fresh post-commit graph token.
	impact.Revision, err = sys.nativeRelationshipTokenLocked(kind, target, impact.ExpiresAt)
	if err != nil {
		return result, errIAMRelationshipCommitted
	}
	result.Impact = impact
	result.Outcome = "confirmed"
	return result, nil
}

func signedRelationshipCondition(r *http.Request) (string, string, time.Time, error) {
	var expires time.Time
	_, signed, ok := strings.Cut(r.Header.Get("Authorization"), "SignedHeaders=")
	signed, _, _ = strings.Cut(signed, ",")
	values := []string{}
	for _, name := range []string{iamBindingCondition, iamImpactCondition, iamImpactExpires} {
		header := r.Header.Values(name)
		if !ok || len(header) != 1 || !strings.Contains(";"+signed+";", ";"+strings.ToLower(name)+";") {
			return "", "", expires, errInvalidArgument
		}
		values = append(values, header[0])
	}
	if len(values[0]) != 64 || len(values[1]) != 64 {
		return "", "", expires, errInvalidArgument
	}
	expires, err := time.Parse(time.RFC3339Nano, values[2])
	if err != nil {
		return "", "", expires, errInvalidArgument
	}
	return values[0], values[1], expires, nil
}
func (a adminAPIHandlers) IAMRelationshipBindings(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "IAMRelationshipBindings")
	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))
	w.Header().Set("Cache-Control", "no-store")
	query, err := url.ParseQuery(r.URL.RawQuery)
	kind, target := query.Get("kind"), query.Get("target")
	if err != nil || len(query) != 2 || len(query["kind"]) != 1 || len(query["target"]) != 1 || !validIAMBindingName(target) || (kind != "user" && kind != "group") {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	objectAPI, credential := validateAdminUsersReq(ctx, w, r, iampolicy.AttachPolicyAdminAction)
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
	var body struct {
		Policies []string `json:"policies"`
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
	data, err := readConfigurationBody(ctx, r, 32*1024)
	if err != nil {
		writeErrorResponseJSON(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	if int64(len(data)) != r.ContentLength {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.Policies == nil {
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
	result, err := globalIAMSys.replaceNativeRelationshipBindings(kind, target, bindingRevision, impactRevision, expires, body.Policies, newIAMMutationGuard(r, credential, iampolicy.AttachPolicyAdminAction))
	if errors.Is(err, errIAMRelationshipCommitted) {
		writeErrorResponseJSON(ctx, w, APIError{Code: "IAMPropagationIncomplete", Description: "The mutation committed but its acknowledgement could not be completed.", HTTPStatusCode: http.StatusServiceUnavailable}, r.URL)
		return
	}
	if errors.Is(err, errIAMBindingConflict) {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrPreconditionFailed), r.URL)
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
	data, _ = json.Marshal(result)
	writeSuccessResponseJSON(w, data)
}
