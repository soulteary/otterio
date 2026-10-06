package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestAdminQueryBridgeDeclaredKeys(t *testing.T) {
	for _, stream := range []bool{false, true} {
		app := fiber.New()
		handler := func(w http.ResponseWriter, r *http.Request) {
			if urlVar(r, "policyName") != "readonly" || urlVar(r, "userOrGroup") != "user-one" || urlVar(r, "isGroup") != "false" {
				t.Error("declared query captures missing")
			}
			if urlVar(r, "unexpected") != "" {
				t.Error("undeclared query was bridged")
			}
			w.WriteHeader(http.StatusOK)
		}
		queries := map[string]string{"policyName": ".*", "userOrGroup": ".*", "isGroup": "true|false"}
		rule := adminRule(http.MethodPut, handler, true, queries)
		if stream {
			rule = adminStreamRule(http.MethodPut, handler, queries)
		}
		registerAdminRoute(app, "/admin-test", []routeRule{rule})
		response, err := app.Test(httptest.NewRequest(http.MethodPut, "/admin-test?policyName=readonly&userOrGroup=user-one&isGroup=false&unexpected=ignored", nil))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatal(response.StatusCode)
		}
	}
}

func TestAdminQueryBridgePreservesPathAndValidation(t *testing.T) {
	app := fiber.New()
	calls := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls++
		if urlVar(r, "bucket") != "path-bucket" {
			t.Error("query overwrote path capture")
		}
		w.WriteHeader(http.StatusOK)
	}
	registerAdminRoute(app, "/admin-test/:bucket", []routeRule{adminRule(http.MethodPut, handler, true, map[string]string{"bucket": ".*", "isGroup": "true|false"})})
	for _, value := range []string{"false", "invalid"} {
		response, err := app.Test(httptest.NewRequest(http.MethodPut, "/admin-test/path-bucket?bucket=query-bucket&isGroup="+value, nil))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if value == "false" && response.StatusCode != http.StatusOK {
			t.Fatal(response.StatusCode)
		}
		if value == "invalid" && response.StatusCode == http.StatusOK {
			t.Fatal("invalid query reached handler")
		}
	}
	if calls != 1 {
		t.Fatal("query validation bypassed")
	}
}
