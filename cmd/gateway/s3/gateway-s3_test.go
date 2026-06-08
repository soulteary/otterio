/*
 * MinIO Cloud Storage, (C) 2017 MinIO, Inc.
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

package s3

import (
	"fmt"
	"testing"

	otteriogo "github.com/minio/minio-go/v7"
	"github.com/soulteary/otterio/pkg/hash"

	otterio "github.com/soulteary/otterio/cmd"
)

func errResponse(code string) otteriogo.ErrorResponse {
	return otteriogo.ErrorResponse{
		Code: code,
	}
}

func TestS3ToObjectError(t *testing.T) {
	testCases := []struct {
		inputErr       error
		expectedErr    error
		bucket, object string
	}{
		{
			inputErr:    errResponse("BucketAlreadyOwnedByYou"),
			expectedErr: otterio.BucketAlreadyOwnedByYou{},
		},
		{
			inputErr:    errResponse("BucketNotEmpty"),
			expectedErr: otterio.BucketNotEmpty{},
		},
		{
			inputErr:    errResponse("InvalidBucketName"),
			expectedErr: otterio.BucketNameInvalid{},
		},
		{
			inputErr:    errResponse("InvalidPart"),
			expectedErr: otterio.InvalidPart{},
		},
		{
			inputErr:    errResponse("NoSuchBucketPolicy"),
			expectedErr: otterio.BucketPolicyNotFound{},
		},
		{
			inputErr:    errResponse("NoSuchBucket"),
			expectedErr: otterio.BucketNotFound{},
		},
		// with empty Object in otteriogo.ErrorResponse, NoSuchKey
		// is interpreted as BucketNotFound
		{
			inputErr:    errResponse("NoSuchKey"),
			expectedErr: otterio.BucketNotFound{},
		},
		{
			inputErr:    errResponse("NoSuchUpload"),
			expectedErr: otterio.InvalidUploadID{},
		},
		{
			inputErr:    errResponse("XOtterioInvalidObjectName"),
			expectedErr: otterio.ObjectNameInvalid{},
		},
		{
			inputErr:    errResponse("AccessDenied"),
			expectedErr: otterio.PrefixAccessDenied{},
		},
		{
			inputErr:    errResponse("XAmzContentSHA256Mismatch"),
			expectedErr: hash.SHA256Mismatch{},
		},
		{
			inputErr:    errResponse("EntityTooSmall"),
			expectedErr: otterio.PartTooSmall{},
		},
		{
			inputErr:    nil,
			expectedErr: nil,
		},
		// Special test case for NoSuchKey with object name
		{
			inputErr: otteriogo.ErrorResponse{
				Code: "NoSuchKey",
			},
			expectedErr: otterio.ObjectNotFound{
				Bucket: "bucket",
				Object: "object",
			},
			bucket: "bucket",
			object: "object",
		},

		// N B error values that aren't of expected types
		// should be left untouched.
		// Special test case for error that is not of type
		// otteriogo.ErrorResponse
		{
			inputErr:    fmt.Errorf("not a ErrorResponse"),
			expectedErr: fmt.Errorf("not a ErrorResponse"),
		},
	}

	for i, tc := range testCases {
		actualErr := otterio.ErrorRespToObjectError(tc.inputErr, tc.bucket, tc.object)
		if actualErr != nil && tc.expectedErr != nil && actualErr.Error() != tc.expectedErr.Error() {
			t.Errorf("Test case %d: Expected error %v but received error %v", i+1, tc.expectedErr, actualErr)
		}
	}
}
