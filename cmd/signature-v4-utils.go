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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/auth"
)

// http Header "x-amz-content-sha256" == "UNSIGNED-PAYLOAD" indicates that the
// client did not calculate sha256 of the payload.
const unsignedPayload = "UNSIGNED-PAYLOAD"

// getContentSha256Header returns the caller-supplied X-Amz-Content-Sha256
// value and whether it was present, treating an explicitly empty header /
// query parameter as "not provided". The empty-as-missing rule is what
// closes upstream-cve-backlog.md row 51's "x-amz-content-sha256 validation"
// class of issues: the legacy implementation read v[0] off the slice, which
// (a) panicked on a length-0 slice and (b) accepted v[0] == "" as a real
// hash, allowing the canonicaliser to compare against an empty string.
//
// For presigned requests the query parameter wins over the header, matching
// the legacy precedence rule and the AWS spec.
func getContentSha256Header(r *http.Request) (value string, present bool) {
	if isRequestPresignedSignatureV4(r) {
		if v, ok := r.URL.Query()[xhttp.AmzContentSha256]; ok && len(v) > 0 && v[0] != "" {
			return v[0], true
		}
	}
	if v, ok := r.Header[xhttp.AmzContentSha256]; ok && len(v) > 0 && v[0] != "" {
		return v[0], true
	}
	return "", false
}

// skipContentSha256Cksum returns true if caller needs to skip
// payload checksum, false if not.
func skipContentSha256Cksum(r *http.Request) bool {
	value, present := getContentSha256Header(r)
	if !present {
		// No usable X-Amz-Content-Sha256 - skip payload validation. This
		// matches the AWS-documented default of UNSIGNED-PAYLOAD when the
		// header is absent on a presigned request, and is the same
		// behavior as the legacy implementation when the header was
		// missing on a signed request.
		return true
	}
	// If x-amz-content-sha256 is set and the value is not
	// 'UNSIGNED-PAYLOAD' we should validate the content sha256.
	return value == unsignedPayload
}

// Returns SHA256 for calculating canonical-request.
func getContentSha256Cksum(r *http.Request, stype serviceType) string {
	if stype == serviceSTS {
		payload, err := io.ReadAll(io.LimitReader(r.Body, stsRequestBodyLimit))
		if err != nil {
			logger.CriticalIf(GlobalContext, err)
		}
		sum256 := sha256.Sum256(payload)
		r.Body = io.NopCloser(bytes.NewReader(payload))
		return hex.EncodeToString(sum256[:])
	}

	if value, present := getContentSha256Header(r); present {
		// We found a non-empty 'X-Amz-Content-Sha256'; return it as-is.
		// skipContentSha256Cksum already used the same helper to decide
		// whether the value is UNSIGNED-PAYLOAD, so the two functions stay
		// in lock-step on what counts as "header was provided".
		return value
	}

	// We couldn't find a usable 'X-Amz-Content-Sha256'. Different defaults
	// apply depending on whether the request was presigned: presigned
	// defaults to UNSIGNED-PAYLOAD per the AWS spec; signed requests
	// default to sha256("").
	if isRequestPresignedSignatureV4(r) {
		return unsignedPayload
	}
	return emptySHA256
}

// isValidRegion - verify if incoming region value is valid with configured Region.
func isValidRegion(reqRegion string, confRegion string) bool {
	if confRegion == "" {
		return true
	}
	if confRegion == "US" {
		confRegion = globalOtterioDefaultRegion
	}
	// Some older s3 clients set region as "US" instead of
	// globalOtterioDefaultRegion, handle it.
	if reqRegion == "US" {
		reqRegion = globalOtterioDefaultRegion
	}
	return reqRegion == confRegion
}

// check if the access key is valid and recognized, additionally
// also returns if the access key is owner/admin.
func checkKeyValid(accessKey string) (auth.Credentials, bool, APIErrorCode) {
	var owner = true
	var cred = globalActiveCred
	if cred.AccessKey != accessKey {
		// Check if the access key is part of users credentials.
		var ok bool
		if cred, ok = globalIAMSys.GetUser(accessKey); !ok {
			return cred, false, ErrInvalidAccessKeyID
		}
		owner = false
	}
	return cred, owner, ErrNone
}

// sumHMAC calculate hmac between two input byte array.
func sumHMAC(key []byte, data []byte) []byte {
	hash := hmac.New(sha256.New, key)
	hash.Write(data)
	return hash.Sum(nil)
}

// extractSignedHeaders extract signed headers from Authorization header
func extractSignedHeaders(signedHeaders []string, r *http.Request) (http.Header, APIErrorCode) {
	reqHeaders := r.Header
	reqQueries := r.URL.Query()
	// find whether "host" is part of list of signed headers.
	// if not return ErrUnsignedHeaders. "host" is mandatory.
	if !contains(signedHeaders, "host") {
		return nil, ErrUnsignedHeaders
	}
	extractedSignedHeaders := make(http.Header)
	for _, header := range signedHeaders {
		// `host` will not be found in the headers, can be found in r.Host.
		// but its alway necessary that the list of signed headers containing host in it.
		val, ok := reqHeaders[http.CanonicalHeaderKey(header)]
		if !ok {
			// try to set headers from Query String
			val, ok = reqQueries[header]
		}
		if ok {
			extractedSignedHeaders[http.CanonicalHeaderKey(header)] = val
			continue
		}
		switch header {
		case "expect":
			// Golang http server strips off 'Expect' header, if the
			// client sent this as part of signed headers we need to
			// handle otherwise we would see a signature mismatch.
			// `aws-cli` sets this as part of signed headers.
			//
			// According to
			// http://www.w3.org/Protocols/rfc2616/rfc2616-sec14.html#sec14.20
			// Expect header is always of form:
			//
			//   Expect       =  "Expect" ":" 1#expectation
			//   expectation  =  "100-continue" | expectation-extension
			//
			// So it safe to assume that '100-continue' is what would
			// be sent, for the time being keep this work around.
			// Adding a *TODO* to remove this later when Golang server
			// doesn't filter out the 'Expect' header.
			extractedSignedHeaders.Set(header, "100-continue")
		case "host":
			// Go http server removes "host" from Request.Header
			extractedSignedHeaders.Set(header, r.Host)
		case "transfer-encoding":
			// Go http server removes "host" from Request.Header
			extractedSignedHeaders[http.CanonicalHeaderKey(header)] = r.TransferEncoding
		case "content-length":
			// Signature-V4 spec excludes Content-Length from signed headers list for signature calculation.
			// But some clients deviate from this rule. Hence we consider Content-Length for signature
			// calculation to be compatible with such clients.
			extractedSignedHeaders.Set(header, strconv.FormatInt(r.ContentLength, 10))
		default:
			return nil, ErrUnsignedHeaders
		}
	}
	return extractedSignedHeaders, ErrNone
}

// Trim leading and trailing spaces and replace sequential spaces with one space, following Trimall()
// in http://docs.aws.amazon.com/general/latest/gr/sigv4-create-canonical-request.html
func signV4TrimAll(input string) string {
	// Compress adjacent spaces (a space is determined by
	// unicode.IsSpace() internally here) to one space and return
	return strings.Join(strings.Fields(input), " ")
}
