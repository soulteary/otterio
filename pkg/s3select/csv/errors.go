/*
 * MinIO Cloud Storage, (C) 2019 MinIO, Inc.
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

package csv

import "errors"

type s3Error struct {
	code       string
	message    string
	statusCode int
	cause      error
}

func (err *s3Error) Cause() error {
	return err.cause
}

func (err *s3Error) ErrorCode() string {
	return err.code
}

func (err *s3Error) ErrorMessage() string {
	return err.message
}

func (err *s3Error) HTTPStatusCode() int {
	return err.statusCode
}

func (err *s3Error) Error() string {
	return err.message
}

func errCSVParsingError(err error) *s3Error {
	return &s3Error{
		code:       "CSVParsingError",
		message:    "Encountered an error parsing the CSV file. Check the file and try again.",
		statusCode: 400,
		cause:      err,
	}
}

func errInvalidTextEncodingError() *s3Error {
	return &s3Error{
		code:       "InvalidTextEncoding",
		message:    "UTF-8 encoding is required.",
		statusCode: 400,
		cause:      errors.New("invalid utf8 encoding"),
	}
}

// errCSVLineTooLong is returned when a single CSV line exceeds the maximum
// scan size without a newline. This bounds per-line memory usage and prevents
// an attacker from OOM-ing the server with input that contains no line breaks
// (GHSA-h749-fxx7-pwpg / CVE-2026-39414).
func errCSVLineTooLong() *s3Error {
	return &s3Error{
		code:       "CSVParsingError",
		message:    "Encountered a CSV line that exceeds the maximum supported length. Check the file and try again.",
		statusCode: 400,
		cause:      errors.New("csv line exceeds maximum scan size"),
	}
}
