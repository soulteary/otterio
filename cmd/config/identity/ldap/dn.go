/*
 * OtterIO Cloud Storage, (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

// Package ldap provides DN normalization in addition to the legacy LDAP
// configuration helpers defined alongside this file.
//
// SECURITY (upstream-cve-backlog.md row "LDAP DN normalization"): every DN
// that flows out of this package - whether it came from a Bind, a search,
// or the static config - must travel through NormalizeDN before it is used
// as a key into the IAM policy maps. The IAM map look-ups in cmd/iam.go are
// case-sensitive string equality, while Active Directory and most other
// production LDAP servers treat DNs as case-insensitive and tolerant of
// whitespace / quoting differences. Without this normalization, the same
// LDAP identity can be granted multiple disjoint policy mappings, and an
// attacker who can influence the casing returned by the directory can pick
// whichever mapping suits them.
//
// NormalizeDN deliberately:
//   - returns an error on syntactically invalid DNs (no silent ToLower
//     fallback - we refuse to authenticate ambiguous identities);
//   - lower-cases attribute types (RFC 4514 declares them
//     case-insensitive);
//   - applies a conservative subset of the RFC 4518 string preparation
//     algorithm to attribute values: lower-case, trim, and fold runs of
//     ASCII whitespace into a single space (this matches AD's behavior
//     for the practical cases that drive the upstream CVEs and avoids
//     pulling in the full Unicode case-fold + NFKC pipeline that golang.org/x/text
//     would require);
//   - sorts the attribute=value pairs inside a multi-valued RDN so that
//     `cn=a+sn=b` and `sn=b+cn=a` produce the same canonical form.
//
// Operators upgrading from a pre-normalization OtterIO build should consult
// docs/security/ldap-dn-normalization-migration.md before rolling out.
package ldap

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	ldap "github.com/go-ldap/ldap/v3"
)

// ErrInvalidDN is returned by NormalizeDN when the input cannot be parsed
// per RFC 4514. Callers in security-sensitive paths must treat this as a
// hard authentication failure rather than degrade to a best-effort
// strings.ToLower of the original input.
var ErrInvalidDN = errors.New("ldap: invalid distinguished name")

// NormalizeDN returns a canonical, case-folded form of dn that is safe to
// use as a key into the IAM policy maps. The result is stable across the
// case-/whitespace-/RDN-ordering variations that AD and other directories
// treat as equal. An empty input maps to an empty output without error so
// callers can normalise optional config fields uniformly.
func NormalizeDN(dn string) (string, error) {
	if dn == "" {
		return "", nil
	}

	parsed, err := ldap.ParseDN(dn)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidDN, err)
	}

	rdns := make([]string, 0, len(parsed.RDNs))
	for _, rdn := range parsed.RDNs {
		atvs := make([]string, 0, len(rdn.Attributes))
		for _, atv := range rdn.Attributes {
			t := strings.ToLower(strings.TrimSpace(atv.Type))
			v := normalizeDNValue(atv.Value)
			atvs = append(atvs, t+"="+escapeRDNValue(v))
		}
		// multi-valued RDN (e.g. cn=a+sn=b): make ordering canonical so the
		// same identity always produces the same string regardless of how the
		// directory laid the values out.
		sort.Strings(atvs)
		rdns = append(rdns, strings.Join(atvs, "+"))
	}
	return strings.Join(rdns, ","), nil
}

// MustNormalizeDN is a convenience wrapper that returns the original input
// when parsing fails. It is intentionally NOT exported to security-sensitive
// callers (use NormalizeDN there); the only acceptable use is logging /
// audit / diagnostic helpers where a best-effort canonical form is more
// useful than an error.
//
//nolint:unused // kept for log/audit call sites; safe by construction.
func MustNormalizeDN(dn string) string {
	out, err := NormalizeDN(dn)
	if err != nil {
		return dn
	}
	return out
}

// normalizeDNValue applies the case-fold + whitespace-fold subset of RFC 4518
// that matters for AD compatibility. Characters that needed quoting in the
// original DN have already been unescaped by ldap.ParseDN, so we operate on
// the literal value here.
func normalizeDNValue(v string) string {
	v = strings.ToLower(v)
	v = strings.TrimSpace(v)
	// collapse runs of ASCII whitespace (space, tab) into a single space;
	// AD treats `cn=Bob  Smith` and `cn=Bob Smith` as the same identity.
	if !strings.ContainsAny(v, " \t") {
		return v
	}
	fields := strings.Fields(v)
	return strings.Join(fields, " ")
}

// escapeRDNValue re-applies the minimal RFC 4514 escaping needed to make the
// canonical string round-trip through ldap.ParseDN. We escape the structural
// separators (`,`, `+`, `;`) and the leading/trailing/embedded NUL byte; the
// rest is left as-is because the normaliser already lower-cased and folded
// whitespace, so non-ASCII bytes are preserved verbatim.
func escapeRDNValue(v string) string {
	if v == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(v))
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch c {
		case ',', '+', '"', '\\', '<', '>', ';', '=':
			b.WriteByte('\\')
			b.WriteByte(c)
		case 0x00:
			b.WriteString("\\00")
		default:
			b.WriteByte(c)
		}
	}
	out := b.String()
	// leading '#' and ' ' as well as trailing ' ' must be escaped per RFC 4514.
	if out[0] == '#' || out[0] == ' ' {
		out = `\` + out
	}
	if out[len(out)-1] == ' ' {
		out = out[:len(out)-1] + `\ `
	}
	return out
}

// EqualDN reports whether two DNs refer to the same LDAP identity, after
// NormalizeDN has been applied to both sides. A parsing failure on either
// input causes EqualDN to return false (security default: refuse to assert
// equality if either side is ambiguous).
func EqualDN(a, b string) bool {
	na, err := NormalizeDN(a)
	if err != nil {
		return false
	}
	nb, err := NormalizeDN(b)
	if err != nil {
		return false
	}
	return na == nb
}

// NormalizeDNSlice normalises a slice of DNs in place. The first error
// encountered is returned and the slice is left in an undefined state -
// callers must treat any error as a hard failure and discard the slice.
func NormalizeDNSlice(dns []string) error {
	for i, dn := range dns {
		out, err := NormalizeDN(dn)
		if err != nil {
			return fmt.Errorf("index %d: %w", i, err)
		}
		dns[i] = out
	}
	return nil
}
