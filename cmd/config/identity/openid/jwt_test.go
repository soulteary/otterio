/*
 * MinIO Cloud Storage, (C) 2018-2019 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package openid

import (
	"crypto"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	xnet "github.com/soulteary/otterio/pkg/net"
)

func TestUpdateClaimsExpiry(t *testing.T) {
	testCases := []struct {
		exp             interface{}
		dsecs           string
		expectedFailure bool
	}{
		{"", "", true},
		{"-1", "0", true},
		{"-1", "900", true},
		{"1574812326", "900", false},
		{1574812326, "900", false},
		{int64(1574812326), "900", false},
		{int(1574812326), "900", false},
		{uint(1574812326), "900", false},
		{uint64(1574812326), "900", false},
		{json.Number("1574812326"), "900", false},
		{1574812326.000, "900", false},
		{time.Duration(3) * time.Minute, "900", false},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run("", func(t *testing.T) {
			claims := map[string]interface{}{}
			claims["exp"] = testCase.exp
			err := updateClaimsExpiry(testCase.dsecs, claims)
			if err != nil && !testCase.expectedFailure {
				t.Errorf("Expected success, got failure %s", err)
			}
			if err == nil && testCase.expectedFailure {
				t.Error("Expected failure, got success")
			}
		})
	}
}

// makeAzureTestJWT builds a redacted, non-functional Azure-style JWT at
// runtime. Keeping the token out of source as a literal avoids tripping
// secret-scanning/push-protection on JWT patterns.
func makeAzureTestJWT(t *testing.T) string {
	t.Helper()

	enc := func(v interface{}) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("Failed to marshal JWT segment: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}

	header := map[string]interface{}{
		"typ": "JWT",
		"alg": "RS256",
		"x5t": "huN95IvPfehq34GzBDZ1GXGirnM",
		"kid": "huN95IvPfehq34GzBDZ1GXGirnM",
	}
	const placeholderGUID = "00000000-0000-0000-0000-000000000000"
	payload := map[string]interface{}{
		"aud":             "00000003-0000-0000-c000-000000000000",
		"iss":             "https://sts.windows.net/" + placeholderGUID + "/",
		"iat":             1594657520,
		"nbf":             1594657520,
		"exp":             1594661420,
		"acr":             "1",
		"amr":             []string{"pwd"},
		"app_displayname": "example-app",
		"appid":           placeholderGUID,
		"appidacr":        "1",
		"family_name":     "Doe",
		"given_name":      "Jane",
		"ipaddr":          "203.0.113.1",
		"name":            "Doe, Jane",
		"oid":             placeholderGUID,
		"scp":             "openid profile email",
		"sub":             "redacted-subject-identifier",
		"tid":             placeholderGUID,
		"unique_name":     "jane.doe@example.com",
		"upn":             "jane.doe@example.com",
		"ver":             "1.0",
	}
	sig := base64.RawURLEncoding.EncodeToString([]byte("invalid-signature-for-test-only"))

	return strings.Join([]string{enc(header), enc(payload), sig}, ".")
}

func TestJWTAzureFail(t *testing.T) {
	const jsonkey = `{"keys":[{"kty":"RSA","use":"sig","kid":"SsZsBNhZcF3Q9S4trpQBTByNRRI","x5t":"SsZsBNhZcF3Q9S4trpQBTByNRRI","n":"uHPewhg4WC3eLVPkEFlj7RDtaKYWXCI5G-LPVzsMKOuIu7qQQbeytIA6P6HT9_iIRt8zNQvuw4P9vbNjgUCpI6vfZGsjk3XuCVoB_bAIhvuBcQh9ePH2yEwS5reR-NrG1PsqzobnZZuigKCoDmuOb_UDx1DiVyNCbMBlEG7UzTQwLf5NP6HaRHx027URJeZvPAWY7zjHlSOuKoS_d1yUveaBFIgZqPWLCg44ck4gvik45HsNVWT9zYfT74dvUSSrMSR-SHFT7Hy1XjbVXpHJHNNAXpPoGoWXTuc0BxMsB4cqjfJqoftFGOG4x32vEzakArLPxAKwGvkvu0jToAyvSQ","e":"AQAB","x5c":"MIIDBTCCAe2gAwIBAgIQWHw7h/Ysh6hPcXpnrJ0N8DANBgkqhkiG9w0BAQsFADAtMSswKQYDVQQDEyJhY2NvdW50cy5hY2Nlc3Njb250cm9sLndpbmRvd3MubmV0MB4XDTIwMDQyNzAwMDAwMFoXDTI1MDQyNzAwMDAwMFowLTErMCkGA1UEAxMiYWNjb3VudHMuYWNjZXNzY29udHJvbC53aW5kb3dzLm5ldDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALhz3sIYOFgt3i1T5BBZY+0Q7WimFlwiORviz1c7DCjriLu6kEG3srSAOj+h0/f4iEbfMzUL7sOD/b2zY4FAqSOr32RrI5N17glaAf2wCIb7gXEIfXjx9shMEua3kfjaxtT7Ks6G52WbooCgqA5rjm/1A8dQ4lcjQmzAZRBu1M00MC3+TT+h2kR8dNu1ESXmbzwFmO84x5UjriqEv3dclL3mgRSIGaj1iwoOOHJOIL4pOOR7DVVk/c2H0++Hb1EkqzEkfkhxU+x8tV421V6RyRzTQF6T6BqFl07nNAcTLAeHKo3yaqH7RRjhuMd9rxM2pAKyz8QCsBr5L7tI06AMr0kCAwEAAaMhMB8wHQYDVR0OBBYEFOI7M+DDFMlP7Ac3aomPnWo1QL1SMA0GCSqGSIb3DQEBCwUAA4IBAQBv+8rBiDY8sZDBoUDYwFQM74QjqCmgNQfv5B0Vjwg20HinERjQeH24uAWzyhWN9++FmeY4zcRXDY5UNmB0nJz7UGlprA9s7voQ0Lkyiud0DO072RPBg38LmmrqoBsLb3MB9MZ2CGBaHftUHfpdTvrgmXSP0IJn7mCUq27g+hFk7n/MLbN1k8JswEODIgdMRvGqN+mnrPKkviWmcVAZccsWfcmS1pKwXqICTKzd6WmVdz+cL7ZSd9I2X0pY4oRwauoE2bS95vrXljCYgLArI3XB2QcnglDDBRYu3Z3aIJb26PTIyhkVKT7xaXhXl4OgrbmQon9/O61G2dzpjzzBPqNP","issuer":"https://login.microsoftonline.com/906aefe9-76a7-4f65-b82d-5ec20775d5aa/v2.0"},{"kty":"RSA","use":"sig","kid":"huN95IvPfehq34GzBDZ1GXGirnM","x5t":"huN95IvPfehq34GzBDZ1GXGirnM","n":"6lldKm5Rc_vMKa1RM_TtUv3tmtj52wLRrJqu13yGM3_h0dwru2ZP53y65wDfz6_tLCjoYuRCuVsjoW37-0zXUORJvZ0L90CAX-58lW7NcE4bAzA1pXv7oR9kQw0X8dp0atU4HnHeaTU8LZxcjJO79_H9cxgwa-clKfGxllcos8TsuurM8xi2dx5VqwzqNMB2s62l3MTN7AzctHUiQCiX2iJArGjAhs-mxS1wmyMIyOSipdodhjQWRAcseW-aFVyRTFVi8okl2cT1HJjPXdx0b1WqYSOzeRdrrLUcA0oR2Tzp7xzOYJZSGNnNLQqa9f6h6h52XbX0iAgxKgEDlRpbJw","e":"AQAB","x5c":["MIIDBTCCAe2gAwIBAgIQPCxFbySVSLZOggeWRzBWOjANBgkqhkiG9w0BAQsFADAtMSswKQYDVQQDEyJhY2NvdW50cy5hY2Nlc3Njb250cm9sLndpbmRvd3MubmV0MB4XDTIwMDYwNzAwMDAwMFoXDTI1MDYwNzAwMDAwMFowLTErMCkGA1UEAxMiYWNjb3VudHMuYWNjZXNzY29udHJvbC53aW5kb3dzLm5ldDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAOpZXSpuUXP7zCmtUTP07VL97ZrY+dsC0ayartd8hjN/4dHcK7tmT+d8uucA38+v7Swo6GLkQrlbI6Ft+/tM11DkSb2dC/dAgF/ufJVuzXBOGwMwNaV7+6EfZEMNF/HadGrVOB5x3mk1PC2cXIyTu/fx/XMYMGvnJSnxsZZXKLPE7LrqzPMYtnceVasM6jTAdrOtpdzEzewM3LR1IkAol9oiQKxowIbPpsUtcJsjCMjkoqXaHYY0FkQHLHlvmhVckUxVYvKJJdnE9RyYz13cdG9VqmEjs3kXa6y1HANKEdk86e8czmCWUhjZzS0KmvX+oeoedl219IgIMSoBA5UaWycCAwEAAaMhMB8wHQYDVR0OBBYEFFXP0ODFhjf3RS6oRijM5Tb+yB8CMA0GCSqGSIb3DQEBCwUAA4IBAQB9GtVikLTbJWIu5x9YCUTTKzNhi44XXogP/v8VylRSUHI5YTMdnWwvDIt/Y1sjNonmSy9PrioEjcIiI1U8nicveafMwIq5VLn+gEY2lg6KDJAzgAvA88CXqwfHHvtmYBovN7goolp8TY/kddMTf6TpNzN3lCTM2MK4Ye5xLLVGdp4bqWCOJ/qjwDxpTRSydYIkLUDwqNjv+sYfOElJpYAB4rTL/aw3ChJ1iaA4MtXEt6OjbUtbOa21lShfLzvNRbYK3+ukbrhmRl9lemJEeUls51vPuIe+jg+Ssp43aw7PQjxt4/MpfNMS2BfZ5F8GVSVG7qNb352cLLeJg5rc398Z"],"issuer":"https://login.microsoftonline.com/906aefe9-76a7-4f65-b82d-5ec20775d5aa/v2.0"},{"kty":"RSA","use":"sig","kid":"M6pX7RHoraLsprfJeRCjSxuURhc","x5t":"M6pX7RHoraLsprfJeRCjSxuURhc","n":"xHScZMPo8FifoDcrgncWQ7mGJtiKhrsho0-uFPXg-OdnRKYudTD7-Bq1MDjcqWRf3IfDVjFJixQS61M7wm9wALDj--lLuJJ9jDUAWTA3xWvQLbiBM-gqU0sj4mc2lWm6nPfqlyYeWtQcSC0sYkLlayNgX4noKDaXivhVOp7bwGXq77MRzeL4-9qrRYKjuzHfZL7kNBCsqO185P0NI2Jtmw-EsqYsrCaHsfNRGRrTvUHUq3hWa859kK_5uNd7TeY2ZEwKVD8ezCmSfR59ZzyxTtuPpkCSHS9OtUvS3mqTYit73qcvprjl3R8hpjXLb8oftfpWr3hFRdpxrwuoQEO4QQ","e":"AQAB","x5c":["MIIC8TCCAdmgAwIBAgIQfEWlTVc1uINEc9RBi6qHMjANBgkqhkiG9w0BAQsFADAjMSEwHwYDVQQDExhsb2dpbi5taWNyb3NvZnRvbmxpbmUudXMwHhcNMTgxMDE0MDAwMDAwWhcNMjAxMDE0MDAwMDAwWjAjMSEwHwYDVQQDExhsb2dpbi5taWNyb3NvZnRvbmxpbmUudXMwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDEdJxkw+jwWJ+gNyuCdxZDuYYm2IqGuyGjT64U9eD452dEpi51MPv4GrUwONypZF/ch8NWMUmLFBLrUzvCb3AAsOP76Uu4kn2MNQBZMDfFa9AtuIEz6CpTSyPiZzaVabqc9+qXJh5a1BxILSxiQuVrI2BfiegoNpeK+FU6ntvAZervsxHN4vj72qtFgqO7Md9kvuQ0EKyo7Xzk/Q0jYm2bD4SypiysJoex81EZGtO9QdSreFZrzn2Qr/m413tN5jZkTApUPx7MKZJ9Hn1nPLFO24+mQJIdL061S9LeapNiK3vepy+muOXdHyGmNctvyh+1+laveEVF2nGvC6hAQ7hBAgMBAAGjITAfMB0GA1UdDgQWBBQ5TKadw06O0cvXrQbXW0Nb3M3h/DANBgkqhkiG9w0BAQsFAAOCAQEAI48JaFtwOFcYS/3pfS5+7cINrafXAKTL+/+he4q+RMx4TCu/L1dl9zS5W1BeJNO2GUznfI+b5KndrxdlB6qJIDf6TRHh6EqfA18oJP5NOiKhU4pgkF2UMUw4kjxaZ5fQrSoD9omjfHAFNjradnHA7GOAoF4iotvXDWDBWx9K4XNZHWvD11Td66zTg5IaEQDIZ+f8WS6nn/98nAVMDtR9zW7Te5h9kGJGfe6WiHVaGRPpBvqC4iypGHjbRwANwofZvmp5wP08hY1CsnKY5tfP+E2k/iAQgKKa6QoxXToYvP7rsSkglak8N5g/+FJGnq4wP6cOzgZpjdPMwaVt5432GA=="],"issuer":"https://login.microsoftonline.com/906aefe9-76a7-4f65-b82d-5ec20775d5aa/v2.0"}]}`

	var jk JWKS
	if err := json.Unmarshal([]byte(jsonkey), &jk); err != nil {
		t.Fatal("Unmarshal: ", err)
	} else if len(jk.Keys) != 3 {
		t.Fatalf("Expected 3 keys, got %d", len(jk.Keys))
	}

	keys := make(map[string]crypto.PublicKey, len(jk.Keys))
	for ii, jks := range jk.Keys {
		var err error
		keys[jks.Kid], err = jks.DecodePublicKey()
		if err != nil {
			t.Fatalf("Failed to decode key %d: %v", ii, err)
		}
	}

	// The token is assembled at runtime from redacted placeholder claims so
	// that no JWT-like literal is stored in source (which would trip secret
	// scanners). It is intentionally non-functional: the signature is bogus
	// and identifying claims are placeholders. It only needs to be a parseable
	// Azure-style JWT so that Validate rejects it as non OIDC-compliant.
	jwtToken := makeAzureTestJWT(t)

	u1, err := xnet.ParseHTTPURL("http://localhost:8443")
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{}
	cfg.mutex = &sync.Mutex{}
	cfg.JWKS.URL = u1
	cfg.publicKeys = keys
	jwt := NewJWT(cfg)
	if jwt.ID() != "jwt" {
		t.Fatalf("Uexpected id %s for the validator", jwt.ID())
	}

	if _, err := jwt.Validate(jwtToken, ""); err == nil {
		// Azure should fail due to non OIDC compliant JWT
		// generated by Azure AD
		t.Fatal(err)
	}
}

func TestJWT(t *testing.T) {
	const jsonkey = `{"keys":
       [
         {"kty":"RSA",
          "n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
          "e":"AQAB",
          "alg":"RS256",
          "kid":"2011-04-29"}
       ]
     }`

	var jk JWKS
	if err := json.Unmarshal([]byte(jsonkey), &jk); err != nil {
		t.Fatal("Unmarshal: ", err)
	} else if len(jk.Keys) != 1 {
		t.Fatalf("Expected 1 keys, got %d", len(jk.Keys))
	}

	keys := make(map[string]crypto.PublicKey, len(jk.Keys))
	for ii, jks := range jk.Keys {
		var err error
		keys[jks.Kid], err = jks.DecodePublicKey()
		if err != nil {
			t.Fatalf("Failed to decode key %d: %v", ii, err)
		}
	}

	u1, err := xnet.ParseHTTPURL("http://localhost:8443")
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{}
	cfg.mutex = &sync.Mutex{}
	cfg.JWKS.URL = u1
	cfg.publicKeys = keys
	jwt := NewJWT(cfg)
	if jwt.ID() != "jwt" {
		t.Fatalf("Uexpected id %s for the validator", jwt.ID())
	}

	u, err := url.Parse("http://localhost:8443/?Token=invalid")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := jwt.Validate(u.Query().Get("Token"), ""); err == nil {
		t.Fatal(err)
	}
}

func TestDefaultExpiryDuration(t *testing.T) {
	testCases := []struct {
		reqURL    string
		duration  time.Duration
		expectErr bool
	}{
		{
			reqURL:   "http://localhost:8443/?Token=xxxxx",
			duration: time.Duration(60) * time.Minute,
		},
		{
			reqURL:    "http://localhost:8443/?DurationSeconds=9s",
			expectErr: true,
		},
		{
			reqURL:    "http://localhost:8443/?DurationSeconds=604801",
			expectErr: true,
		},
		{
			reqURL:    "http://localhost:8443/?DurationSeconds=800",
			expectErr: true,
		},
		{
			reqURL:   "http://localhost:8443/?DurationSeconds=901",
			duration: time.Duration(901) * time.Second,
		},
	}

	for i, testCase := range testCases {
		u, err := url.Parse(testCase.reqURL)
		if err != nil {
			t.Fatal(err)
		}
		d, err := GetDefaultExpiration(u.Query().Get("DurationSeconds"))
		gotErr := (err != nil)
		if testCase.expectErr != gotErr {
			t.Errorf("Test %d: Expected %v, got %v with error %s", i+1, testCase.expectErr, gotErr, err)
		}
		if d != testCase.duration {
			t.Errorf("Test %d: Expected duration %d, got %d", i+1, testCase.duration, d)
		}
	}
}
