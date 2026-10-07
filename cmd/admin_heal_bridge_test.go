package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestAdminHealWildcardPrefixPreserved(t *testing.T) {
	for _, tc := range []struct{ encoded, prefix string }{
		{"dir/object", "dir/object"},
		{"dir/a+b", "dir/a+b"},
		{"dir/a%2Bb", "dir/a+b"},
		{"dir/a%252Fb", "dir/a%2Fb"},
		{"dir/a%25b", "dir/a%b"},
	} {
		app := newFiberApp()
		captured := false
		app.Use(func(c fiber.Ctx) error {
			err := c.Next()
			vars := allPathParams(c)
			hi, code := extractHealInitParams(vars, url.Values{mgmtClientToken: {"test"}}, nil)
			if code != ErrNone || hi.bucket != "mybucket" || hi.objPrefix != tc.prefix {
				t.Errorf("heal %s: bucket %q prefix %q code %v, want %q", tc.encoded, hi.bucket, hi.objPrefix, code, tc.prefix)
			}
			captured = true
			return err
		})
		registerAdminHealRoutes(app, "/admin-test", adminAPIHandlers{})
		handler := fiberHTTPTestHandler(app)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/admin-test/heal/mybucket/"+tc.encoded, nil))
		if !captured {
			t.Fatal("heal route did not dispatch")
		}
	}
}
