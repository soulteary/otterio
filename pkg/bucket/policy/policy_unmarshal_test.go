/*
 * OtterIO Cloud Storage, (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBucketPolicyUnmarshalDoesNotPanic mirrors the IAM-side panic harness in
// pkg/iam/policy. It pushes an explicit catalog of malformed JSON inputs
// through json.Unmarshal of bucket-scoped Principal / Resource / Action
// containers and asserts that none of them produce a panic. Combined with
// FuzzBucketPolicyUnmarshal below this gives us both deterministic regression
// coverage and a corpus that future fuzzers can grow.
func TestBucketPolicyUnmarshalDoesNotPanic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data string
	}{
		{"emptyObject", `{}`},
		{"principalAsArray", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": ["*"], "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/*"}]}`},
		{"principalAsNumber", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": 1, "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/*"}]}`},
		{"principalUnknownString", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": "alice", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/*"}]}`},
		{"resourceAsObject", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": {}}]}`},
		{"resourceArrayWithNull", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": [null]}]}`},
		{"resourceWildcardOnly", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": "*"}]}`},
		{"actionAsObject", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": "*", "Action": {}, "Resource": "arn:aws:s3:::b/*"}]}`},
		{"actionEmptyArray", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": "*", "Action": [], "Resource": "arn:aws:s3:::b/*"}]}`},
		{"effectMixedCase", `{"Version": "2012-10-17", "Statement": [{"Effect": "ALLOW", "Principal": "*", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/*"}]}`},
		{"effectNumeric", `{"Version": "2012-10-17", "Statement": [{"Effect": 1, "Principal": "*", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/*"}]}`},
		{"sidVeryLong", `{"Version": "2012-10-17", "Statement": [{"Sid": "` + strings.Repeat("S", 65536) + `", "Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/*"}]}`},
		{"conditionAsString", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/*", "Condition": "yes"}]}`},
		{"conditionMissingFunc", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/*", "Condition": {"NoSuchOp": {"k": "v"}}}]}`},
		{"truncated", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow"`},
		{"controlChars", "{\"Version\": \"2012-10-17\", \"Statement\": [{\"Effect\": \"\u0000\", \"Principal\": \"*\", \"Action\": \"s3:GetObject\", \"Resource\": \"arn:aws:s3:::b/*\"}]}"},
		{"unicodeNoncharacter", "{\"Version\": \"2012-10-17\", \"Statement\": [{\"Effect\": \"Allow\", \"Principal\": \"*\", \"Action\": \"s3:Get\uFFFFObject\", \"Resource\": \"arn:aws:s3:::b/*\"}]}"},
		{"deepArrayResources", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": [` + strings.Repeat(`"arn:aws:s3:::b/*",`, 1000) + `"arn:aws:s3:::b/*"]}]}`},
		{"recursivePrincipal", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": {"AWS": ["*"], "Federated": ["*"], "Service": ["lambda"]}, "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/*"}]}`},
		{"missingResource", `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject"}]}`},
		{"versionMissing", `{"Statement": [{"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/*"}]}`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("UnmarshalJSON panicked on %q: %v", tc.name, r)
				}
			}()
			var p Policy
			_ = json.Unmarshal([]byte(tc.data), &p)
		})
	}
}

// TestPrincipalUnmarshalErrorDoesNotEchoInput pins down the error wording for
// the Principal fallback path. Older versions of the upstream MinIO codebase
// echoed the attacker-controlled raw JSON value back into the error message
// (`invalid principal '%v'`), which is useful for log poisoning, response
// fingerprinting, and reflected attacks against tooling that surfaces error
// messages to humans. Starting with this commit the message is a static
// constant. If a future change reintroduces a `%v`-formatted attacker payload
// this test fires.
func TestPrincipalUnmarshalErrorDoesNotEchoInput(t *testing.T) {
	t.Parallel()

	const probe = "<script>alert('pwned')</script>"
	data, err := json.Marshal(probe)
	if err != nil {
		t.Fatalf("setup: marshal probe: %v", err)
	}

	var p Principal
	err = json.Unmarshal(data, &p)
	if err == nil {
		t.Fatalf("expected an error for non-wildcard string principal, got nil")
	}
	if strings.Contains(err.Error(), probe) {
		t.Fatalf("error message echoes raw attacker input verbatim: %q (raw probe %q present)", err.Error(), probe)
	}
	if !strings.Contains(err.Error(), "invalid principal") {
		t.Fatalf("error message lost its prefix; downstream callers may rely on it: got %q", err.Error())
	}
}

// TestPrincipalUnmarshalAcceptsWildcard is the negative control for the
// fallback-rejection test: the literal "*" string MUST still produce a valid
// Principal whose AWS set contains the wildcard.
func TestPrincipalUnmarshalAcceptsWildcard(t *testing.T) {
	t.Parallel()

	var p Principal
	if err := json.Unmarshal([]byte(`"*"`), &p); err != nil {
		t.Fatalf("wildcard principal rejected: %v", err)
	}
	if !p.AWS.Contains("*") {
		t.Fatalf("wildcard principal did not populate AWS set: %#v", p)
	}
}
