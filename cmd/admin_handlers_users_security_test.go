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
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/soulteary/otterio/pkg/madmin"
)

// TestAddUserRejectsPolicyNameField pins down the handler-layer defense for
// CVE-2021-43858 / GHSA-j6jc-jqqc-p6cx. Sending an admin AddUser request whose
// encrypted madmin.UserInfo body carries a non-empty PolicyName must be
// rejected with HTTP 400 (XOtterioAdminUserInfoPolicyNameNotAllowed). The
// upstream regression scenario was a caller with admin:CreateUser - but
// without admin:AttachPolicy - smuggling a policy through the AddUser request,
// bypassing the dedicated AttachPolicy authorisation gate. Closing the door
// at the handler boundary prevents that primitive even if the IAM-layer
// silent-strip in cmd/iam.go ever regresses.
func TestAddUserRejectsPolicyNameField(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatalf("failed to initialize admin testbed: %v", err)
	}
	defer bed.TearDown()

	globalOtterioAddr = "127.0.0.1:9000"

	uinfo := madmin.UserInfo{
		SecretKey:  "secret-not-used-here-but-required-format-1234",
		PolicyName: "consoleAdmin",
		Status:     madmin.AccountEnabled,
	}
	body, err := json.Marshal(uinfo)
	if err != nil {
		t.Fatalf("marshal UserInfo: %v", err)
	}

	encrypted, err := madmin.EncryptData(globalActiveCred.SecretKey, body)
	if err != nil {
		t.Fatalf("encrypt UserInfo: %v", err)
	}

	queryVal := url.Values{}
	queryVal.Set("accessKey", "smuggled-user")

	req, err := buildAdminRequest(queryVal, "PUT", "/add-user",
		int64(len(encrypted)), bytes.NewReader(encrypted))
	if err != nil {
		t.Fatalf("buildAdminRequest: %v", err)
	}

	rec := httptest.NewRecorder()
	bed.router.ServeHTTP(rec, req)

	if rec.Code != 400 {
		respBody, _ := io.ReadAll(rec.Body)
		t.Fatalf("expected 400 for AddUser carrying PolicyName, got %d body=%s", rec.Code, string(respBody))
	}

	respBody, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(respBody), "PolicyName") {
		t.Fatalf("expected response body to mention PolicyName, got %s", string(respBody))
	}
}

// TestAddUserAcceptsRequestWithoutPolicyName is the negative control for the
// reject test above. A well-formed AddUser request whose madmin.UserInfo body
// does NOT carry PolicyName must reach the IAM layer and be processed
// normally; if this test ever starts failing it means the new guard regressed
// from "rejects PolicyName" to "rejects everything" and is masking real
// AddUser failures.
func TestAddUserAcceptsRequestWithoutPolicyName(t *testing.T) {
	skipIfWindowsAdminErasureTestBed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bed, err := prepareAdminErasureTestBed(ctx)
	if err != nil {
		t.Fatalf("failed to initialize admin testbed: %v", err)
	}
	defer bed.TearDown()

	globalOtterioAddr = "127.0.0.1:9000"

	uinfo := madmin.UserInfo{
		SecretKey: "another-very-long-secret-value-1234567890",
		Status:    madmin.AccountEnabled,
	}
	body, err := json.Marshal(uinfo)
	if err != nil {
		t.Fatalf("marshal UserInfo: %v", err)
	}

	encrypted, err := madmin.EncryptData(globalActiveCred.SecretKey, body)
	if err != nil {
		t.Fatalf("encrypt UserInfo: %v", err)
	}

	queryVal := url.Values{}
	queryVal.Set("accessKey", "compatible-user")

	req, err := buildAdminRequest(queryVal, "PUT", "/add-user",
		int64(len(encrypted)), bytes.NewReader(encrypted))
	if err != nil {
		t.Fatalf("buildAdminRequest: %v", err)
	}

	rec := httptest.NewRecorder()
	bed.router.ServeHTTP(rec, req)

	// We do not assert 200 here because the legitimate AddUser path involves
	// notification fan-out to peers and other subsystems that are not fully
	// stood up in this single-node testbed. What we MUST NOT see is the new
	// 400 PolicyName rejection: that would indicate the guard fires on every
	// request, not just the smuggling case.
	respBody, _ := io.ReadAll(rec.Body)
	if rec.Code == 400 && strings.Contains(string(respBody), "PolicyName") {
		t.Fatalf("AddUser with empty PolicyName was rejected as if it carried PolicyName: code=%d body=%s", rec.Code, string(respBody))
	}
}

// TestAddUserPolicyNameIgnoredAtIAMLayer pins down the second line of defense
// statically: even if the handler-layer reject above is ever removed, the IAM
// layer's CreateUser must continue to ignore madmin.UserInfo.PolicyName
// silently (matching upstream MinIO PR #13976). We assert this by parsing
// cmd/iam.go and checking that the CreateUser method body does not reference
// the PolicyName field at all - if a future refactor accidentally restores
// the upstream pre-#13976 behavior ("policyDBSet(name, uinfo.PolicyName)"),
// this test fires immediately, even outside a fully-initialized testbed.
func TestAddUserPolicyNameIgnoredAtIAMLayer(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("iam.go")
	if err != nil {
		t.Fatalf("read cmd/iam.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "iam.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse cmd/iam.go: %v", err)
	}

	var checked bool
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "CreateUser" || fn.Recv == nil {
			continue
		}
		checked = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel != nil && sel.Sel.Name == "PolicyName" {
				pos := fset.Position(sel.Pos())
				t.Fatalf("CVE-2021-43858 second-line regression: cmd/iam.go:%d references uinfo.PolicyName inside CreateUser; the IAM layer must keep silently dropping that field (upstream PR #13976)", pos.Line)
			}
			return true
		})
	}
	if !checked {
		t.Fatalf("could not find IAMSys.CreateUser declaration in cmd/iam.go - did the function rename? update this test")
	}
}
