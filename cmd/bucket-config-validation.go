// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
)

// CAS edits must describe only behavior implemented by this server. Legacy
// parsers can discard unknown fields, and a single Decode may not consume the
// body far enough to verify its digest. Keep legacy unconditional semantics.
func prepareConditionalBucketConfiguration(ctx context.Context, w http.ResponseWriter, r *http.Request, file string) bool {
	if len(r.Header.Values(bucketConfigIfMatchHeader)) == 0 {
		return true
	}
	if _, code := bucketConfigCondition(r, file); code != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(code), r.URL, guessIsBrowserReq(r))
		return false
	}
	fail := func(code APIErrorCode) bool {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(code), r.URL, guessIsBrowserReq(r))
		return false
	}
	shaValues := r.Header.Values("X-Amz-Content-Sha256")
	if len(shaValues) != 1 || len(shaValues[0]) != 64 {
		return fail(ErrContentSHA256Mismatch)
	}
	if _, err := hex.DecodeString(shaValues[0]); err != nil {
		return fail(ErrContentSHA256Mismatch)
	}
	limit := int64(maxBucketVersioningConfigSize)
	if file == bucketPolicyConfig {
		limit = int64(maxBucketPolicySize)
	}
	if r.ContentLength < 0 {
		return fail(ErrMissingContentLength)
	}
	if r.ContentLength > limit {
		return fail(ErrEntityTooLarge)
	}
	if r.Method == http.MethodDelete && r.ContentLength != 0 {
		return fail(ErrInvalidRequest)
	}
	data, err := readConfigurationBody(ctx, r, limit)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL, guessIsBrowserReq(r))
		return false
	}
	if int64(len(data)) > limit {
		return fail(ErrEntityTooLarge)
	}
	if int64(len(data)) != r.ContentLength {
		return fail(ErrIncompleteBody)
	}
	if r.Method != http.MethodDelete {
		if file == bucketPolicyConfig {
			if !validConditionalBucketPolicy(data) {
				return fail(ErrMalformedPolicy)
			}
		} else if !validConditionalBucketXML(data, file) {
			return fail(ErrMalformedXML)
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return true
}

// Decode a bounded JSON tree while rejecting duplicate keys, including keys
// inside conditions. encoding/json otherwise silently keeps the last value.
func decodeUniqueBucketJSON(d *json.Decoder, depth int) (interface{}, error) {
	if depth > 32 {
		return nil, errors.New("policy nesting exceeds limit")
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return token, nil
	}
	switch delim {
	case '{':
		result := make(map[string]interface{})
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("invalid policy key")
			}
			if _, duplicate := result[key]; duplicate {
				return nil, errors.New("duplicate policy key")
			}
			value, err := decodeUniqueBucketJSON(d, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		if end, err := d.Token(); err != nil || end != json.Delim('}') {
			return nil, errors.New("invalid policy object")
		}
		return result, nil
	case '[':
		result := make([]interface{}, 0)
		for d.More() {
			value, err := decodeUniqueBucketJSON(d, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		if end, err := d.Token(); err != nil || end != json.Delim(']') {
			return nil, errors.New("invalid policy array")
		}
		return result, nil
	default:
		return nil, errors.New("invalid policy delimiter")
	}
}

func bucketJSONKeys(object map[string]interface{}, allowed ...string) bool {
	for key := range object {
		if !contains(allowed, key) {
			return false
		}
	}
	return true
}

func validConditionalBucketPolicy(data []byte) bool {
	if !validConfigurationJSONEncoding(data) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	value, err := decodeUniqueBucketJSON(d, 0)
	if err != nil {
		return false
	}
	if _, err := d.Token(); err != io.EOF {
		return false
	}
	policy, ok := value.(map[string]interface{})
	if !ok || !bucketJSONKeys(policy, "ID", "Id", "Version", "Statement") {
		return false
	}
	if _, upper := policy["ID"]; upper {
		if _, standard := policy["Id"]; standard {
			return false
		}
	}
	statements, ok := policy["Statement"].([]interface{})
	if !ok {
		return false
	}
	for _, value := range statements {
		statement, ok := value.(map[string]interface{})
		if !ok || !bucketJSONKeys(statement, "Sid", "Effect", "Principal", "Action", "Resource", "Condition") {
			return false
		}
		if principal, object := statement["Principal"].(map[string]interface{}); object && !bucketJSONKeys(principal, "AWS") {
			return false
		}
		if value, present := statement["Condition"]; present {
			conditions, ok := value.(map[string]interface{})
			if !ok || len(conditions) == 0 {
				return false
			}
			for _, value := range conditions {
				arguments, ok := value.(map[string]interface{})
				if !ok || len(arguments) == 0 {
					return false
				}
			}
		}
	}
	return true
}

// Values indicate whether a child can repeat. A missing parent is a scalar;
// nested elements and attributes on scalar values must never be discarded.
var conditionalLifecycleXML = map[string]map[string]bool{
	"LifecycleConfiguration":       {"Rule": true},
	"BucketLifecycleConfiguration": {"Rule": true},
	"Rule": {"ID": false, "Status": false, "Filter": false, "Prefix": false, "Expiration": false,
		"Transition": false, "NoncurrentVersionExpiration": false, "NoncurrentVersionTransition": false},
	"Filter":                      {"Prefix": false, "Tag": false, "And": false},
	"And":                         {"Prefix": false, "Tag": true},
	"Tag":                         {"Key": false, "Value": false},
	"Expiration":                  {"Days": false, "Date": false, "ExpiredObjectDeleteMarker": false},
	"Transition":                  {"Days": false, "Date": false, "StorageClass": false},
	"NoncurrentVersionExpiration": {"NoncurrentDays": false},
	"NoncurrentVersionTransition": {"NoncurrentDays": false, "StorageClass": false},
}

func validConditionalBucketXML(data []byte, file string) bool {
	const namespace = "http://s3.amazonaws.com/doc/2006-03-01/"
	schema := conditionalLifecycleXML
	if file == bucketVersioningConfig {
		schema = map[string]map[string]bool{"VersioningConfiguration": {"Status": false}}
	}
	type frame struct {
		name         string
		seen         map[string]bool
		values       map[string]string
		text         []byte
		tagged       bool
		deleteMarker bool
	}
	var stack []frame
	rootSeen, rootClosed, declaration := false, false, false
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := d.Token()
		if err == io.EOF {
			return rootSeen && rootClosed && len(stack) == 0
		}
		if err != nil {
			return false
		}
		switch token := token.(type) {
		case xml.StartElement:
			if rootClosed || (token.Name.Space != "" && token.Name.Space != namespace) {
				return false
			}
			root := len(stack) == 0
			if root {
				if rootSeen || (file == bucketVersioningConfig && token.Name.Local != "VersioningConfiguration") ||
					(file == bucketLifecycleConfig && token.Name.Local != "LifecycleConfiguration" && token.Name.Local != "BucketLifecycleConfiguration") {
					return false
				}
				rootSeen = true
			} else {
				parent := &stack[len(stack)-1]
				repeat, allowed := schema[parent.name][token.Name.Local]
				if !allowed || (parent.seen[token.Name.Local] && !repeat) {
					return false
				}
				parent.seen[token.Name.Local] = true
			}
			for _, attr := range token.Attr {
				if !root || attr.Name.Space != "" || attr.Name.Local != "xmlns" || (attr.Value != "" && attr.Value != namespace) {
					return false
				}
			}
			if len(token.Attr) > 1 {
				return false
			}
			stack = append(stack, frame{name: token.Name.Local, seen: make(map[string]bool), values: make(map[string]string)})
		case xml.EndElement:
			if len(stack) == 0 {
				return false
			}
			completed := stack[len(stack)-1]
			switch completed.name {
			case "Filter":
				if len(completed.seen) > 1 {
					return false
				}
			case "And":
				if !completed.seen["Prefix"] || !completed.seen["Tag"] {
					return false
				}
			case "Tag":
				tag := lifecycle.Tag{Key: completed.values["Key"], Value: completed.values["Value"]}
				if tag.Validate() != nil {
					return false
				}
				completed.tagged = true
			case "Expiration", "Transition":
				if completed.seen["Days"] && completed.seen["Date"] {
					return false
				}
				completed.deleteMarker, _ = strconv.ParseBool(strings.TrimSpace(completed.values["ExpiredObjectDeleteMarker"]))
			case "Rule":
				// Delete markers have no object tags. Reject a new configuration
				// that promises a tag restriction on their expiration.
				if completed.tagged && completed.deleteMarker {
					return false
				}
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				rootClosed = true
			} else if schema[completed.name] == nil {
				stack[len(stack)-1].values[completed.name] = string(completed.text)
			}
			if len(stack) != 0 {
				parent := &stack[len(stack)-1]
				parent.tagged = parent.tagged || completed.tagged
				parent.deleteMarker = parent.deleteMarker || completed.deleteMarker
			}
		case xml.CharData:
			if (len(stack) == 0 || schema[stack[len(stack)-1].name] != nil) && strings.TrimSpace(string(token)) != "" {
				return false
			}
			if len(stack) != 0 && schema[stack[len(stack)-1].name] == nil {
				stack[len(stack)-1].text = append(stack[len(stack)-1].text, token...)
			}
		case xml.Directive:
			return false
		case xml.ProcInst:
			if token.Target != "xml" || rootSeen || declaration {
				return false
			}
			declaration = true
		}
	}
}
