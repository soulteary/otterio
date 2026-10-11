// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

// The epoch is process-local: in-flight requests cannot survive a restart.
// It prevents native delete/recreate ABA even when credentials are identical.
type nativeServiceCreationProof struct {
	epoch       uint64
	actor       auth.Credentials
	parent      string
	fingerprint string
	revision    string
	expires     time.Time
}

func (sys *IAMSys) captureNativeServiceCreation(actor auth.Credentials) (nativeServiceCreationProof, error) {
	sys.store.rlock()
	defer sys.store.runlock()
	proof := nativeServiceCreationProof{epoch: sys.nativeIdentityEpoch, actor: actor}
	// Authentication is fresh here; authorization is repeated after decoding.
	guard := iamMutationGuard{credential: actor, args: iampolicy.Args{AccountName: actor.AccessKey, DenyOnly: true}}
	if err := sys.checkIAMMutationGuardsLocked([]iamMutationGuard{guard}); err != nil {
		return proof, err
	}
	return proof, nil
}
func (sys *IAMSys) captureNativeServiceParent(proof nativeServiceCreationProof, parent string) (nativeServiceCreationProof, error) {
	sys.store.rlock()
	defer sys.store.runlock()
	credential, ok := sys.iamUsersMap[parent]
	if proof.epoch != sys.nativeIdentityEpoch || !ok || parent == globalActiveCred.AccessKey || !credential.IsValid() || credential.Status == auth.AccountOff || credential.IsTemp() || credential.IsServiceAccount() || credential.ParentUser != "" {
		return proof, errIAMActionNotAllowed
	}
	proof.parent = parent
	proof.fingerprint = iamBindingsRevision("service-create-parent", parent, nil, credential)
	return proof, nil
}
func (sys *IAMSys) createNativeServiceAccount(ctx context.Context, proof nativeServiceCreationProof, groups []string, opts newServiceAccountOpts, guard iamMutationGuard, callerArgs iampolicy.Args) (auth.Credentials, error) {
	var policyBuf []byte
	if opts.sessionPolicy != nil {
		if err := opts.sessionPolicy.Validate(); err != nil {
			return auth.Credentials{}, err
		}
		var err error
		policyBuf, err = json.Marshal(opts.sessionPolicy)
		if err != nil {
			return auth.Credentials{}, err
		}
		if len(policyBuf) > 16*humanize.KiByte {
			return auth.Credentials{}, errInvalidArgument
		}
	}
	sys.store.lock()
	defer sys.store.unlock()
	if err := ctx.Err(); err != nil {
		return auth.Credentials{}, err
	}
	if err := sys.checkIAMMutationGuardsLocked([]iamMutationGuard{guard}); err != nil {
		return auth.Credentials{}, err
	}
	if proof.revision != "" {
		now := time.Now().UTC()
		if !proof.expires.After(now) || proof.expires.After(now.Add(30*time.Second)) {
			return auth.Credentials{}, errIAMBindingConflict
		}
		token, err := sys.nativeRelationshipTokenLocked("user", proof.parent, proof.expires)
		if err != nil {
			return auth.Credentials{}, err
		}
		if token != proof.revision {
			return auth.Credentials{}, errIAMBindingConflict
		}
	}
	if proof.epoch != sys.nativeIdentityEpoch || guard.credential.AccessKey != proof.actor.AccessKey || guard.credential.SecretKey != proof.actor.SecretKey || proof.parent == "" || callerArgs.AccountName != proof.actor.AccessKey {
		return auth.Credentials{}, errIAMActionNotAllowed
	}
	parent, ok := sys.iamUsersMap[proof.parent]
	if !ok || proof.fingerprint != iamBindingsRevision("service-create-parent", proof.parent, nil, parent) {
		return auth.Credentials{}, errIAMActionNotAllowed
	}
	if err := validateSubPolicyEscalationWith(callerArgs, opts.sessionPolicy, sys.nativePolicyAllowsLocked); err != nil {
		return auth.Credentials{}, errIAMActionNotAllowed
	}
	return sys.newServiceAccountLocked(proof.parent, groups, opts, policyBuf)
}

const iamServiceCreationCapability = "X-Otterio-IAM-Service-Creation"

func signedNativeServiceCreation(r *http.Request) error {
	values := r.Header.Values(iamServiceCreationCapability)
	_, signed, ok := strings.Cut(r.Header.Get("Authorization"), "SignedHeaders=")
	signed, _, _ = strings.Cut(signed, ",")
	if !ok || len(values) != 1 || values[0] != "v1" || !strings.Contains(";"+signed+";", ";"+strings.ToLower(iamServiceCreationCapability)+";") {
		return errInvalidArgument
	}
	return nil
}

func readProtectedServiceCreation(ctx context.Context, r *http.Request, secret string) ([]byte, error) {
	sha := r.Header.Values("X-Amz-Content-Sha256")
	if r.ContentLength <= 0 || r.ContentLength > 32*1024 || len(sha) != 1 || len(sha[0]) != 64 {
		return nil, errInvalidArgument
	}
	if _, err := hex.DecodeString(sha[0]); err != nil {
		return nil, errInvalidArgument
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	data, err := readConfigurationBody(ctx, r, 32*1024)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != r.ContentLength {
		return nil, errInvalidArgument
	}
	return madmin.DecryptData(secret, bytes.NewReader(data))
}
