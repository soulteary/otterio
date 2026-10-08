// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package lifecycle

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	sdktags "github.com/soulteary/otterio-sdk/v7/pkg/tags"
)

func TestTaggedLifecycleActions(t *testing.T) {
	cases := []struct {
		name, action string
		want         Action
		noncurrent   bool
		marker       bool
	}{
		{"expiration-days", `<Expiration><Days>1</Days></Expiration>`, DeleteAction, false, false},
		{"expiration-date", `<Expiration><Date>2000-01-01T00:00:00Z</Date></Expiration>`, DeleteAction, false, false},
		{"transition-days", `<Transition><Days>1</Days><StorageClass>archive</StorageClass></Transition>`, TransitionAction, false, false},
		{"transition-date", `<Transition><Date>2000-01-01T00:00:00Z</Date><StorageClass>archive</StorageClass></Transition>`, TransitionAction, false, false},
		{"noncurrent-expiration", `<NoncurrentVersionExpiration><NoncurrentDays>1</NoncurrentDays></NoncurrentVersionExpiration>`, DeleteVersionAction, true, false},
		{"noncurrent-transition", `<NoncurrentVersionTransition><NoncurrentDays>1</NoncurrentDays><StorageClass>archive</StorageClass></NoncurrentVersionTransition>`, TransitionVersionAction, true, false},
		{"mixed-expiration-transition", `<Expiration><Days>1</Days></Expiration><Transition><Days>2</Days><StorageClass>archive</StorageClass></Transition>`, DeleteAction, false, false},
		{"mixed-noncurrent-actions", `<NoncurrentVersionExpiration><NoncurrentDays>1</NoncurrentDays></NoncurrentVersionExpiration><NoncurrentVersionTransition><NoncurrentDays>2</NoncurrentDays><StorageClass>archive</StorageClass></NoncurrentVersionTransition>`, DeleteVersionAction, true, false},
		{"legacy-delete-marker", `<Expiration><ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker></Expiration>`, DeleteVersionAction, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, filter := range []string{
				`<Tag><Key>environment</Key><Value>prod</Value></Tag>`,
				`<And><Prefix>logs/</Prefix><Tag><Key>environment</Key><Value>prod</Value></Tag><Tag><Key>class</Key><Value>archive</Value></Tag></And>`,
			} {
				doc := `<LifecycleConfiguration><Rule><ID>tagged</ID><Filter>` + filter + `</Filter><Status>Enabled</Status>` + tc.action + `</Rule></LifecycleConfiguration>`
				lc, err := ParseLifecycleConfig(strings.NewReader(doc))
				if err != nil || lc.Validate() != nil {
					t.Fatalf("invalid fixture: %v", err)
				}
				obj := ObjectOpts{Name: "logs/item", UserTags: "environment=prod&class=archive", ModTime: time.Now().Add(-30 * 24 * time.Hour), IsLatest: !tc.noncurrent, VersionID: "version", SuccessorModTime: time.Now().Add(-10 * 24 * time.Hour), DeleteMarker: tc.marker, NumVersions: 1}
				if got := lc.ComputeAction(obj); got != tc.want {
					t.Fatalf("matching tags action=%v want=%v", got, tc.want)
				}
				if got := len(lc.FilterActionableRules(obj)); got != 1 {
					t.Fatalf("one matching rule was selected %d times", got)
				}
				for _, tags := range []string{"", "environment=dev&class=archive", "environment=prod&class=other"} {
					if tags == "environment=prod&class=other" && !strings.Contains(filter, "<And>") {
						continue
					}
					obj.UserTags = tags
					if got := lc.ComputeAction(obj); got != NoneAction || len(lc.FilterActionableRules(obj)) != 0 {
						t.Fatalf("nonmatching tags selected action %v with tags %q", got, tags)
					}
				}
			}
			// Prefix-only configurations preserve the same implemented actions.
			doc := `<LifecycleConfiguration><Rule><Filter><Prefix>logs/</Prefix></Filter><Status>Enabled</Status>` + tc.action + `</Rule></LifecycleConfiguration>`
			lc, err := ParseLifecycleConfig(strings.NewReader(doc))
			if err != nil || lc.Validate() != nil {
				t.Fatalf("invalid prefix fixture: %v", err)
			}
			obj := ObjectOpts{Name: "logs/item", ModTime: time.Now().Add(-30 * 24 * time.Hour), IsLatest: !tc.noncurrent, VersionID: "version", SuccessorModTime: time.Now().Add(-10 * 24 * time.Hour), DeleteMarker: tc.marker, NumVersions: 1}
			if got := lc.ComputeAction(obj); got != tc.want || len(lc.FilterActionableRules(obj)) != 1 {
				t.Fatalf("prefix-only action=%v want=%v", got, tc.want)
			}
			obj.Name = "outside/item"
			if got := lc.ComputeAction(obj); got != NoneAction {
				t.Fatalf("nonmatching prefix selected action=%v", got)
			}
		})
	}
}

func TestURLencodedLifecycleTags(t *testing.T) {
	for _, tag := range []Tag{
		{Key: "environment name", Value: "production archive"},
		{Key: "environment+name", Value: "production+archive"},
		{Key: "environment%name", Value: "100% archive"},
		{Key: "环境", Value: "生产环境"},
		{Key: "empty value", Value: ""},
	} {
		if err := tag.Validate(); err != nil {
			t.Fatalf("invalid native tag fixture: %v", err)
		}
		encoded := url.Values{tag.Key: []string{tag.Value}}.Encode()
		// The pinned SDK permits ASCII/space/plus tags and URL-encodes them.
		// Native lifecycle also permits percent and Unicode; exercise the same
		// wire encoding for these values without its narrower tag validator.
		if sdk, err := sdktags.NewTags(map[string]string{tag.Key: tag.Value}, true); err == nil {
			encoded = sdk.String()
		}
		lc := Lifecycle{Rules: []Rule{{Status: Enabled, Filter: Filter{Tag: tag}, Expiration: Expiration{Days: 1}}}}
		obj := ObjectOpts{Name: "item", UserTags: encoded, ModTime: time.Now().Add(-72 * time.Hour), IsLatest: true}
		if !lc.Rules[0].Filter.TestTags(strings.Split(encoded, "&")) || lc.ComputeAction(obj) != DeleteAction {
			t.Errorf("encoded tag %q did not satisfy %q", encoded, tag.String())
		}
		obj.UserTags = "other=value"
		if lc.ComputeAction(obj) != NoneAction {
			t.Errorf("missing key %q selected an expiration", tag.Key)
		}
	}
	filter := Filter{Tag: Tag{Key: "environment", Value: "prod"}}
	for _, encoded := range []string{
		"environment=prod&environment=prod", "environment=prod&%65nvironment=prod",
		"environment=prod&other=one&other=two", "environment=prod&bad=%zz",
		"environment=prod&bad=%ff", "environment=prod&=value", "environment=prod&missing-value",
		"environment=prod;other=value", "environment=prod&invalid%ff=value",
	} {
		if filter.TestTags(strings.Split(encoded, "&")) {
			t.Errorf("malformed or duplicate tags %q broadened a match", encoded)
		}
	}
	if !filter.TestTags([]string{"environment=prod", "unrelated=value"}) {
		t.Fatal("valid unrelated tag prevented matching")
	}
}

func TestConcurrentLifecycleTagMatching(t *testing.T) {
	tags := make([]Tag, 3, 4) // Spare capacity must not become shared scratch space.
	for i := range tags {
		tags[i] = Tag{Key: fmt.Sprint("key-", i), Value: "value"}
	}
	filter := Filter{And: And{Tags: tags}}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if !filter.TestTags([]string{"key-0=value", "key-1=value", "key-2=value"}) || filter.TestTags([]string{"key-0=value"}) {
					t.Error("concurrent filter changed tag matching")
					return
				}
			}
		}()
	}
	wg.Wait()
	if tags[:cap(tags)][3].Key != "" || tags[:cap(tags)][3].Value != "" {
		t.Fatal("filter modified spare tag capacity")
	}
}

func TestDeleteMarkerActiveRules(t *testing.T) {
	lc, err := ParseLifecycleConfig(strings.NewReader(`<LifecycleConfiguration><Rule><Filter><Prefix>logs/</Prefix></Filter><Status>Enabled</Status><Expiration><ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker></Expiration></Rule></LifecycleConfiguration>`))
	if err != nil || lc.Validate() != nil {
		t.Fatalf("invalid fixture: %v", err)
	}
	for _, prefix := range []string{"", "logs/", "logs/item"} {
		if !lc.HasActiveRules(prefix, false) || !lc.HasActiveRules(prefix, true) {
			t.Errorf("delete-marker rule was skipped for prefix %q", prefix)
		}
	}
	if lc.HasActiveRules("outside/", false) || lc.HasActiveRules("outside/", true) {
		t.Fatal("delete-marker rule broadened its prefix")
	}
	if lc.ComputeAction(ObjectOpts{Name: "logs/item", DeleteMarker: true, NumVersions: 1, IsLatest: true, VersionID: "marker", ModTime: time.Now().Add(-72 * time.Hour)}) != DeleteVersionAction {
		t.Fatal("active marker rule did not remove its marker")
	}
	lc.Rules[0].Status = Disabled
	if lc.HasActiveRules("", true) {
		t.Fatal("disabled marker rule became active")
	}
}
