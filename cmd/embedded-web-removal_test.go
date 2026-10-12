/*
 * OtterIO Cloud Storage, (C) 2026 soulteary, https://github.com/soulteary/otterio
 * Licensed under the Apache License, Version 2.0.
 */

package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	xjwt "github.com/soulteary/otterio/cmd/jwt"
	"github.com/soulteary/otterio/pkg/auth"
)

func TestEmbeddedWebRemovalBrowserSetting(t *testing.T) {
	for _, value := range []string{"off", "false", "0"} {
		if err := validateEmbeddedBrowserSetting(value); err != nil {
			t.Errorf("legacy disable setting %q: %v", value, err)
		}
	}
	for _, value := range []string{"on", "true", "1"} {
		err := validateEmbeddedBrowserSetting(value)
		if err == nil || !strings.Contains(err.Error(), "oc-console") {
			t.Errorf("enable setting %q must explain the replacement: %v", value, err)
		}
	}
	if err := validateEmbeddedBrowserSetting("invalid"); err == nil {
		t.Fatal("invalid legacy setting must fail")
	}
}

func TestEmbeddedWebRemovalReservedRPCNamespaces(t *testing.T) {
	for _, path := range []string{
		storageRESTPrefix + "/mnt/data" + storageRESTVersionPrefix + storageRESTMethodHealth,
		peerRESTPrefix + peerRESTVersionPrefix + peerRESTMethodHealth,
		lockRESTPrefix + lockRESTVersionPrefix + lockRESTMethodHealth,
		bootstrapRESTPrefix + bootstrapRESTVersionPrefix + bootstrapRESTMethodHealth,
	} {
		if !guessIsRPCReq(httptest.NewRequest(http.MethodPost, path, nil)) {
			t.Errorf("retained node namespace rejected: %s", path)
		}
	}
	for _, path := range []string{"/otterio/webrpc", "/otterio/zip", "/otterio/lock-evil/v6/health", "/otterio/storage-evil/file"} {
		if guessIsRPCReq(httptest.NewRequest(http.MethodPost, path, nil)) {
			t.Errorf("non-node path bypasses reserved-bucket protection: %s", path)
		}
	}
}

// Use the production server/gateway routers so stripping Web routes cannot
// accidentally remove management, STS, metrics or the separate listener.
func TestEmbeddedWebRemovalProtocolRoutes(t *testing.T) {
	savedObject := newObjectLayerFn()
	savedIAM, savedCred, savedNotification := globalIAMSys, globalActiveCred, globalNotificationSys
	savedDNS, savedFederation := globalDNSConfig, globalBucketFederation
	savedDist, savedErasure, savedGateway := globalIsDistErasure, globalIsErasure, globalIsGateway
	savedConsole := globalOtterioConsoleAddr
	t.Cleanup(func() {
		setObjectLayer(savedObject)
		globalIAMSys, globalActiveCred, globalNotificationSys = savedIAM, savedCred, savedNotification
		globalDNSConfig, globalBucketFederation = savedDNS, savedFederation
		globalIsDistErasure, globalIsErasure, globalIsGateway = savedDist, savedErasure, savedGateway
		globalOtterioConsoleAddr = savedConsole
	})
	obj, fsDir, err := prepareFS(t)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(fsDir)
	setObjectLayer(obj)
	globalActiveCred = auth.Credentials{AccessKey: "protocol-root", SecretKey: "protocol-root-secret"}
	globalNotificationSys = &NotificationSys{}
	globalDNSConfig, globalBucketFederation = nil, false
	globalIsDistErasure, globalIsErasure = false, false
	globalIAMSys = NewIAMSys()
	globalIAMSys.store = newIAMObjectStore(obj)
	close(globalIAMSys.configLoaded)
	t.Setenv(EnvPrometheusAuthType, "jwt")

	for _, gateway := range []bool{false, true} {
		for _, split := range []bool{false, true} {
			name := "server"
			if gateway {
				name = "gateway"
			}
			if split {
				name += "-split"
			}
			t.Run(name, func(t *testing.T) {
				globalIsGateway = gateway
				globalOtterioConsoleAddr = ""
				if split {
					globalOtterioConsoleAddr = "127.0.0.1:9001"
				}
				var s3, management http.Handler
				if gateway {
					a, b, err := configureGatewayHandlers(true, true, true)
					if err != nil {
						t.Fatal(err)
					}
					s3 = fiberHTTPTestHandler(a)
					if b != nil {
						management = fiberHTTPTestHandler(b)
					}
				} else {
					a, b, err := configureServerHandlers(nil)
					if err != nil {
						t.Fatal(err)
					}
					s3 = fiberHTTPTestHandler(a)
					if b != nil {
						management = fiberHTTPTestHandler(b)
					}
				}
				if split && management == nil {
					t.Fatal("management listener removed")
				}
				if !split {
					management = s3
				}
				for _, h := range []http.Handler{s3, management} {
					for _, route := range []struct{ method, path string }{
						{http.MethodGet, "/otterio/"},
						{http.MethodGet, "/otterio/index_bundle.js"},
						{http.MethodGet, "/otterio/login"},
						{http.MethodPost, "/otterio/webrpc"},
						{http.MethodPut, "/otterio/upload/bucket/object"},
						{http.MethodGet, "/otterio/download/bucket/object?token=legacy"},
						{http.MethodPost, "/otterio/zip?token=legacy"},
					} {
						req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"web.Login"}`))
						req.Header.Set("User-Agent", "Mozilla/5.0")
						rec := httptest.NewRecorder()
						h.ServeHTTP(rec, req)
						if rec.Code < 400 || rec.Header().Get("Location") != "" || strings.Contains(strings.ToLower(rec.Body.String()), "<html") {
							t.Errorf("legacy %s %s available: code=%d body=%q", route.method, route.path, rec.Code, rec.Body.String())
						}
					}
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/otterio/health/live", nil))
					if rec.Code != http.StatusOK {
						t.Errorf("health removed: %d %s", rec.Code, rec.Body.String())
					}
				}
				req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
				req.Header.Set("User-Agent", "Mozilla/5.0")
				req.Header.Set("X-Amz-Content-Sha256", emptySHA256)
				if err := signRequestV4(req, globalActiveCred.AccessKey, globalActiveCred.SecretKey); err != nil {
					t.Fatal(err)
				}
				rec := httptest.NewRecorder()
				s3.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ListAllMyBucketsResult") {
					t.Errorf("S3 list removed: %d %s", rec.Code, rec.Body.String())
				}
				rec = httptest.NewRecorder()
				management.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/otterio/admin/v3/info", nil))
				if rec.Code != http.StatusForbidden || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
					t.Errorf("Admin auth route removed: %d %s", rec.Code, rec.Body.String())
				}
				req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Action=AssumeRoleWithWebIdentity&Version=invalid"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				rec = httptest.NewRecorder()
				s3.ServeHTTP(rec, req)
				if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "MissingParameter") {
					t.Errorf("STS route removed: %d %s", rec.Code, rec.Body.String())
				}
				for _, path := range []string{prometheusMetricsPathLegacy, prometheusMetricsV2ClusterPath, prometheusMetricsV2NodePath} {
					rec = httptest.NewRecorder()
					s3.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, otterioReservedBucketPath+path, nil))
					if rec.Code != http.StatusForbidden {
						t.Errorf("metrics auth route %s changed: %d", path, rec.Code)
					}
				}
			})
		}
	}
}

func TestEmbeddedWebRemovalMetricsBearerAuthentication(t *testing.T) {
	// The Web adapter previously owned this parser by name. Pin issuer, expiry
	// and signature verification independently of metric collector state.
	old := globalActiveCred
	globalActiveCred = auth.Credentials{AccessKey: "metrics-root", SecretKey: "metrics-root-secret"}
	t.Cleanup(func() { globalActiveCred = old })
	h := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, tc := range []struct {
		name, issuer, secret string
		expiry               time.Time
		want                 int
	}{
		{"valid", "prometheus", globalActiveCred.SecretKey, UTCNow().Add(time.Hour), http.StatusNoContent},
		{"wrong-issuer", "console", globalActiveCred.SecretKey, UTCNow().Add(time.Hour), http.StatusForbidden},
		{"missing-issuer", "", globalActiveCred.SecretKey, UTCNow().Add(time.Hour), http.StatusForbidden},
		{"expired", "prometheus", globalActiveCred.SecretKey, UTCNow().Add(-time.Hour), http.StatusForbidden},
		{"forged", "prometheus", "wrong-secret", UTCNow().Add(time.Hour), http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := xjwt.NewMapClaims()
			claims.SetAccessKey(globalActiveCred.AccessKey)
			claims.SetExpiry(tc.expiry)
			if tc.issuer != "" {
				claims.MapClaims["iss"] = tc.issuer
			}
			token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, claims).SignedString([]byte(tc.secret))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/otterio/prometheus/metrics", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				body, _ := io.ReadAll(rec.Result().Body)
				t.Errorf("want %d got %d: %s", tc.want, rec.Code, body)
			}
		})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/otterio/prometheus/metrics", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("anonymous metrics changed: %d", rec.Code)
	}
}
