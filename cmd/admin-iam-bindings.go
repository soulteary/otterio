// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/soulteary/otterio-sdk/v7/pkg/set"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

const iamBindingCapability = "X-Otterio-IAM-Bindings"
const iamBindingCondition = "X-Otterio-IAM-If-Match"
const iamBindingRevision = "X-Otterio-IAM-Revision"

var errIAMBindingConflict = errors.New("IAM bindings changed")
var errIAMCreateExists = errors.New("IAM target already exists")

// Revisions fingerprint identity bytes without exposing a dictionary oracle
// for weak secrets. The random process key never leaves the server; restarting
// invalidates open drafts. Native multi-writer deployments are unsupported.
var iamBindingRevisionKey = func() [32]byte {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		panic("cannot initialize IAM binding revision key")
	}
	return key
}()

type iamBindingSnapshot struct {
	Kind        string   `json:"kind"`
	Target      string   `json:"target"`
	Policies    []string `json:"policies"`
	Revision    string   `json:"revision"`
	Conditional bool     `json:"conditional"`
}

// This protocol deliberately covers one native IAM writer. Process-local IAM
// locks cannot implement a distributed compare-and-swap against asynchronous
// peers or shared etcd. Legacy readers and writers remain compatible.
func iamBindingSupported() bool {
	return globalPolicyOPA == nil && iamBindingSupportedFor(globalIAMSys, globalIsDistErasure, globalEtcdClient != nil)
}

func iamBindingSupportedFor(sys *IAMSys, distributed, externalStore bool) bool {
	if sys == nil || !sys.Initialized() || distributed || externalStore || sys.usersSysType != OtterIOUsersSysType {
		return false
	}
	_, ok := sys.store.(*IAMObjectStore)
	return ok
}

func validIAMBindingName(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n/\\,")
}

func canonicalIAMBindings(policies []string) ([]string, error) {
	if len(policies) > 100 {
		return nil, errInvalidArgument
	}
	result := append([]string{}, policies...)
	sort.Strings(result)
	for i, name := range result {
		if !validIAMBindingName(name) || (i > 0 && name == result[i-1]) {
			return nil, errInvalidArgument
		}
	}
	return result, nil
}

func iamBindingsRevision(kind, target string, policies []string, identity any) string {
	data, _ := json.Marshal(struct {
		Kind, Target string
		Policies     []string
		Identity     any
	}{kind, target, policies, identity})
	hash := hmac.New(sha256.New, iamBindingRevisionKey[:])
	_, _ = hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

// Caller holds the same store lock used by every legacy policy mutation.
func (sys *IAMSys) nativeBindingsLocked(kind, target string) (iamBindingSnapshot, error) {
	result := iamBindingSnapshot{Kind: kind, Target: target, Policies: []string{}, Conditional: true}
	var identity any
	if sys.usersSysType != OtterIOUsersSysType || !validIAMBindingName(target) {
		return result, errIAMActionNotAllowed
	}
	if kind == "user" {
		credential, ok := sys.iamUsersMap[target]
		if !ok {
			return result, errNoSuchUser
		}
		if target == globalActiveCred.AccessKey || credential.IsTemp() || credential.IsServiceAccount() {
			return result, errIAMActionNotAllowed
		}
		result.Policies = sys.iamUserPolicyMap[target].toSlice()
		memberships := sys.iamUserGroupMemberships[target].ToSlice()
		sort.Strings(memberships)
		type groupSnapshot struct {
			Name     string
			Info     GroupInfo
			Policies []string
		}
		groupSnapshots := make([]groupSnapshot, 0, len(memberships))
		for _, group := range memberships {
			info := sys.iamGroupsMap[group]
			info.Members = append([]string{}, info.Members...)
			sort.Strings(info.Members)
			groupPolicies := sys.iamGroupPolicyMap[group].toSlice()
			sort.Strings(groupPolicies)
			groupSnapshots = append(groupSnapshots, groupSnapshot{group, info, groupPolicies})
		}
		identity = struct {
			Credential  any
			Memberships []string
			Groups      []groupSnapshot
		}{credential, memberships, groupSnapshots}
	} else if kind == "group" {
		if _, ok := sys.iamGroupsMap[target]; !ok {
			return result, errNoSuchGroup
		}
		result.Policies = sys.iamGroupPolicyMap[target].toSlice()
		group := sys.iamGroupsMap[target]
		group.Members = append([]string{}, group.Members...)
		sort.Strings(group.Members)
		identity = group
	} else {
		return result, errInvalidArgument
	}
	result.Policies = append([]string{}, result.Policies...)
	sort.Strings(result.Policies)
	result.Revision = iamBindingsRevision(kind, target, result.Policies, identity)
	return result, nil
}

func (sys *IAMSys) nativeBindings(kind, target string) (iamBindingSnapshot, error) {
	if !sys.Initialized() {
		return iamBindingSnapshot{}, errServerNotInitialized
	}
	sys.store.rlock()
	defer sys.store.runlock()
	return sys.nativeBindingsLocked(kind, target)
}

func (sys *IAMSys) replaceNativeBindings(kind, target, revision string, policies []string, guards ...iamMutationGuard) (iamBindingSnapshot, error) {
	policies, err := canonicalIAMBindings(policies)
	if err != nil {
		return iamBindingSnapshot{}, err
	}
	if !sys.Initialized() {
		return iamBindingSnapshot{}, errServerNotInitialized
	}
	sys.store.lock()
	defer sys.store.unlock()
	if err := sys.checkIAMMutationGuardsLocked(guards); err != nil {
		return iamBindingSnapshot{}, err
	}
	current, err := sys.nativeBindingsLocked(kind, target)
	if err != nil {
		return current, err
	}
	if current.Revision != revision {
		return current, errIAMBindingConflict
	}
	if err = sys.policyDBSet(target, strings.Join(policies, ","), regularUser, kind == "group"); err != nil {
		return current, err
	}
	return sys.nativeBindingsLocked(kind, target)
}

// IAMPolicyBindings returns or atomically replaces direct named bindings. It
// never expands inherited group policies or accepts service/temporary targets.
func (a adminAPIHandlers) IAMPolicyBindings(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "IAMPolicyBindings")
	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))
	query, err := url.ParseQuery(r.URL.RawQuery)
	kind, target := query.Get("kind"), query.Get("target")
	if err != nil || len(query) != 2 || len(query["kind"]) != 1 || len(query["target"]) != 1 || (kind != "user" && kind != "group") || !validIAMBindingName(target) {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	action := iampolicy.AdminAction(iampolicy.GetUserAdminAction)
	if kind == "group" {
		action = iampolicy.GetGroupAdminAction
	}
	if r.Method == http.MethodPut {
		action = iampolicy.AttachPolicyAdminAction
	}
	objectAPI, credential := validateAdminUsersReq(ctx, w, r, action)
	if objectAPI == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if !iamBindingSupported() {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrNotImplemented), r.URL)
		return
	}
	var snapshot iamBindingSnapshot
	if err := globalIAMSys.validateIAMMutationGuard(newIAMMutationGuard(r, credential, action)); err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAccessDenied), r.URL)
		return
	}
	if r.Method == http.MethodGet {
		snapshot, err = globalIAMSys.nativeBindings(kind, target)
	} else if r.Method == http.MethodPut {
		// Require one exact revision and ensure it participates in the SigV4
		// signature; an unsigned proxy header cannot authorize replacement.
		revisions := r.Header.Values(iamBindingCondition)
		auth := r.Header.Get("Authorization")
		_, signed, ok := strings.Cut(auth, "SignedHeaders=")
		signed, _, _ = strings.Cut(signed, ",")
		if !ok || len(revisions) != 1 || len(revisions[0]) != 64 || !strings.Contains(";"+signed+";", ";"+strings.ToLower(iamBindingCondition)+";") {
			writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
			return
		}
		var body struct {
			Policies []string `json:"policies"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 32*1024+1))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.Policies == nil {
			writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
			return
		}
		snapshot, err = globalIAMSys.replaceNativeBindings(kind, target, revisions[0], body.Policies, newIAMMutationGuard(r, credential, action))
	} else {
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(http.StatusMethodNotAllowed)
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
	w.Header().Set(iamBindingCapability, "v1")
	w.Header().Set(iamBindingRevision, snapshot.Revision)
	writeSuccessResponseJSON(w, encodeResponseJSON(snapshot))
}

type iamCreateRequest struct {
	SecretKey string   `json:"secretKey,omitempty"`
	Members   []string `json:"members,omitempty"`
}

// createNativeIAM checks absence and persists under the existing store lock.
// Legacy AddUser/UpdateGroupMembers are upserts and cannot express this intent.
func (sys *IAMSys) createNativeIAM(kind, target string, args iamCreateRequest, guards ...iamMutationGuard) error {
	if !sys.Initialized() {
		return errServerNotInitialized
	}
	if sys.usersSysType != OtterIOUsersSysType || !validIAMBindingName(target) {
		return errIAMActionNotAllowed
	}
	sys.store.lock()
	defer sys.store.unlock()
	if err := sys.checkIAMMutationGuardsLocked(guards); err != nil {
		return err
	}
	if kind == "user" {
		if target == globalActiveCred.AccessKey || !auth.IsAccessKeyValid(target) || !auth.IsSecretKeyValid(args.SecretKey) || len(args.SecretKey) > 128 || len(args.Members) != 0 {
			return errInvalidArgument
		}
		if _, exists := sys.iamUsersMap[target]; exists {
			return errIAMCreateExists
		}
		identity := newUserIdentity(auth.Credentials{AccessKey: target, SecretKey: args.SecretKey, Status: auth.AccountOn})
		if err := sys.store.saveUserIdentity(context.Background(), target, regularUser, identity); err != nil {
			return err
		}
		sys.iamUsersMap[target] = identity.Credentials
		sys.nativeIdentityEpoch++
		return nil
	}
	if kind != "group" || args.SecretKey != "" || len(args.Members) > 100 {
		return errInvalidArgument
	}
	if _, exists := sys.iamGroupsMap[target]; exists {
		return errIAMCreateExists
	}
	members, err := canonicalIAMBindings(args.Members)
	if err != nil {
		return err
	}
	for _, member := range members {
		credential, exists := sys.iamUsersMap[member]
		if !exists {
			return errNoSuchUser
		}
		if credential.IsTemp() || credential.IsServiceAccount() {
			return errIAMActionNotAllowed
		}
	}
	group := newGroupInfo(members)
	if err := sys.store.saveGroupInfo(context.Background(), target, group); err != nil {
		return err
	}
	sys.iamGroupsMap[target] = group
	sys.nativeIdentityEpoch++
	for _, member := range members {
		membership := sys.iamUserGroupMemberships[member]
		if membership == nil {
			membership = set.CreateStringSet()
		}
		membership.Add(target)
		sys.iamUserGroupMemberships[member] = membership
	}
	return nil
}

// IAMCreate advertises and executes create-only user/group operations. Both
// routes require the same administrative permission. Request credentials and
// secrets remain encrypted; no secret is returned by the server.
func (a adminAPIHandlers) IAMCreate(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "IAMCreate")
	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))
	query, err := url.ParseQuery(r.URL.RawQuery)
	kind, target := query.Get("kind"), query.Get("target")
	if err != nil || len(query) != 2 || len(query["kind"]) != 1 || len(query["target"]) != 1 || (kind != "user" && kind != "group") || !validIAMBindingName(target) {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	action := iampolicy.AdminAction(iampolicy.CreateUserAdminAction)
	if kind == "group" {
		action = iampolicy.AddUserToGroupAdminAction
	}
	objectAPI, credential := validateAdminUsersReq(ctx, w, r, action)
	if objectAPI == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if !iamBindingSupported() {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrNotImplemented), r.URL)
		return
	}
	if err := globalIAMSys.validateIAMMutationGuard(newIAMMutationGuard(r, credential, action)); err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAccessDenied), r.URL)
		return
	}
	if r.Method == http.MethodGet {
		w.Header().Set("X-Otterio-IAM-Create", "v1")
		writeSuccessResponseJSON(w, encodeResponseJSON(struct {
			Kind       string `json:"kind"`
			Target     string `json:"target"`
			CreateOnly bool   `json:"createOnly"`
		}{kind, target, true}))
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	data, err := madmin.DecryptData(credential.SecretKey, io.LimitReader(r.Body, 32*1024+1))
	if err != nil || len(data) > 20*1024 {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	var args iamCreateRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&args) != nil || decoder.Decode(new(any)) != io.EOF {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	err = globalIAMSys.createNativeIAM(kind, target, args, newIAMMutationGuard(r, credential, action))
	if errors.Is(err, errIAMCreateExists) {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrPreconditionFailed), r.URL)
		return
	}
	if err != nil {
		writeErrorResponseJSON(ctx, w, toAdminAPIErr(ctx, err), r.URL)
		return
	}
	w.Header().Set("X-Otterio-IAM-Create", "v1")
	writeSuccessNoContent(w)
}

func (sys *IAMSys) rotateNativeIAMSecret(target, secret string, guards ...iamMutationGuard) error {
	if !sys.Initialized() {
		return errServerNotInitialized
	}
	if sys.usersSysType != OtterIOUsersSysType || !validIAMBindingName(target) || !auth.IsSecretKeyValid(secret) || len(secret) > 128 || !utf8.ValidString(secret) || strings.ContainsAny(secret, "\x00\r\n") {
		return errInvalidArgument
	}
	sys.store.lock()
	defer sys.store.unlock()
	if err := sys.checkIAMMutationGuardsLocked(guards); err != nil {
		return err
	}
	credential, exists := sys.iamUsersMap[target]
	if !exists {
		return errNoSuchUser
	}
	if target == globalActiveCred.AccessKey || credential.IsTemp() || credential.IsServiceAccount() {
		return errIAMActionNotAllowed
	}
	// Copy the authoritative current credential under the lock; preserve its
	// status, memberships and type even after a concurrent administrator edit.
	credential.SecretKey = secret
	identity := newUserIdentity(credential)
	if err := sys.store.saveUserIdentity(context.Background(), target, regularUser, identity); err != nil {
		return err
	}
	sys.iamUsersMap[target] = credential
	sys.nativeIdentityEpoch++
	return nil
}

func (a adminAPIHandlers) IAMUserSecret(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "IAMUserSecret")
	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))
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
	w.Header().Set("Cache-Control", "no-store")
	if !iamBindingSupported() {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrNotImplemented), r.URL)
		return
	}
	if err := globalIAMSys.validateIAMMutationGuard(newIAMMutationGuard(r, credential, iampolicy.CreateUserAdminAction)); err != nil {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAccessDenied), r.URL)
		return
	}
	if r.Method == http.MethodGet {
		w.Header().Set("X-Otterio-IAM-User-Secret", "v1")
		writeSuccessResponseJSON(w, encodeResponseJSON(struct {
			Kind       string `json:"kind"`
			Target     string `json:"target"`
			SecretOnly bool   `json:"secretOnly"`
		}{"user", target, true}))
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	data, err := madmin.DecryptData(credential.SecretKey, io.LimitReader(r.Body, 2048))
	if err != nil || len(data) > 1024 {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	var args struct {
		NewSecretKey string `json:"newSecretKey"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&args) != nil || decoder.Decode(new(any)) != io.EOF {
		writeErrorResponseJSON(ctx, w, errorCodes.ToAPIErr(ErrAdminInvalidArgument), r.URL)
		return
	}
	if err := globalIAMSys.rotateNativeIAMSecret(target, args.NewSecretKey, newIAMMutationGuard(r, credential, iampolicy.CreateUserAdminAction)); err != nil {
		writeErrorResponseJSON(ctx, w, toAdminAPIErr(ctx, err), r.URL)
		return
	}
	w.Header().Set("X-Otterio-IAM-User-Secret", "v1")
	writeSuccessNoContent(w)
}

// Production handlers always supply a guard. Recheck the authenticated
// credential and its current native policies inside the persistence lock.
// Temporary/service identities and external authorization have no safe native
// snapshot here, so this version rejects them rather than borrowing a parent.
type iamMutationGuard struct {
	credential auth.Credentials
	args       iampolicy.Args
}

func newIAMMutationGuard(r *http.Request, credential auth.Credentials, action iampolicy.AdminAction) iamMutationGuard {
	return iamMutationGuard{credential, iampolicy.Args{AccountName: credential.AccessKey, Action: iampolicy.Action(action), ConditionValues: getConditionValues(r, "", credential.AccessKey, nil)}}
}
func (sys *IAMSys) checkIAMMutationGuardsLocked(guards []iamMutationGuard) error {
	if len(guards) > 1 {
		return errIAMActionNotAllowed
	}
	for _, guard := range guards {
		if globalPolicyOPA != nil || guard.credential.IsTemp() || guard.credential.IsServiceAccount() {
			return errIAMActionNotAllowed
		}
		current := globalActiveCred
		owner := guard.credential.AccessKey == current.AccessKey
		if !owner {
			var ok bool
			current, ok = sys.iamUsersMap[guard.credential.AccessKey]
			if !ok {
				return errIAMActionNotAllowed
			}
		}
		if !current.IsValid() || current.Status == auth.AccountOff || current.IsTemp() || current.IsServiceAccount() || subtle.ConstantTimeCompare([]byte(current.SecretKey), []byte(guard.credential.SecretKey)) != 1 {
			return errIAMActionNotAllowed
		}
		if owner {
			continue
		}
		policies, err := sys.policyDBGet(current.AccessKey, false)
		if err != nil {
			return errIAMActionNotAllowed
		}
		combined := iampolicy.Policy{Version: iampolicy.DefaultVersion}
		for _, name := range policies {
			if policy, ok := sys.iamPolicyDocsMap[name]; ok {
				combined.Statements = append(combined.Statements, policy.Statements...)
			}
		}
		if !combined.IsAllowed(guard.args) {
			return errIAMActionNotAllowed
		}
	}
	return nil
}

func (sys *IAMSys) validateIAMMutationGuard(guard iamMutationGuard) error {
	sys.store.rlock()
	defer sys.store.runlock()
	return sys.checkIAMMutationGuardsLocked([]iamMutationGuard{guard})
}
