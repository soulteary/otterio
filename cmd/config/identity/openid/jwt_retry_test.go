// Copyright 2026 soulteary.
// SPDX-License-Identifier: Apache-2.0

package openid

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	xnet "github.com/soulteary/otterio/pkg/net"
)

func TestJWTValidateAlgorithmsAfterJWKSRefresh(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []map[string]string{{
				"kty": "RSA", "kid": "rotated-key",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": "AQAB",
			}},
		}); err != nil {
			t.Errorf("encode JWKS: %v", err)
		}
	}))
	defer server.Close()
	jwksURL, err := xnet.ParseHTTPURL(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		method     jwtgo.SigningMethod
		signingKey interface{}
		allowed    bool
	}{
		{jwtgo.SigningMethodRS256, key, true},
		{jwtgo.SigningMethodRS384, key, true},
		{jwtgo.SigningMethodRS512, key, true},
		// PSS uses the same RSA public key as the allowed RS algorithms. A default
		// parser would accept these valid signatures despite the algorithm policy.
		{jwtgo.SigningMethodPS256, key, false},
		{jwtgo.SigningMethodPS384, key, false},
		{jwtgo.SigningMethodPS512, key, false},
		{jwtgo.SigningMethodHS256, []byte("synthetic-test-secret"), false},
		{jwtgo.SigningMethodHS384, []byte("synthetic-test-secret"), false},
		{jwtgo.SigningMethodHS512, []byte("synthetic-test-secret"), false},
	}
	for _, test := range tests {
		t.Run(test.method.Alg(), func(t *testing.T) {
			cfg := Config{
				publicKeys:  make(map[string]crypto.PublicKey),
				mutex:       &sync.Mutex{},
				closeRespFn: func(body io.ReadCloser) { body.Close() },
			}
			cfg.JWKS.URL = jwksURL
			validator := NewJWT(cfg)
			token := jwtgo.NewWithClaims(test.method, jwtgo.MapClaims{
				"sub": "test-user",
				"exp": time.Now().Add(time.Hour).Unix(),
			})
			token.Header["kid"] = "rotated-key"
			signed, err := token.SignedString(test.signingKey)
			if err != nil {
				t.Fatal(err)
			}
			before := requests.Load()
			claims, err := validator.Validate(signed, "")
			if got := requests.Load() - before; got != 1 {
				t.Fatalf("expected one JWKS refresh, got %d", got)
			}
			if _, ok := validator.publicKeys["rotated-key"].(*rsa.PublicKey); !ok {
				t.Fatal("refresh did not load the RSA public key")
			}
			if test.allowed {
				if err != nil {
					t.Fatalf("allowed algorithm rejected after refresh: %v", err)
				}
				if claims["sub"] != "test-user" {
					t.Fatalf("unexpected claims: %v", claims)
				}
				return
			}
			// Require algorithm rejection, rather than a key-type/signature error:
			// HMAC must be rejected by policy even before attempting verification.
			if err == nil || !strings.Contains(err.Error(), "signing method "+test.method.Alg()+" is invalid") {
				t.Fatalf("expected algorithm rejection after refresh, got claims=%v err=%v", claims, err)
			}
			if claims != nil {
				t.Fatalf("rejected token returned claims: %v", claims)
			}
		})
	}
}
