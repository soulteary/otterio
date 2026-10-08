// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import "testing"

func TestBucketConfigJSONEncoding(t *testing.T) {
	for _, value := range []string{`\ud800`, `\udc00`, `\ud800x`, `\ud800\u0041`, `\ud800\ud800`, "\xff"} {
		doc := []byte(`{"newSecretKey":"secret-` + value + `"}`)
		if validConfigurationJSONEncoding(doc) {
			t.Errorf("accepted a substituted JSON string %q", doc)
		}
		if _, err := decodeSelfCredentialSecret(doc); err == nil {
			t.Errorf("accepted a substituted secret %q", doc)
		}
	}
	for _, tc := range []struct{ wire, decoded string }{
		{`\ud83d\ude00`, "😀"},
		{`\ufffd`, "�"},
		{"�", "�"},
		{`\\ud800`, `\ud800`},
		{`\\udc00`, `\udc00`},
	} {
		doc := []byte(`{"newSecretKey":"secret-` + tc.wire + `"}`)
		if !validConfigurationJSONEncoding(doc) {
			t.Errorf("rejected a valid JSON string %q", doc)
		}
		secret, err := decodeSelfCredentialSecret(doc)
		if err != nil || secret != "secret-"+tc.decoded {
			t.Errorf("changed valid secret %q into %q: %v", doc, secret, err)
		}
	}
}
