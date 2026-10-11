// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"github.com/valyala/fasthttp"
	"testing"
	"time"
)

func TestConsoleIAMRelationshipSecretHeaderBodyLimit(t *testing.T) {
	for _, route := range []string{"self-credentials", "iam-relationship-user-secret"} {
		var header fasthttp.RequestHeader
		header.SetMethod("PUT")
		header.SetRequestURI(adminPathPrefix + adminAPIVersionPrefix + "/" + route + "?kind=user&target=native-target")
		config := configurationRequestConfig(&header)
		if configurationRequestLimit(&header) != 4096 || config.MaxRequestBodySize != 4096 || config.ReadTimeout != 15*time.Second {
			t.Fatal("secret body was not bounded at header receipt")
		}
	}
	var header fasthttp.RequestHeader
	header.SetMethod("PUT")
	header.SetRequestURI("/bucket/object")
	if configurationRequestLimit(&header) != 0 {
		t.Fatal("object upload inherited secret body cap")
	}
}

func TestConsoleIAMRelationshipGroupHeaderBodyLimit(t *testing.T) {
	for _, version := range []string{adminAPIVersionPrefix, adminAPIVersionV2Prefix} {
		var header fasthttp.RequestHeader
		header.SetMethod("PUT")
		header.SetRequestURI(adminPathPrefix + version + "/iam-relationship-groups?kind=group&target=native-group")
		config := configurationRequestConfig(&header)
		if configurationRequestLimit(&header) != 32*1024 || config.MaxRequestBodySize != 32*1024 || config.ReadTimeout != 15*time.Second {
			t.Fatal("group mutation missing header read limit")
		}
	}
}
