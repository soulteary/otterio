// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

const selfCredentialsHeader = "X-Otterio-Self-Credentials"

type selfCredentialsInfo struct {
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	CanRotateSecret bool   `json:"canRotateSecret"`
}

var errSelfSecretConflict = errors.New("the signed credential has changed")

func selfCredentialsKind(cred auth.Credentials, owner bool, usersType UsersSysType) string {
	switch {
	case owner:
		return "root"
	case cred.IsServiceAccount():
		return "service"
	case cred.IsTemp():
		return "sts"
	case usersType != OtterIOUsersSysType:
		return "directory"
	default:
		return "iam"
	}
}

func selfSecretArgs(r *http.Request, cred auth.Credentials, claims map[string]interface{}) iampolicy.Args {
	return iampolicy.Args{
		AccountName: cred.AccessKey, Groups: cred.Groups, Action: iampolicy.CreateUserAdminAction,
		ConditionValues: getConditionValues(r, "", cred.AccessKey, claims), Claims: claims, DenyOnly: true,
	}
}

// IAM's in-memory lock cannot serialize another node's cache or an external
// authorization service. Rotation remains disabled in those deployments.
func selfCredentialsRotationSupported() bool {
	if globalIsDistErasure || globalEtcdClient != nil || globalPolicyOPA != nil || globalIAMSys == nil {
		return false
	}
	_, ok := globalIAMSys.store.(*IAMObjectStore)
	return ok
}

// SelfCredentials exposes only identity class and an advisory rotation hint.
// PUT performs the authorization again; this response grants no capabilities.
func (a adminAPIHandlers) SelfCredentials(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "SelfCredentials")
	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))
	if newObjectLayerFn() == nil || globalNotificationSys == nil || !globalIAMSys.Initialized() {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}
	cred, claims, owner, code := validateAdminSignature(ctx, r, "")
	if code != ErrNone {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(code), r.URL)
		return
	}
	w.Header().Set(selfCredentialsHeader, "v1")
	kind := selfCredentialsKind(cred, owner, globalIAMSys.usersSysType)
	status := "enabled"
	if cred.Status == auth.AccountOff {
		status = "disabled"
	}
	args := selfSecretArgs(r, cred, claims)
	allowed := kind == "iam" && status == "enabled" && selfCredentialsRotationSupported() && globalIAMSys.IsAllowed(args)
	if r.Method == http.MethodGet {
		data, _ := json.Marshal(selfCredentialsInfo{Kind: kind, Status: status, CanRotateSecret: allowed})
		writeSuccessResponseJSON(w, data)
		return
	}
	if r.Method != http.MethodPut || !allowed {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAccessDenied), r.URL)
		return
	}
	if len(r.URL.RawQuery) != 0 || r.ContentLength <= 0 || r.ContentLength > 4096 {
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
	// Consume the hash reader through EOF before decryption. Limiting it to
	// ContentLength can manufacture EOF without checking the underlying digest.
	encrypted, err := readConfigurationBody(ctx, r, 4096)
	if err != nil {
		writeErrorResponseJSON(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	if int64(len(encrypted)) != r.ContentLength {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	// The body is encrypted with the currently signed secret, just like other
	// admin credential mutations. Never include decrypt/decode details in logs.
	data, err := madmin.DecryptData(cred.SecretKey, bytes.NewReader(encrypted))
	if err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminConfigBadJSON), r.URL)
		return
	}
	secret, err := decodeSelfCredentialSecret(data)
	if err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	if err = globalIAMSys.RotateSelfSecret(ctx, cred.AccessKey, cred.SecretKey, secret, args); err != nil {
		if errors.Is(err, errSelfSecretConflict) {
			writeErrorResponseJSON(ctx, w, APIError{Code: "CredentialConflict", Description: "The credential changed before this request completed.", HTTPStatusCode: http.StatusConflict}, r.URL)
		} else if errors.Is(err, errInvalidArgument) {
			writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		} else if errors.Is(err, errIAMActionNotAllowed) {
			writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAccessDenied), r.URL)
		} else {
			writeErrorResponseJSON(ctx, w, toAdminAPIErr(ctx, err), r.URL)
		}
		return
	}
	// Persistence has committed. A peer reload failure is an uncertain global
	// outcome, rather than an invitation to retry with the old secret.
	for _, peer := range globalNotificationSys.LoadUserWithContext(ctx, cred.AccessKey, false) {
		if peer.Err != nil {
			writeErrorResponseJSON(ctx, w, APIError{Code: "CredentialPropagationIncomplete", Description: "The credential was saved, but a peer reload could not be confirmed.", HTTPStatusCode: http.StatusServiceUnavailable}, r.URL)
			return
		}
	}
	writeSuccessNoContent(w)
}

func validSelfSecret(secret string) bool {
	return len(secret) >= 8 && len(secret) <= 128 && utf8.ValidString(secret) && !strings.ContainsAny(secret, "\x00\r\n")
}

func decodeSelfCredentialSecret(data []byte) (string, error) {
	if len(data) > 1024 || !validConfigurationJSONEncoding(data) {
		return "", errInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') || !decoder.More() {
		return "", errInvalidArgument
	}
	key, err := decoder.Token()
	if err != nil || key != "newSecretKey" {
		return "", errInvalidArgument
	}
	var secret string
	if decoder.Decode(&secret) != nil || decoder.More() {
		return "", errInvalidArgument
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || decoder.Decode(new(interface{})) != io.EOF || !validSelfSecret(secret) {
		return "", errInvalidArgument
	}
	return secret, nil
}

// RotateSelfSecret compares the authenticated secret under the same lock that
// protects disable/delete and persistence. Only the secret field is replaced.
func (sys *IAMSys) RotateSelfSecret(ctx context.Context, accessKey, expected, secret string, args iampolicy.Args) error {
	if !sys.Initialized() {
		return errServerNotInitialized
	}
	if sys.usersSysType != OtterIOUsersSysType || !selfCredentialsRotationSupported() || accessKey == globalActiveCred.AccessKey || !validSelfSecret(secret) {
		return errIAMActionNotAllowed
	}
	store, ok := sys.store.(*IAMObjectStore)
	if !ok {
		return errIAMActionNotAllowed
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !store.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	defer store.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cred, ok := sys.iamUsersMap[accessKey]
	if !ok {
		return errNoSuchUser
	}
	if cred.IsTemp() || cred.IsServiceAccount() || cred.Status == auth.AccountOff || !cred.IsValid() {
		return errIAMActionNotAllowed
	}
	if subtle.ConstantTimeCompare([]byte(cred.SecretKey), []byte(expected)) != 1 {
		return errSelfSecretConflict
	}
	// Re-evaluate native IAM policy mappings under this same store lock so
	// revocation between HTTP authorization and persistence cannot be lost.
	args.AccountName, args.Groups = accessKey, cred.Groups
	args.Action, args.IsOwner, args.DenyOnly = iampolicy.CreateUserAdminAction, false, true
	if !sys.selfSecretAllowedLocked(args) {
		return errIAMActionNotAllowed
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(expected)) == 1 {
		return errInvalidArgument
	}
	cred.SecretKey = secret
	if err := sys.store.saveUserIdentity(ctx, accessKey, regularUser, newUserIdentity(cred)); err != nil {
		return err
	}
	sys.iamUsersMap[accessKey] = cred
	return nil
}

// Caller holds sys.store's write lock. Calling IsAllowed here would recursively
// acquire that lock; use its regular-user native policy evaluation directly.
func (sys *IAMSys) selfSecretAllowedLocked(args iampolicy.Args) bool {
	policies, err := sys.policyDBGet(args.AccountName, false)
	if err != nil {
		return false
	}
	for _, group := range args.Groups {
		groupPolicies, err := sys.policyDBGet(group, true)
		if err != nil {
			return false
		}
		policies = append(policies, groupPolicies...)
	}
	combined := iampolicy.Policy{Version: iampolicy.DefaultVersion}
	for _, name := range policies {
		if p, ok := sys.iamPolicyDocsMap[name]; ok {
			combined.Statements = append(combined.Statements, p.Statements...)
		}
	}
	return len(combined.Statements) != 0 && combined.IsAllowed(args)
}
