/*
 * MinIO Cloud Storage, (C) 2016-2020 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
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
	"context"
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v3"
	xhttp "github.com/soulteary/otterio/cmd/http"
)

func writeResponseFiber(c fiber.Ctx, statusCode int, response []byte, mType mimeType) {
	setCommonHeadersFiber(c)
	if mType != mimeNone {
		c.Set(xhttp.ContentType, string(mType))
	}
	c.Set(xhttp.ContentLength, strconv.Itoa(len(response)))
	c.Status(statusCode)
	if response != nil {
		_, _ = c.Write(response)
	}
}

//nolint:unused
func writeSuccessResponseJSONFiber(c fiber.Ctx, response []byte) {
	writeResponseFiber(c, fiber.StatusOK, response, mimeJSON)
}

//nolint:unused
func writeSuccessResponseXMLFiber(c fiber.Ctx, response []byte) {
	writeResponseFiber(c, fiber.StatusOK, response, mimeXML)
}

//nolint:unused
func writeSuccessNoContentFiber(c fiber.Ctx) {
	writeResponseFiber(c, fiber.StatusNoContent, nil, mimeNone)
}

//nolint:unused
func writeRedirectSeeOtherFiber(c fiber.Ctx, location string) {
	c.Set(xhttp.Location, location)
	writeResponseFiber(c, fiber.StatusSeeOther, nil, mimeNone)
}

//nolint:unused
func writeSuccessResponseHeadersOnlyFiber(c fiber.Ctx) {
	writeResponseFiber(c, fiber.StatusOK, nil, mimeNone)
}

func writeErrorResponseFiber(ctx context.Context, c fiber.Ctx, err APIError, browser bool) {
	reqURL := requestURL(c)
	switch err.Code {
	case "SlowDown", "XOtterioServerNotInitialized", "XOtterioReadQuorum", "XOtterioWriteQuorum":
		c.Set(xhttp.RetryAfter, "120")
	case "InvalidRegion":
		err.Description = fmt.Sprintf("Region does not match; expecting '%s'.", globalServerRegion)
	case "AuthorizationHeaderMalformed":
		err.Description = fmt.Sprintf("The authorization header is malformed; the region is wrong; expecting '%s'.", globalServerRegion)
	case "AccessDenied":
		if browser && globalBrowserEnabled {
			c.Set(xhttp.Location, otterioReservedBucketPath+reqURL.Path)
			c.Status(fiber.StatusTemporaryRedirect)
			return
		}
	}

	errorResponse := getAPIErrorResponse(ctx, err, reqURL.Path,
		fiberRequestID(c), globalDeploymentID)
	encodedErrorResponse := encodeResponse(errorResponse)
	writeResponseFiber(c, err.HTTPStatusCode, encodedErrorResponse, mimeXML)
}

func writeErrorResponseHeadersOnlyFiber(c fiber.Ctx, err APIError) {
	writeResponseFiber(c, err.HTTPStatusCode, nil, mimeNone)
}

func writeErrorResponseStringFiber(_ context.Context, c fiber.Ctx, err APIError) {
	writeResponseFiber(c, err.HTTPStatusCode, []byte(err.Description), mimeNone)
}

func writeErrorResponseJSONFiber(ctx context.Context, c fiber.Ctx, err APIError) {
	reqURL := requestURL(c)
	errorResponse := getAPIErrorResponse(ctx, err, reqURL.Path, fiberRequestID(c), globalDeploymentID)
	encodedErrorResponse := encodeResponseJSON(errorResponse)
	writeResponseFiber(c, err.HTTPStatusCode, encodedErrorResponse, mimeJSON)
}

func setCommonHeadersFiber(c fiber.Ctx) {
	c.Set(xhttp.ServerInfo, "OtterIO")
	if region := globalServerRegion; region != "" {
		c.Set(xhttp.AmzBucketRegion, region)
	}
	c.Set(xhttp.AcceptRanges, "bytes")

	// Strip confidential encryption material from the response, mirroring the
	// net/http setCommonHeaders -> crypto.RemoveSensitiveHeaders behavior. Keep
	// these keys in sync with crypto.RemoveSensitiveHeaders. Operate on the
	// response pointer's header field directly (copying it into a local would
	// make Del a no-op).
	resp := c.Response()
	resp.Header.Del(xhttp.AmzServerSideEncryptionCustomerKey)
	resp.Header.Del(xhttp.AmzServerSideEncryptionCopyCustomerKey)
	resp.Header.Del(xhttp.AmzMetaUnencryptedContentLength)
	resp.Header.Del(xhttp.AmzMetaUnencryptedContentMD5)
}
