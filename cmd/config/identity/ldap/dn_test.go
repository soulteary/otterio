/*
 * OtterIO Cloud Storage, (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package ldap

import (
	"errors"
	"strings"
	"testing"
)

// TestNormalizeDNCaseFold pins down the primary attack surface from the
// upstream LDAP DN-normalization CVEs: an Active Directory backend treats
// `cn=alice,...` and `CN=Alice,...` as the same identity, so OtterIO must
// produce the same canonical key for both. Without this guarantee, an
// attacker who can influence the casing returned by the directory can pick
// whichever IAM policy mapping the administrator did NOT intend.
func TestNormalizeDNCaseFold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a, b string
	}{
		{"matrix-1 simple case fold", "cn=alice,ou=users,dc=corp,dc=com", "CN=Alice,OU=Users,DC=Corp,DC=Com"},
		{"matrix-2 RDN type case fold", "cn=alice,dc=corp,dc=com", "Cn=alice,Dc=corp,DC=com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			na, err := NormalizeDN(tc.a)
			if err != nil {
				t.Fatalf("normalize a: %v", err)
			}
			nb, err := NormalizeDN(tc.b)
			if err != nil {
				t.Fatalf("normalize b: %v", err)
			}
			if na != nb {
				t.Fatalf("expected identical canonical form for %q vs %q, got %q vs %q", tc.a, tc.b, na, nb)
			}
			if !EqualDN(tc.a, tc.b) {
				t.Fatalf("EqualDN should return true for %q vs %q", tc.a, tc.b)
			}
		})
	}
}

// TestNormalizeDNWhitespace covers test-matrix rows #3-#5: collapsing runs of
// internal whitespace, trimming leading / trailing whitespace, and treating
// whitespace around comma separators as cosmetic. AD ignores all of these
// differences in DN comparisons.
func TestNormalizeDNWhitespace(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a, b string
	}{
		{"matrix-3 internal whitespace", "cn=Bob  Smith,dc=corp,dc=com", "cn=Bob Smith,dc=corp,dc=com"},
		{"matrix-3 internal tabs", "cn=Bob\t\tSmith,dc=corp,dc=com", "cn=Bob Smith,dc=corp,dc=com"},
		{"matrix-4 leading and trailing whitespace", "  cn=alice,dc=corp,dc=com  ", "cn=alice,dc=corp,dc=com"},
		// matrix-5: ldap.ParseDN is the canonical source of truth for whitespace
		// around separators, so we only assert that the two parse-equivalent
		// inputs collapse to the same canonical form, not the exact bytes.
		{"matrix-5 whitespace around comma", "cn=alice, ou=users,dc=corp,dc=com", "cn=alice,ou=users,dc=corp,dc=com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			na, err := NormalizeDN(tc.a)
			if err != nil {
				t.Fatalf("normalize a: %v", err)
			}
			nb, err := NormalizeDN(tc.b)
			if err != nil {
				t.Fatalf("normalize b: %v", err)
			}
			if na != nb {
				t.Fatalf("expected identical canonical form for %q vs %q, got %q vs %q", tc.a, tc.b, na, nb)
			}
		})
	}
}

// TestNormalizeDNMultiValuedRDN covers test-matrix row #6. A multi-valued RDN
// carries multiple attribute=value pairs separated by `+`; per RFC 4514 the
// internal ordering is meaningless. NormalizeDN sorts the attributes so two
// equivalent multi-valued RDNs collapse to the same canonical form.
func TestNormalizeDNMultiValuedRDN(t *testing.T) {
	t.Parallel()

	a := "cn=a+sn=b,dc=corp,dc=com"
	b := "sn=b+cn=a,dc=corp,dc=com"

	na, err := NormalizeDN(a)
	if err != nil {
		t.Fatalf("normalize a: %v", err)
	}
	nb, err := NormalizeDN(b)
	if err != nil {
		t.Fatalf("normalize b: %v", err)
	}
	if na != nb {
		t.Fatalf("multi-valued RDN ordering must not change canonical form: %q -> %q vs %q -> %q", a, na, b, nb)
	}
}

// TestNormalizeDNEscapeRoundTrip covers test-matrix row #7. RFC 4514 supports
// two escape forms for special characters - backslash + literal byte, and
// backslash + two-digit hex. Both must collapse to the same canonical DN.
func TestNormalizeDNEscapeRoundTrip(t *testing.T) {
	t.Parallel()

	a := `cn=a\,b,dc=corp,dc=com`  // backslash-comma form
	b := `cn=a\2Cb,dc=corp,dc=com` // hex-pair form for ','
	c := `cn=a\2cb,dc=corp,dc=com` // hex-pair lowercase

	na, err := NormalizeDN(a)
	if err != nil {
		t.Fatalf("normalize a: %v", err)
	}
	nb, err := NormalizeDN(b)
	if err != nil {
		t.Fatalf("normalize b: %v", err)
	}
	nc, err := NormalizeDN(c)
	if err != nil {
		t.Fatalf("normalize c: %v", err)
	}
	if na != nb || nb != nc {
		t.Fatalf("escape forms must collapse to the same canonical DN, got %q / %q / %q", na, nb, nc)
	}
	// The canonical form must round-trip through the parser (i.e. it stays a
	// syntactically valid DN), so the special character is still escaped.
	if !strings.Contains(na, `\,`) {
		t.Fatalf("canonical DN %q must keep ',' escaped to remain parseable", na)
	}
}

// TestNormalizeDNRejectInvalid covers test-matrix row #8: the normaliser must
// surface a typed error for syntactically invalid inputs rather than silently
// degrade to strings.ToLower. Authentication paths rely on this so they can
// refuse to issue credentials for ambiguous identities.
func TestNormalizeDNRejectInvalid(t *testing.T) {
	t.Parallel()

	cases := []string{
		"not a dn",                 // no `=`
		"cn=alice,,dc=corp,dc=com", // empty RDN
		"=value",                   // empty type
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			out, err := NormalizeDN(in)
			if err == nil {
				t.Fatalf("expected error for invalid DN %q, got %q", in, out)
			}
			if !errors.Is(err, ErrInvalidDN) {
				t.Fatalf("expected ErrInvalidDN, got %T %v", err, err)
			}
			if out != "" {
				t.Fatalf("expected empty canonical form on error, got %q", out)
			}
		})
	}
}

// TestNormalizeDNUnicode covers test-matrix row #9. We do not pretend to
// implement the full RFC 4518 Unicode case-fold pipeline (that would require
// pulling in golang.org/x/text), but we DO promise that a deterministic,
// stable canonical form is produced for non-ASCII inputs - i.e. running
// NormalizeDN twice on the same string is idempotent. This guards against
// future refactors that add a half-finished Unicode normalization step and
// accidentally break idempotency, which would silently re-shard the IAM map.
func TestNormalizeDNUnicode(t *testing.T) {
	t.Parallel()

	cases := []string{
		"cn=İstanbul,dc=corp,dc=com",
		"cn=café,dc=corp,dc=com",
		"cn=北京,dc=corp,dc=com",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			once, err := NormalizeDN(in)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			twice, err := NormalizeDN(once)
			if err != nil {
				t.Fatalf("re-normalize: %v", err)
			}
			if once != twice {
				t.Fatalf("NormalizeDN is not idempotent for %q: %q vs %q", in, once, twice)
			}
		})
	}
}

// TestNormalizeDNEmpty pins the contract that an empty input maps to an
// empty output without error. Several optional config fields (e.g. the
// LookupBindDN when the directory permits anonymous bind) are legitimately
// empty and must not be rejected.
func TestNormalizeDNEmpty(t *testing.T) {
	t.Parallel()

	out, err := NormalizeDN("")
	if err != nil {
		t.Fatalf("empty input must not error: %v", err)
	}
	if out != "" {
		t.Fatalf("empty input must produce empty output, got %q", out)
	}
}

// TestNormalizeDNSlice exercises the convenience wrapper used by the LDAP
// package on group-DN lists.
func TestNormalizeDNSlice(t *testing.T) {
	t.Parallel()

	in := []string{
		"CN=Admins,OU=Groups,DC=Corp,DC=Com",
		"cn=devs,ou=groups,dc=corp,dc=com",
	}
	if err := NormalizeDNSlice(in); err != nil {
		t.Fatalf("NormalizeDNSlice: %v", err)
	}
	for _, dn := range in {
		if dn != strings.ToLower(dn) {
			t.Fatalf("expected normalised slice entry to be lower-cased, got %q", dn)
		}
	}

	bad := []string{"cn=ok,dc=c", "garbage"}
	if err := NormalizeDNSlice(bad); err == nil {
		t.Fatalf("expected NormalizeDNSlice to surface error for invalid entry")
	}
}
