/*
 * MinIO Cloud Storage, (C) 2015, 2016, 2017 MinIO, Inc.
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
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/textproto"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/soulteary/otterio/cmd/config"
)

// Tests validate bucket LocationConstraint.
func TestIsValidLocationContraint(t *testing.T) {
	obj, fsDir, err := prepareFS(t)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(fsDir)
	if err = newTestConfig(globalOtterioDefaultRegion, obj); err != nil {
		t.Fatal(err)
	}

	// Corrupted XML
	malformedReq := &http.Request{
		Body:          io.NopCloser(bytes.NewReader([]byte("<>"))),
		ContentLength: int64(len("<>")),
	}

	// Not an XML
	badRequest := &http.Request{
		Body:          io.NopCloser(bytes.NewReader([]byte("garbage"))),
		ContentLength: int64(len("garbage")),
	}

	// generates the input request with XML bucket configuration set to the request body.
	createExpectedRequest := func(req *http.Request, location string) *http.Request {
		createBucketConfig := createBucketLocationConfiguration{}
		createBucketConfig.Location = location
		createBucketConfigBytes, _ := xml.Marshal(createBucketConfig)
		createBucketConfigBuffer := bytes.NewReader(createBucketConfigBytes)
		req.Body = io.NopCloser(createBucketConfigBuffer)
		req.ContentLength = int64(createBucketConfigBuffer.Len())
		return req
	}

	testCases := []struct {
		request            *http.Request
		serverConfigRegion string
		expectedCode       APIErrorCode
	}{
		// Test case - 1.
		{createExpectedRequest(&http.Request{}, "eu-central-1"), globalOtterioDefaultRegion, ErrNone},
		// Test case - 2.
		// In case of empty request body ErrNone is returned.
		{createExpectedRequest(&http.Request{}, ""), globalOtterioDefaultRegion, ErrNone},
		// Test case - 3
		// In case of garbage request body ErrMalformedXML is returned.
		{badRequest, globalOtterioDefaultRegion, ErrMalformedXML},
		// Test case - 4
		// In case of invalid XML request body ErrMalformedXML is returned.
		{malformedReq, globalOtterioDefaultRegion, ErrMalformedXML},
	}

	for i, testCase := range testCases {
		config.SetRegion(globalServerConfig, testCase.serverConfigRegion)
		_, actualCode := parseLocationConstraint(testCase.request)
		if testCase.expectedCode != actualCode {
			t.Errorf("Test %d: Expected the APIErrCode to be %d, but instead found %d", i+1, testCase.expectedCode, actualCode)
		}
	}
}

// Test validate form field size.
func TestValidateFormFieldSize(t *testing.T) {
	testCases := []struct {
		header http.Header
		err    error
	}{
		// Empty header returns error as nil,
		{
			header: nil,
			err:    nil,
		},
		// Valid header returns error as nil.
		{
			header: http.Header{
				"Content-Type": []string{"image/png"},
			},
			err: nil,
		},
		// Invalid header value > maxFormFieldSize+1
		{
			header: http.Header{
				"Garbage": []string{strings.Repeat("a", int(maxFormFieldSize)+1)},
			},
			err: errSizeUnexpected,
		},
	}

	// Run validate form field size check under all test cases.
	for i, testCase := range testCases {
		err := validateFormFieldSize(context.Background(), testCase.header)
		if err != nil {
			if err.Error() != testCase.err.Error() {
				t.Errorf("Test %d: Expected error %s, got %s", i+1, testCase.err, err)
			}
		}
	}
}

// Tests validate metadata extraction from http headers.
func TestExtractMetadataHeaders(t *testing.T) {
	testCases := []struct {
		header     http.Header
		metadata   map[string]string
		shouldFail bool
	}{
		// Validate if there a known 'content-type'.
		{
			header: http.Header{
				"Content-Type": []string{"image/png"},
			},
			metadata: map[string]string{
				"content-type": "image/png",
			},
			shouldFail: false,
		},
		// Validate if there are no keys to extract.
		{
			header: http.Header{
				"Test-1": []string{"123"},
			},
			metadata:   map[string]string{},
			shouldFail: false,
		},
		// Validate that there are all headers extracted
		{
			header: http.Header{
				"X-Amz-Meta-Appid":     []string{"amz-meta"},
				"X-Otterio-Meta-Appid": []string{"otterio-meta"},
			},
			metadata: map[string]string{
				"X-Amz-Meta-Appid":     "amz-meta",
				"X-Otterio-Meta-Appid": "otterio-meta",
			},
			shouldFail: false,
		},
		// Fail if header key is not in canonicalized form
		{
			header: http.Header{
				"x-amz-meta-appid": []string{"amz-meta"},
			},
			metadata: map[string]string{
				"x-amz-meta-appid": "amz-meta",
			},
			shouldFail: false,
		},
		// Support multiple values
		{
			header: http.Header{
				"x-amz-meta-key": []string{"amz-meta1", "amz-meta2"},
			},
			metadata: map[string]string{
				"x-amz-meta-key": "amz-meta1,amz-meta2",
			},
			shouldFail: false,
		},
		// Empty header input returns empty metadata.
		{
			header:     nil,
			metadata:   nil,
			shouldFail: true,
		},
	}

	// Validate if the extracting headers.
	for i, testCase := range testCases {
		metadata := make(map[string]string)
		err := extractMetadataFromMime(context.Background(), textproto.MIMEHeader(testCase.header), metadata)
		if err != nil && !testCase.shouldFail {
			t.Fatalf("Test %d failed to extract metadata: %v", i+1, err)
		}
		if err == nil && testCase.shouldFail {
			t.Fatalf("Test %d should fail, but it passed", i+1)
		}
		if err == nil && !reflect.DeepEqual(metadata, testCase.metadata) {
			t.Fatalf("Test %d failed: Expected \"%#v\", got \"%#v\"", i+1, testCase.metadata, metadata)
		}
	}
}

// Test getResource()
func TestGetResource(t *testing.T) {
	testCases := []struct {
		p                string
		host             string
		domains          []string
		expectedResource string
	}{
		{"/a/b/c", "test.mydomain.com", []string{"mydomain.com"}, "/test/a/b/c"},
		{"/a/b/c", "test.mydomain.com", []string{"notmydomain.com"}, "/a/b/c"},
		{"/a/b/c", "test.mydomain.com", nil, "/a/b/c"},
	}
	for i, test := range testCases {
		gotResource, err := getResource(test.p, test.host, test.domains)
		if err != nil {
			t.Fatal(err)
		}
		if gotResource != test.expectedResource {
			t.Fatalf("test %d: expected %s got %s", i+1, test.expectedResource, gotResource)
		}
	}
}

// TestExtractMetadataReservedPrefixStripped is the regression test for
// GHSA-3rh2-v3gr-35p9 (SSE metadata injection). Even when the reserved-header
// middleware is bypassed (e.g. because a future router forgets to mount it),
// extractMetadataFromMime must drop any user-supplied key whose canonical or
// "X-Amz-Meta-" wrapped form falls inside the OtterIO/MinIO internal
// namespace, so a malicious client cannot taint freshly uploaded objects with
// fake SSE bookkeeping that would render them unreadable.
func TestExtractMetadataReservedPrefixStripped(t *testing.T) {
	testCases := []struct {
		name             string
		header           http.Header
		keysShouldStay   []string
		keysShouldVanish []string
	}{
		{
			name: "wrapped-otterio-internal-iv",
			header: http.Header{
				"X-Amz-Meta-X-Otterio-Internal-Server-Side-Encryption-Iv": []string{"AAAA"},
				"X-Amz-Meta-Appid": []string{"app"},
			},
			keysShouldStay:   []string{"X-Amz-Meta-Appid"},
			keysShouldVanish: []string{"X-Amz-Meta-X-Otterio-Internal-Server-Side-Encryption-Iv"},
		},
		{
			name: "wrapped-otterio-internal-sealed-key-lowercase",
			header: http.Header{
				"x-amz-meta-x-otterio-internal-server-side-encryption-sealed-key": []string{"BBBB"},
			},
			keysShouldVanish: []string{"x-amz-meta-x-otterio-internal-server-side-encryption-sealed-key"},
		},
		{
			name: "wrapped-minio-internal-legacy",
			header: http.Header{
				"X-Amz-Meta-X-Minio-Internal-Server-Side-Encryption-Iv": []string{"CCCC"},
			},
			keysShouldVanish: []string{"X-Amz-Meta-X-Minio-Internal-Server-Side-Encryption-Iv"},
		},
		{
			name: "bare-otterio-internal-via-extractor",
			header: http.Header{
				"X-Otterio-Internal-Server-Side-Encryption-Iv": []string{"DDDD"},
				"X-Amz-Meta-Appid": []string{"app"},
			},
			keysShouldStay:   []string{"X-Amz-Meta-Appid"},
			keysShouldVanish: []string{"X-Otterio-Internal-Server-Side-Encryption-Iv"},
		},
		{
			name: "benign-meta-with-internal-substring",
			header: http.Header{
				"X-Amz-Meta-Internal-Note": []string{"ok"},
			},
			keysShouldStay: []string{"X-Amz-Meta-Internal-Note"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			metadata := make(map[string]string)
			if err := extractMetadataFromMime(context.Background(), textproto.MIMEHeader(tc.header), metadata); err != nil {
				t.Fatalf("extractMetadataFromMime returned unexpected error: %v", err)
			}
			for _, k := range tc.keysShouldVanish {
				if _, ok := metadata[k]; ok {
					t.Fatalf("reserved key %q must not survive extraction, got metadata=%v", k, metadata)
				}
			}
			for _, k := range tc.keysShouldStay {
				if _, ok := metadata[k]; !ok {
					t.Fatalf("benign key %q was unexpectedly stripped, got metadata=%v", k, metadata)
				}
			}
		})
	}
}

// TestHasReservedMetadataPrefix exercises the shared prefix predicate used by
// both the router-edge middleware and the metadata extractor. Keep this test
// in sync with reservedMetadataPrefixesLower.
func TestHasReservedMetadataPrefix(t *testing.T) {
	cases := []struct {
		key      string
		reserved bool
	}{
		{"X-Otterio-Internal-Server-Side-Encryption-Iv", true},
		{"x-otterio-internal-foo", true},
		{"X-Amz-Meta-X-Otterio-Internal-Server-Side-Encryption-Iv", true},
		{"X-Minio-Internal-Server-Side-Encryption-Iv", true},
		{"X-Amz-Meta-X-Minio-Internal-Server-Side-Encryption-Iv", true},
		{"X-Amz-Meta-Appid", false},
		{"X-Otterio-Meta-Appid", false},
		{"X-Amz-Meta-Internal", false},
		{"Content-Type", false},
	}
	for _, c := range cases {
		if got := hasReservedMetadataPrefix(c.key); got != c.reserved {
			t.Errorf("hasReservedMetadataPrefix(%q) = %v, want %v", c.key, got, c.reserved)
		}
	}
}
