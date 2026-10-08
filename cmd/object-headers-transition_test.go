// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"net/http/httptest"
	"testing"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
)

func TestTransitionStorageClassHeaderSurvivesRuleRemoval(t *testing.T) {
	previousLifecycle, previousMetadata := globalLifecycleSys, globalBucketMetadataSys
	globalLifecycleSys, globalBucketMetadataSys = NewLifecycleSys(), NewBucketMetadataSys()
	t.Cleanup(func() { globalLifecycleSys, globalBucketMetadataSys = previousLifecycle, previousMetadata })
	oi := ObjectInfo{Bucket: "removed-lifecycle", Name: "old-version", VersionID: "specific-version", Size: 21,
		TransitionStatus: lifecycle.TransitionComplete, StorageClass: "ARCHIVE"}
	w := httptest.NewRecorder()
	if err := setObjectHeaders(w, oi, nil, ObjectOptions{VersionID: oi.VersionID}); err != nil {
		t.Fatal(err)
	}
	if got := w.Header().Get(xhttp.AmzStorageClass); got != "ARCHIVE" {
		t.Fatalf("persisted tier header depends on removed lifecycle: got %q", got)
	}
	if got := w.Header()[xhttp.AmzVersionID]; len(got) != 1 || got[0] != oi.VersionID || w.Header().Get(xhttp.ContentLength) != "21" {
		t.Fatal("exact-version headers changed")
	}
}
