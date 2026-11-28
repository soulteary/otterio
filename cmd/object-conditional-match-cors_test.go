// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	xhttp "github.com/soulteary/otterio/cmd/http"
)

func TestConditionalMatchCORS(t *testing.T) {
	const origin = "https://conditional-client.example"
	globalAPIConfig.mu.Lock()
	previousOrigins := globalAPIConfig.corsAllowOrigins
	globalAPIConfig.corsAllowOrigins = []string{origin}
	globalAPIConfig.mu.Unlock()
	objectLayer, cacheLayer := newObjectLayerFn(), newCachedObjectLayerFn()
	gateway, distributed := globalIsGateway, globalIsDistErasure
	federation, dnsConfig := globalBucketFederation, globalDNSConfig
	t.Cleanup(func() {
		globalAPIConfig.mu.Lock()
		globalAPIConfig.corsAllowOrigins = previousOrigins
		globalAPIConfig.mu.Unlock()
		setObjectLayer(objectLayer)
		setCacheObjectLayer(cacheLayer)
		globalIsGateway, globalIsDistErasure = gateway, distributed
		globalBucketFederation, globalDNSConfig = federation, dnsConfig
	})
	setObjectLayer(&FSObjects{})
	setCacheObjectLayer(nil)
	globalIsGateway, globalIsDistErasure = false, false
	globalBucketFederation, globalDNSConfig = false, nil

	app := newFiberApp()
	app.Use(corsMiddlewareFiber())
	app.Get("/bucket/object", func(c fiber.Ctx) error {
		writeSuccessResponseHeadersOnlyFiber(c)
		return nil
	})
	assertListContains := func(header http.Header, list, name string) {
		t.Helper()
		for _, value := range strings.Split(header.Get(list), ",") {
			if strings.EqualFold(strings.TrimSpace(value), name) {
				return
			}
		}
		t.Errorf("%s must explicitly include %s for credentialed Fetch: %q", list, name, header.Get(list))
	}
	assertCredentials := func(header http.Header) {
		t.Helper()
		if header.Get("Access-Control-Allow-Origin") != origin || header.Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("credentialed CORS response missing exact origin or credentials: %v", header)
		}
	}
	for _, method := range []string{http.MethodPut, http.MethodPost, http.MethodDelete} {
		for _, condition := range []string{xhttp.IfMatch, xhttp.IfNoneMatch} {
			t.Run(method+"/"+condition, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodOptions, "/bucket/object", nil)
				request.Header.Set("Origin", origin)
				request.Header.Set("Access-Control-Request-Method", method)
				request.Header.Set("Access-Control-Request-Headers", strings.ToLower(condition))
				response, err := app.Test(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusNoContent {
					t.Fatalf("preflight status=%d", response.StatusCode)
				}
				assertCredentials(response.Header)
				assertListContains(response.Header, "Access-Control-Allow-Methods", method)
				assertListContains(response.Header, "Access-Control-Allow-Headers", condition)
			})
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/bucket/object", nil)
	request.Header.Set("Origin", origin)
	request.AddCookie(&http.Cookie{Name: "session", Value: "test"})
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("capability response status=%d", response.StatusCode)
	}
	assertCredentials(response.Header)
	for _, capability := range []string{conditionalWritesHeader, conditionalMatchWritesHeader, conditionalCopyWritesHeader, conditionalDeleteHeader} {
		if response.Header.Get(capability) != "v1" {
			t.Errorf("missing %s capability: %v", capability, response.Header)
		}
		assertListContains(response.Header, "Access-Control-Expose-Headers", capability)
	}
}
