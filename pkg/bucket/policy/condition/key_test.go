/*
 * MinIO Cloud Storage, (C) 2018 MinIO, Inc.
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

package condition

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestKeyIsValid(t *testing.T) {
	testCases := []struct {
		key            Key
		expectedResult bool
	}{
		{S3XAmzCopySource, true},
		{S3XAmzServerSideEncryption, true},
		{S3XAmzServerSideEncryptionCustomerAlgorithm, true},
		{S3XAmzMetadataDirective, true},
		{S3XAmzStorageClass, true},
		{S3LocationConstraint, true},
		{S3Prefix, true},
		{S3Delimiter, true},
		{S3MaxKeys, true},
		{AWSReferer, true},
		{AWSSourceIP, true},
		{Key("foo"), false},
	}

	for i, testCase := range testCases {
		result := testCase.key.IsValid()

		if testCase.expectedResult != result {
			t.Fatalf("case %v: expected: %v, got: %v\n", i+1, testCase.expectedResult, result)
		}
	}
}

func TestKeyMarshalJSON(t *testing.T) {
	testCases := []struct {
		key            Key
		expectedResult []byte
		expectErr      bool
	}{
		{S3XAmzCopySource, []byte(`"s3:x-amz-copy-source"`), false},
		{Key("foo"), nil, true},
	}

	for i, testCase := range testCases {
		result, err := json.Marshal(testCase.key)
		expectErr := (err != nil)

		if testCase.expectErr != expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v\n", i+1, testCase.expectErr, expectErr)
		}

		if !testCase.expectErr {
			if !reflect.DeepEqual(result, testCase.expectedResult) {
				t.Fatalf("case %v: key: expected: %v, got: %v\n", i+1, string(testCase.expectedResult), string(result))
			}
		}
	}
}

func TestKeyName(t *testing.T) {
	testCases := []struct {
		key            Key
		expectedResult string
	}{
		{S3XAmzCopySource, "x-amz-copy-source"},
		{AWSReferer, "Referer"},
	}

	for i, testCase := range testCases {
		result := testCase.key.Name()

		if testCase.expectedResult != result {
			t.Fatalf("case %v: expected: %v, got: %v\n", i+1, testCase.expectedResult, result)
		}
	}
}

func TestKeyUnmarshalJSON(t *testing.T) {
	testCases := []struct {
		data        []byte
		expectedKey Key
		expectErr   bool
	}{
		{[]byte(`"s3:x-amz-copy-source"`), S3XAmzCopySource, false},
		{[]byte(`"foo"`), Key(""), true},
	}

	for i, testCase := range testCases {
		var key Key
		err := json.Unmarshal(testCase.data, &key)
		expectErr := (err != nil)

		if testCase.expectErr != expectErr {
			t.Fatalf("case %v: error: expected: %v, got: %v\n", i+1, testCase.expectErr, expectErr)
		}

		if !testCase.expectErr {
			if testCase.expectedKey != key {
				t.Fatalf("case %v: key: expected: %v, got: %v\n", i+1, testCase.expectedKey, key)
			}
		}
	}
}

func TestKeySetAdd(t *testing.T) {
	testCases := []struct {
		set            KeySet
		key            Key
		expectedResult KeySet
	}{
		{NewKeySet(), S3XAmzCopySource, NewKeySet(S3XAmzCopySource)},
		{NewKeySet(S3XAmzCopySource), S3XAmzCopySource, NewKeySet(S3XAmzCopySource)},
	}

	for i, testCase := range testCases {
		testCase.set.Add(testCase.key)

		if !reflect.DeepEqual(testCase.expectedResult, testCase.set) {
			t.Fatalf("case %v: expected: %v, got: %v\n", i+1, testCase.expectedResult, testCase.set)
		}
	}
}

func TestKeySetDifference(t *testing.T) {
	testCases := []struct {
		set            KeySet
		setToDiff      KeySet
		expectedResult KeySet
	}{
		{NewKeySet(), NewKeySet(S3XAmzCopySource), NewKeySet()},
		{NewKeySet(S3Prefix, S3Delimiter, S3MaxKeys), NewKeySet(S3Delimiter, S3MaxKeys), NewKeySet(S3Prefix)},
	}

	for i, testCase := range testCases {
		result := testCase.set.Difference(testCase.setToDiff)

		if !reflect.DeepEqual(testCase.expectedResult, result) {
			t.Fatalf("case %v: expected: %v, got: %v\n", i+1, testCase.expectedResult, result)
		}
	}
}

func TestKeySetIsEmpty(t *testing.T) {
	testCases := []struct {
		set            KeySet
		expectedResult bool
	}{
		{NewKeySet(), true},
		{NewKeySet(S3Delimiter), false},
	}

	for i, testCase := range testCases {
		result := testCase.set.IsEmpty()

		if testCase.expectedResult != result {
			t.Fatalf("case %v: expected: %v, got: %v\n", i+1, testCase.expectedResult, result)
		}
	}
}

func TestKeySetString(t *testing.T) {
	testCases := []struct {
		set            KeySet
		expectedResult string
	}{
		{NewKeySet(), `[]`},
		{NewKeySet(S3Delimiter), `[s3:delimiter]`},
	}

	for i, testCase := range testCases {
		result := testCase.set.String()

		if testCase.expectedResult != result {
			t.Fatalf("case %v: expected: %v, got: %v\n", i+1, testCase.expectedResult, result)
		}
	}
}

func TestKeySetToSlice(t *testing.T) {
	testCases := []struct {
		set            KeySet
		expectedResult []Key
	}{
		{NewKeySet(), []Key{}},
		{NewKeySet(S3Delimiter), []Key{S3Delimiter}},
	}

	for i, testCase := range testCases {
		result := testCase.set.ToSlice()

		if !reflect.DeepEqual(testCase.expectedResult, result) {
			t.Fatalf("case %v: expected: %v, got: %v\n", i+1, testCase.expectedResult, result)
		}
	}
}

// TestExistingObjectTagConditionParsedAndMatches pins the behavior of the
// per-tag prefix condition keys ("s3:ExistingObjectTag/<k>",
// "s3:RequestObjectTag/<k>"). It covers:
//
//   - JSON unmarshal accepts "s3:ExistingObjectTag/dept" but rejects the bare
//     "s3:ExistingObjectTag/" prefix.
//   - IsValid mirrors the unmarshal path.
//   - StringEquals condition over a per-tag key returns true when the request
//     supplies a matching value under "ExistingObjectTag/<k>" and false
//     otherwise. The lookup key is intentionally the s3:-stripped form,
//     because Key.Name() trims the "s3:" prefix and stringEqualsFunc.evaluate
//     uses Name() (see stringequalsfunc.go).
//   - Difference treats a concrete prefix-form key as covered by the bare
//     prefix in the allowed-key set, so action-allowed maps can register
//     "s3:ExistingObjectTag/" once and admit any tag-key family member.
func TestExistingObjectTagConditionParsedAndMatches(t *testing.T) {
	t.Run("IsValid prefix forms", func(t *testing.T) {
		validCases := []Key{
			Key("s3:ExistingObjectTag/dept"),
			Key("s3:ExistingObjectTag/customer-id"),
			Key("s3:RequestObjectTag/dept"),
		}
		for _, k := range validCases {
			if !k.IsValid() {
				t.Errorf("expected %q to be valid", k)
			}
		}

		invalidCases := []Key{
			S3ExistingObjectTag, // bare prefix
			S3RequestObjectTag,  // bare prefix
			Key("s3:ExistingObjectTag"),
			Key("s3:RequestObjectTagdept"),
		}
		for _, k := range invalidCases {
			if k.IsValid() {
				t.Errorf("expected %q to be invalid", k)
			}
		}
	})

	t.Run("Unmarshal accepts concrete and rejects bare prefix", func(t *testing.T) {
		var k Key
		if err := json.Unmarshal([]byte(`"s3:ExistingObjectTag/dept"`), &k); err != nil {
			t.Fatalf("unexpected error parsing concrete key: %v", err)
		}
		if k != Key("s3:ExistingObjectTag/dept") {
			t.Fatalf("unexpected key: %v", k)
		}

		var k2 Key
		if err := json.Unmarshal([]byte(`"s3:ExistingObjectTag/"`), &k2); err == nil {
			t.Fatalf("expected error for bare prefix key, got %v", k2)
		}
	})

	t.Run("StringEquals matches request value via stripped lookup name", func(t *testing.T) {
		k := Key("s3:ExistingObjectTag/dept")
		fn, err := NewStringEqualsFunc(k, "finance")
		if err != nil {
			t.Fatalf("NewStringEqualsFunc: %v", err)
		}

		// stringEqualsFunc.evaluate uses Key.Name(), which strips "s3:".
		matchValues := map[string][]string{
			"ExistingObjectTag/dept": {"finance"},
		}
		if !fn.(*stringEqualsFunc).evaluate(matchValues) {
			t.Fatalf("expected match for %v with values %v", k, matchValues)
		}

		nonMatchValues := map[string][]string{
			"ExistingObjectTag/dept": {"engineering"},
		}
		if fn.(*stringEqualsFunc).evaluate(nonMatchValues) {
			t.Fatalf("expected mismatch for %v with values %v", k, nonMatchValues)
		}

		emptyValues := map[string][]string{}
		if fn.(*stringEqualsFunc).evaluate(emptyValues) {
			t.Fatalf("expected mismatch for %v with empty values", k)
		}
	})

	t.Run("Difference treats prefix as covering concrete key", func(t *testing.T) {
		policyKey := Key("s3:ExistingObjectTag/dept")
		statementKeys := NewKeySet(policyKey)
		actionAllowed := NewKeySet(S3ExistingObjectTag, AWSReferer)

		diff := statementKeys.Difference(actionAllowed)
		if !diff.IsEmpty() {
			t.Fatalf("expected concrete tag key to be covered by bare prefix, got diff=%v", diff)
		}

		// Sanity: an unrelated key should still surface in the diff.
		unrelated := NewKeySet(policyKey, Key("s3:UnknownThing"))
		if d := unrelated.Difference(actionAllowed); d.IsEmpty() {
			t.Fatalf("expected unrelated key to remain in diff")
		}
	})
}
