// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"github.com/valyala/fasthttp"
	"testing"
	"time"
)

func TestConsoleIAMRelationshipBindingsHeaderBodyLimit(t *testing.T) {
	for _, version := range []string{adminAPIVersionPrefix, adminAPIVersionV2Prefix} {
		var header fasthttp.RequestHeader
		header.SetMethod("PUT")
		header.SetRequestURI(adminPathPrefix + version + "/iam-relationship-bindings?kind=user&target=native-user")
		config := configurationRequestConfig(&header)
		if configurationRequestLimit(&header) != 32*1024 || config.MaxRequestBodySize != 32*1024 || config.ReadTimeout != 15*time.Second {
			t.Fatal("bindings write missing header limit")
		}
	}
}
