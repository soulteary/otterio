/*
 * MinIO Cloud Storage, (C) 2016, 2017 MinIO, Inc.
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

package cmd

import (
	"net/http"
	"os"
	"testing"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"

	xjwt "github.com/soulteary/otterio/cmd/jwt"
)

// Tests the shared metrics/request bearer authenticator.
func TestBearerRequestAuthenticate(t *testing.T) {
	obj, fsDir, err := prepareFS(t)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(fsDir)
	if err = newTestConfig(globalOtterioDefaultRegion, obj); err != nil {
		t.Fatal(err)
	}

	creds := globalActiveCred
	token, err := getTokenString(creds.AccessKey, creds.SecretKey)
	if err != nil {
		t.Fatalf("unable get token %s", err)
	}
	testCases := []struct {
		req         *http.Request
		expectedErr error
	}{
		// Set valid authorization header.
		{
			req: &http.Request{
				Header: http.Header{
					"Authorization": []string{token},
				},
			},
			expectedErr: nil,
		},
		// No authorization header.
		{
			req: &http.Request{
				Header: http.Header{},
			},
			expectedErr: errNoAuthToken,
		},
		// Invalid authorization token.
		{
			req: &http.Request{
				Header: http.Header{
					"Authorization": []string{"invalid-token"},
				},
			},
			expectedErr: errAuthentication,
		},
	}

	for i, testCase := range testCases {
		_, _, gotErr := authenticateBearerRequest(testCase.req)
		if testCase.expectedErr != gotErr {
			t.Errorf("Test %d, expected err %s, got %s", i+1, testCase.expectedErr, gotErr)
		}
	}
}

func BenchmarkParseJWTStandardClaims(b *testing.B) {
	obj, fsDir, err := prepareFS(b)
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(fsDir)
	if err = newTestConfig(globalOtterioDefaultRegion, obj); err != nil {
		b.Fatal(err)
	}

	creds := globalActiveCred
	token, err := authenticateNode(creds.AccessKey, creds.SecretKey, "")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err = xjwt.ParseWithStandardClaims(token, xjwt.NewStandardClaims(), []byte(creds.SecretKey))
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkParseJWTMapClaims(b *testing.B) {
	obj, fsDir, err := prepareFS(b)
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(fsDir)
	if err = newTestConfig(globalOtterioDefaultRegion, obj); err != nil {
		b.Fatal(err)
	}

	creds := globalActiveCred
	token, err := authenticateNode(creds.AccessKey, creds.SecretKey, "")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err = xjwt.ParseWithClaims(token, xjwt.NewMapClaims(), func(*xjwt.MapClaims) ([]byte, error) {
				return []byte(creds.SecretKey), nil
			})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkAuthenticateNode(b *testing.B) {
	obj, fsDir, err := prepareFS(b)
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(fsDir)
	if err = newTestConfig(globalOtterioDefaultRegion, obj); err != nil {
		b.Fatal(err)
	}

	creds := globalActiveCred
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		authenticateNode(creds.AccessKey, creds.SecretKey, "")
	}
}

func getTokenString(accessKey, secretKey string) (string, error) {
	claims := xjwt.NewMapClaims()
	claims.SetExpiry(UTCNow().Add(time.Hour))
	claims.SetAccessKey(accessKey)
	return jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, claims).SignedString([]byte(secretKey))
}
