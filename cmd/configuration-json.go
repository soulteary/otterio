// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"strconv"
	"unicode/utf8"
)

// Go's JSON decoder replaces invalid UTF-8 and unpaired UTF-16 escapes with
// U+FFFD. Signed configuration documents and secrets must not silently change
// during decoding. Explicit U+FFFD and correctly paired escapes are valid.
func validConfigurationJSONEncoding(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	quoted := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		code, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		switch {
		case code >= 0xd800 && code <= 0xdbff:
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		case code >= 0xdc00 && code <= 0xdfff:
			return false
		}
	}
	return !quoted
}
