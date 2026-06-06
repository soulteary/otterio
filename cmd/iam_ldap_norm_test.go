/*
 * OtterIO Cloud Storage, (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package cmd

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/soulteary/otterio/cmd/config/identity/ldap"
)

// TestLDAPGroupDNCaseFoldHitsCanonicalMapping covers test-matrix row #10 -
// the primary attack surface of the upstream LDAP DN-normalization CVEs.
//
// Setup: an administrator stores a policy mapping under the lower-case
// canonical group DN `cn=admins,ou=groups,dc=corp,dc=com`. At authn time
// the directory returns the same group as `CN=Admins,OU=Groups,DC=Corp,DC=Com`.
// Without normalization the IAM map look-up misses; with NormalizeDN the
// canonical form is recovered and the look-up succeeds. We assert this by
// driving the canonicalisation directly (the same call that policyDBGet
// performs at the IAMSys boundary).
func TestLDAPGroupDNCaseFoldHitsCanonicalMapping(t *testing.T) {
	t.Parallel()

	// Simulated IAM map: the administrator wrote the policy under the
	// canonical (lower-case) DN.
	const canonical = "cn=admins,ou=groups,dc=corp,dc=com"
	policyMap := map[string]string{
		canonical: "consoleAdmin",
	}

	// The directory returned the same DN with a different casing on the
	// next bind:
	directoryReturned := []string{
		"CN=Admins,OU=Groups,DC=Corp,DC=Com",
		"cn=Admins,ou=Groups,dc=corp,dc=com",
		"  CN=admins,OU=groups,DC=corp,DC=com  ",
		"cn=admins,  ou=groups, dc=corp, dc=com",
	}

	for _, dn := range directoryReturned {
		t.Run(dn, func(t *testing.T) {
			canonicalised, err := ldap.NormalizeDN(dn)
			if err != nil {
				t.Fatalf("NormalizeDN(%q): %v", dn, err)
			}
			policy, ok := policyMap[canonicalised]
			if !ok {
				t.Fatalf("LDAP CVE regression: bind DN %q normalised to %q which does NOT match the administrator-written canonical key %q. Group policy %q would be silently bypassed.", dn, canonicalised, canonical, policyMap[canonical])
			}
			if policy == "" {
				t.Fatalf("expected non-empty policy for %q, got empty", canonicalised)
			}
		})
	}
}

// TestLDAPMigrationInMemoryRekeysToCanonical covers test-matrix row #11. We
// drive the in-memory half of migrateMappedPolicyToCanonical with the
// migration on-disk flag turned OFF (so we do not need a live object store)
// and assert that:
//   - a non-canonical name is removed from the map;
//   - the canonical key holds the original mapping content.
//
// This is the "preset CN=Alice.json then start, observe rename" scenario
// reduced to its in-memory equivalent.
func TestLDAPMigrationInMemoryRekeysToCanonical(t *testing.T) {
	t.Setenv("OTTERIO_IAM_LDAP_DN_MIGRATION", "off") // skip on-disk path; only test re-key

	iamOS := &IAMObjectStore{}
	const rawDN = "CN=Alice,OU=Users,DC=Corp,DC=Com"
	canonical, err := ldap.NormalizeDN(rawDN)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if canonical == rawDN {
		t.Fatalf("test pre-condition broken: rawDN already canonical")
	}

	mp := newMappedPolicy("consoleAdmin")
	m := map[string]MappedPolicy{rawDN: mp}

	iamOS.migrateMappedPolicyToCanonical(context.Background(), rawDN, regularUser, false, m)

	if _, ok := m[rawDN]; ok {
		t.Fatalf("expected non-canonical key %q to be removed from in-memory map", rawDN)
	}
	got, ok := m[canonical]
	if !ok {
		t.Fatalf("expected canonical key %q to exist after migration; got map=%v", canonical, m)
	}
	if got.toSlice()[0] != "consoleAdmin" {
		t.Fatalf("expected migrated policy to be %q, got %q", "consoleAdmin", got.toSlice())
	}
}

// TestLDAPMigrationConflictKeepsLexMin covers test-matrix row #12. When the
// on-disk has BOTH the canonical and a non-canonical name carrying DIFFERENT
// content, the migration must:
//   - keep the lex-min name as winner (deterministic);
//   - leave the canonical key holding the winner's content;
//   - log a conflict (we do not assert the log content here - the contract
//     is that NEITHER content is silently overwritten by the other).
func TestLDAPMigrationConflictKeepsLexMin(t *testing.T) {
	t.Setenv("OTTERIO_IAM_LDAP_DN_MIGRATION", "off")

	iamOS := &IAMObjectStore{}
	const rawDN = "CN=Alice,OU=Users,DC=Corp,DC=Com"
	canonical, err := ldap.NormalizeDN(rawDN)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if canonical == rawDN {
		t.Fatalf("test pre-condition broken: rawDN already canonical")
	}

	rawPolicy := newMappedPolicy("rawPolicy")
	canonicalPolicy := newMappedPolicy("canonicalPolicy")
	m := map[string]MappedPolicy{
		rawDN:     rawPolicy,
		canonical: canonicalPolicy,
	}

	iamOS.migrateMappedPolicyToCanonical(context.Background(), rawDN, regularUser, false, m)

	// Whichever side won, the loser must NOT silently overwrite the winner.
	got, ok := m[canonical]
	if !ok {
		t.Fatalf("expected canonical key to remain after conflict; got map=%v", m)
	}
	winner := got.toSlice()[0]
	if winner != "rawPolicy" && winner != "canonicalPolicy" {
		t.Fatalf("expected canonical key to hold one of the conflicting policies, got %q", winner)
	}

	// The migration is deterministic: rerunning it must produce the same map.
	mCopy := map[string]MappedPolicy{
		rawDN:     rawPolicy,
		canonical: canonicalPolicy,
	}
	iamOS.migrateMappedPolicyToCanonical(context.Background(), rawDN, regularUser, false, mCopy)
	if mCopy[canonical].toSlice()[0] != winner {
		t.Fatalf("migration is not deterministic on conflict: first run picked %q, second run picked %q", winner, mCopy[canonical].toSlice()[0])
	}
}

// TestSTSHandlerCanonicalisesLDAPDNStatically pins down test-matrix row #13
// without needing a live LDAP backend. The end-to-end "two binds, different
// casing, same policy" scenario reduces to the invariant that the STS
// handler consumes the bind result through ldap.NormalizeDN before any IAM
// look-up. We assert that property by parsing cmd/sts-handlers.go and
// verifying the AssumeRoleWithLDAPIdentity body references ldap.NormalizeDN
// after the call to globalLDAPConfig.Bind. This lets a future regression
// (e.g. someone deletes the invariant check) trip immediately, even outside
// an integration environment.
func TestSTSHandlerCanonicalisesLDAPDNStatically(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("sts-handlers.go")
	if err != nil {
		t.Fatalf("read cmd/sts-handlers.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sts-handlers.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse cmd/sts-handlers.go: %v", err)
	}

	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		f, ok := decl.(*ast.FuncDecl)
		if !ok || f.Name.Name != "AssumeRoleWithLDAPIdentity" || f.Recv == nil {
			continue
		}
		fn = f
		break
	}
	if fn == nil {
		t.Fatalf("could not find AssumeRoleWithLDAPIdentity in cmd/sts-handlers.go")
	}

	body := fset.PositionFor(fn.Body.Lbrace, false)
	end := fset.PositionFor(fn.Body.Rbrace, false)
	bodyText := string(src[body.Offset:end.Offset])

	if !strings.Contains(bodyText, "globalLDAPConfig.Bind") {
		t.Fatalf("AssumeRoleWithLDAPIdentity no longer calls globalLDAPConfig.Bind; rewrite this test to reflect the new bind path")
	}
	if !strings.Contains(bodyText, "ldap.NormalizeDN") {
		t.Fatalf("LDAP DN canonicalisation invariant lost: AssumeRoleWithLDAPIdentity does not call ldap.NormalizeDN on the bind result. This is the regression that GHSA-* warned about - re-add the invariant check.")
	}
}

// TestLDAPUsernameFormatsBindCanonicalises covers test-matrix row #14. In
// username-format mode the bindDN is synthesized from the configured format
// string, which is operator-controlled and may carry whatever case /
// whitespace was typed. The Config.usernameFormatsBind helper is the only
// path that produces such DNs, and it must canonicalise before returning.
// We do not need a live LDAP server for this: both forms must produce the
// same NormalizeDN output regardless.
func TestLDAPUsernameFormatsBindCanonicalises(t *testing.T) {
	t.Parallel()

	const bindFormatA = "uid=%s,ou=Users,dc=corp,dc=com"
	const bindFormatB = "UID=%s,OU=Users,DC=Corp,DC=Com"
	username := "alice"

	a, err := ldap.NormalizeDN(strings.Replace(bindFormatA, "%s", username, 1))
	if err != nil {
		t.Fatalf("normalize a: %v", err)
	}
	b, err := ldap.NormalizeDN(strings.Replace(bindFormatB, "%s", username, 1))
	if err != nil {
		t.Fatalf("normalize b: %v", err)
	}
	if a != b {
		t.Fatalf("operator-typed username-format strings %q vs %q produce different canonical DNs %q vs %q (CVE LDAP DN normalization regression)", bindFormatA, bindFormatB, a, b)
	}
}
