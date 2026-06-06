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
	"strings"
	"testing"

	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
)

// parseTestPolicy is a tiny convenience helper for tests.
func parseTestPolicy(t *testing.T, doc string) *iampolicy.Policy {
	t.Helper()
	p, err := iampolicy.ParseConfig(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("invalid test policy: %v\n%s", err, doc)
	}
	return p
}

// TestValidateSubPolicyEscalation is the regression test for the
// MinIO RELEASE.2025-10-15 service-account sub-policy privilege-escalation
// fix. A caller that only holds s3:GetObject on bucket1/* must not be able
// to mint a service account whose embedded session policy grants s3:* on *,
// nor s3:GetObject on a different bucket, nor any admin action it does not
// itself hold.
func TestValidateSubPolicyEscalation(t *testing.T) {
	// callerCan is a stand-in for IAMSys.IsAllowed: it only allows
	// s3:GetObject on bucket1 (and any object beneath it).
	callerCan := func(args iampolicy.Args) bool {
		if string(args.Action) != "s3:GetObject" {
			return false
		}
		if args.BucketName != "bucket1" {
			return false
		}
		return true
	}

	cases := []struct {
		name      string
		policy    string
		shouldErr bool
	}{
		{
			name: "subset-allowed",
			policy: `{
				"Version":"2012-10-17",
				"Statement":[{
					"Sid":"ok",
					"Effect":"Allow",
					"Action":["s3:GetObject"],
					"Resource":["arn:aws:s3:::bucket1/*"]
				}]
			}`,
			shouldErr: false,
		},
		{
			name: "wildcard-action-escalation",
			policy: `{
				"Version":"2012-10-17",
				"Statement":[{
					"Sid":"esc",
					"Effect":"Allow",
					"Action":["s3:*"],
					"Resource":["arn:aws:s3:::bucket1/*"]
				}]
			}`,
			shouldErr: true,
		},
		{
			name: "different-bucket-escalation",
			policy: `{
				"Version":"2012-10-17",
				"Statement":[{
					"Sid":"esc",
					"Effect":"Allow",
					"Action":["s3:GetObject"],
					"Resource":["arn:aws:s3:::bucket2/*"]
				}]
			}`,
			shouldErr: true,
		},
		{
			name: "deny-only-is-harmless",
			policy: `{
				"Version":"2012-10-17",
				"Statement":[{
					"Sid":"deny",
					"Effect":"Deny",
					"Action":["s3:DeleteObject"],
					"Resource":["arn:aws:s3:::bucket99/*"]
				}]
			}`,
			shouldErr: false,
		},
		{
			name: "admin-action-escalation",
			policy: `{
				"Version":"2012-10-17",
				"Statement":[{
					"Effect":"Allow",
					"Action":["admin:CreateUser"]
				}]
			}`,
			shouldErr: true,
		},
		{
			name:      "nil-policy-is-noop",
			policy:    "",
			shouldErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sub *iampolicy.Policy
			if tc.policy != "" {
				sub = parseTestPolicy(t, tc.policy)
			}
			err := validateSubPolicyEscalationWith(iampolicy.Args{}, sub, callerCan)
			if tc.shouldErr && err == nil {
				t.Fatalf("expected escalation to be rejected, got nil error")
			}
			if !tc.shouldErr && err != nil {
				t.Fatalf("expected sub-policy to be accepted, got %v", err)
			}
		})
	}
}
