// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package lifecycle

import (
	"strings"
	"testing"
	"time"
)

func TestLifecycleDeletionPrecedesNoncurrentTransition(t *testing.T) {
	lc := Lifecycle{Rules: []Rule{
		{ID: "transition-first", Status: Enabled, NoncurrentVersionTransition: NoncurrentVersionTransition{NoncurrentDays: 1, StorageClass: "archive"}},
		{ID: "expire-second", Status: Enabled, NoncurrentVersionExpiration: NoncurrentVersionExpiration{NoncurrentDays: 2}},
	}}
	obj := ObjectOpts{Name: "item", ModTime: time.Now().Add(-30 * 24 * time.Hour), VersionID: "old-version", SuccessorModTime: time.Now().Add(-10 * 24 * time.Hour)}
	if got := lc.ComputeAction(obj); got != DeleteVersionAction {
		t.Fatalf("due expiration must win over an earlier transition rule: got %v", got)
	}
}

func selectionLifecycle(t *testing.T, rules string) Lifecycle {
	t.Helper()
	lc, err := ParseLifecycleConfig(strings.NewReader(`<LifecycleConfiguration>` + rules + `</LifecycleConfiguration>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := lc.Validate(); err != nil {
		t.Fatal(err)
	}
	return *lc
}

func TestLifecycleSelectionTargetAndDeadline(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	obj := ObjectOpts{Name: "logs/item", ModTime: now.Add(-30 * 24 * time.Hour), VersionID: "version", IsLatest: true, SuccessorModTime: now.Add(-10 * 24 * time.Hour)}
	for _, noncurrent := range []bool{false, true} {
		kind, days := "Transition", "Days"
		wantAction, base := TransitionAction, obj.ModTime
		if noncurrent {
			kind, days = "NoncurrentVersionTransition", "NoncurrentDays"
			wantAction, base = TransitionVersionAction, obj.SuccessorModTime
		}
		transitionRule := func(id, day, target string) string {
			return `<Rule><ID>` + id + `</ID><Status>Enabled</Status><Filter><Prefix></Prefix></Filter><` + kind + `><` + days + `>` + day + `</` + days + `><StorageClass>` + target + `</StorageClass></` + kind + `></Rule>`
		}
		obj.IsLatest = !noncurrent
		for _, tc := range []struct {
			name, rules, wantRule, wantTarget string
			wantDays                          int
		}{
			{"future-first", transitionRule("future", "100", "future-tier") + transitionRule("due", "1", "due-tier"), "due", "due-tier", 1},
			{"earliest-second", transitionRule("later", "5", "later-tier") + transitionRule("earlier", "1", "earlier-tier"), "earlier", "earlier-tier", 1},
			{"earliest-first", transitionRule("earlier", "1", "earlier-tier") + transitionRule("later", "5", "later-tier"), "earlier", "earlier-tier", 1},
			{"equal-first", transitionRule("first", "1", "first-tier") + transitionRule("second", "1", "second-tier"), "first", "first-tier", 1},
			{"equal-reversed", transitionRule("second", "1", "second-tier") + transitionRule("first", "1", "first-tier"), "second", "second-tier", 1},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				got := selectionLifecycle(t, tc.rules).SelectAt(obj, now)
				want := Selection{Action: wantAction, RuleID: tc.wantRule, StorageClass: tc.wantTarget, Due: ExpectedExpiryTime(base, tc.wantDays)}
				if got != want {
					t.Fatalf("selected action and destination must come from the same earliest due rule: got %+v want %+v", got, want)
				}
			})
		}
	}
}

func TestLifecycleSelectionNoncurrentIdentity(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	lc := selectionLifecycle(t, `<Rule><ID>mixed</ID><Status>Enabled</Status><Filter><Prefix></Prefix></Filter><Transition><Days>1</Days><StorageClass>current-tier</StorageClass></Transition><NoncurrentVersionTransition><NoncurrentDays>1</NoncurrentDays><StorageClass>old-tier</StorageClass></NoncurrentVersionTransition></Rule>`)
	base := ObjectOpts{Name: "item", ModTime: now.Add(-30 * 24 * time.Hour), VersionID: "old", SuccessorModTime: now.Add(-10 * 24 * time.Hour)}
	for _, tc := range []struct {
		name   string
		change func(*ObjectOpts)
		want   Selection
	}{
		{"noncurrent", func(*ObjectOpts) {}, Selection{Action: TransitionVersionAction, StorageClass: "old-tier", RuleID: "mixed", Due: ExpectedExpiryTime(base.SuccessorModTime, 1)}},
		{"current", func(o *ObjectOpts) { o.IsLatest = true }, Selection{Action: TransitionAction, StorageClass: "current-tier", RuleID: "mixed", Due: ExpectedExpiryTime(base.ModTime, 1)}},
		{"missing-version", func(o *ObjectOpts) { o.VersionID = "" }, Selection{Action: TransitionAction, StorageClass: "current-tier", RuleID: "mixed", Due: ExpectedExpiryTime(base.ModTime, 1)}},
		{"missing-successor", func(o *ObjectOpts) { o.SuccessorModTime = time.Time{} }, Selection{}},
		{"young-successor", func(o *ObjectOpts) { o.SuccessorModTime = now.Add(-time.Hour) }, Selection{}},
		{"future-successor", func(o *ObjectOpts) { o.SuccessorModTime = now.Add(time.Hour) }, Selection{}},
		{"old-successor-young-object", func(o *ObjectOpts) { o.ModTime = now.Add(-time.Hour) }, Selection{Action: TransitionVersionAction, StorageClass: "old-tier", RuleID: "mixed", Due: ExpectedExpiryTime(base.SuccessorModTime, 1)}},
		{"delete-marker", func(o *ObjectOpts) { o.DeleteMarker = true }, Selection{}},
		{"complete", func(o *ObjectOpts) { o.TransitionStatus = TransitionComplete }, Selection{}},
		{"missing-name", func(o *ObjectOpts) { o.Name = "" }, Selection{}},
		{"missing-modtime", func(o *ObjectOpts) { o.ModTime = time.Time{} }, Selection{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := base
			tc.change(&obj)
			if got := lc.SelectAt(obj, now); got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestLifecycleSelectionDeletionPriorityAndOrder(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, noncurrent := range []bool{false, true} {
		obj := ObjectOpts{Name: "item", ModTime: now.Add(-30 * 24 * time.Hour), VersionID: "version", IsLatest: !noncurrent, SuccessorModTime: now.Add(-10 * 24 * time.Hour)}
		transition := `<Rule><ID>transfer</ID><Status>Enabled</Status><Filter><Prefix></Prefix></Filter><Transition><Days>1</Days><StorageClass>tier</StorageClass></Transition></Rule>`
		expiration := `<Rule><ID>expire</ID><Status>Enabled</Status><Filter><Prefix></Prefix></Filter><Expiration><Days>2</Days></Expiration></Rule>`
		wantAction, base := DeleteAction, obj.ModTime
		if noncurrent {
			transition = `<Rule><ID>transfer</ID><Status>Enabled</Status><Filter><Prefix></Prefix></Filter><NoncurrentVersionTransition><NoncurrentDays>1</NoncurrentDays><StorageClass>tier</StorageClass></NoncurrentVersionTransition></Rule>`
			expiration = `<Rule><ID>expire</ID><Status>Enabled</Status><Filter><Prefix></Prefix></Filter><NoncurrentVersionExpiration><NoncurrentDays>2</NoncurrentDays></NoncurrentVersionExpiration></Rule>`
			wantAction, base = DeleteVersionAction, obj.SuccessorModTime
		}
		for _, rules := range []string{transition + expiration, expiration + transition} {
			want := Selection{Action: wantAction, RuleID: "expire", Due: ExpectedExpiryTime(base, 2)}
			if got := selectionLifecycle(t, rules).SelectAt(obj, now); got != want {
				t.Fatalf("noncurrent=%v: got %+v want %+v", noncurrent, got, want)
			}
			obj.TransitionStatus, obj.RestoreExpires = TransitionComplete, now.Add(-time.Hour)
			if got := selectionLifecycle(t, rules).SelectAt(obj, now); got != want {
				t.Fatalf("permanent expiration must win over restored-copy cleanup: got %+v want %+v", got, want)
			}
		}
	}
}

func TestLifecycleSelectionFiltersAndClockBoundary(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	lc := selectionLifecycle(t, `<Rule><ID>other-tag</ID><Status>Enabled</Status><Filter><Tag><Key>environment name</Key><Value>other</Value></Tag></Filter><Transition><Date>2000-01-01T00:00:00Z</Date><StorageClass>wrong-tier</StorageClass></Transition></Rule><Rule><ID>selected</ID><Status>Enabled</Status><Filter><And><Prefix>logs/</Prefix><Tag><Key>environment name</Key><Value>prod+archive</Value></Tag></And></Filter><Transition><Date>2026-10-08T00:00:00Z</Date><StorageClass>right-tier</StorageClass></Transition></Rule>`)
	obj := ObjectOpts{Name: "logs/item", UserTags: "environment+name=prod%2Barchive", ModTime: now.Add(-30 * 24 * time.Hour), IsLatest: true}
	if got := lc.SelectAt(obj, now.Add(-time.Nanosecond)); got != (Selection{}) {
		t.Fatalf("future or nonmatching rule selected: %+v", got)
	}
	want := Selection{Action: TransitionAction, StorageClass: "right-tier", RuleID: "selected", Due: now}
	if got := lc.SelectAt(obj, now); got != want {
		t.Fatalf("exact deadline must select matching rule: got %+v want %+v", got, want)
	}
	for _, change := range []func(*ObjectOpts){
		func(o *ObjectOpts) { o.Name = "outside/item" },
		func(o *ObjectOpts) { o.UserTags = "environment+name=prod%2Barchive&environment+name=prod%2Barchive" },
		func(o *ObjectOpts) { o.UserTags = "environment+name=prod%zzarchive" },
	} {
		changed := obj
		change(&changed)
		if got := lc.SelectAt(changed, now); got != (Selection{}) {
			t.Fatalf("invalid/nonmatching filter selected %+v", got)
		}
	}
	lc.Rules[1].Status = Disabled
	if got := lc.SelectAt(obj, now); got != (Selection{}) {
		t.Fatalf("disabled rule selected %+v", got)
	}
}

func TestLifecycleRestoreExpirySafetyAndDeadline(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	base := ObjectOpts{Name: "item", ModTime: now.Add(-30 * 24 * time.Hour), VersionID: "old", TransitionStatus: TransitionComplete, RestoreExpires: now}
	for _, tc := range []struct {
		name   string
		change func(*ObjectOpts)
		want   Action
	}{
		{"noncurrent", func(*ObjectOpts) {}, DeleteRestoredVersionAction},
		{"current", func(o *ObjectOpts) { o.IsLatest = true }, DeleteRestoredVersionAction},
		{"unversioned", func(o *ObjectOpts) { o.VersionID = "" }, DeleteRestoredAction},
		{"ongoing", func(o *ObjectOpts) { o.RestoreOngoing = true }, NoneAction},
		{"pending", func(o *ObjectOpts) { o.TransitionStatus = TransitionPending }, NoneAction},
		{"not-transitioned", func(o *ObjectOpts) { o.TransitionStatus = "" }, NoneAction},
		{"marker", func(o *ObjectOpts) { o.DeleteMarker = true }, NoneAction},
		{"future", func(o *ObjectOpts) { o.RestoreExpires = now.Add(time.Nanosecond) }, NoneAction},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := base
			tc.change(&obj)
			got := (Lifecycle{}).SelectAt(obj, now)
			if got.Action != tc.want || got.StorageClass != "" || got.RuleID != "" {
				t.Fatalf("got %+v want action %v", got, tc.want)
			}
			if tc.want != NoneAction && got.Due != now {
				t.Fatalf("restored-copy deadline changed: got %v want %v", got.Due, now)
			}
		})
	}
}

func TestLifecycleLongDelaysDoNotWrapIntoPast(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, days := range []int{36500, 106752, 1000000, int(^uint(0) >> 1)} {
		due := ExpectedExpiryTime(now, days)
		if !due.After(now) {
			t.Fatalf("positive %d-day delay wrapped into the past: %v", days, due)
		}
		if days <= 1000000 {
			want := now.UTC().AddDate(0, 0, days+1).Truncate(24 * time.Hour)
			if due != want {
				t.Fatalf("calendar deadline for %d days: got %v want %v", days, due, want)
			}
		}
		lc := Lifecycle{Rules: []Rule{{Status: Enabled, NoncurrentVersionTransition: NoncurrentVersionTransition{NoncurrentDays: ExpirationDays(days), StorageClass: "archive"}}}}
		obj := ObjectOpts{Name: "item", ModTime: now.Add(-time.Hour), VersionID: "old", SuccessorModTime: now.Add(-time.Hour)}
		if got := lc.SelectAt(obj, now); got != (Selection{}) {
			t.Fatalf("large positive delay caused premature transition: %+v", got)
		}
	}
}

func TestPredictExpiryMatchesSelectedVersionAndEarliestRule(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	lc := selectionLifecycle(t, `<Rule><ID>later</ID><Status>Enabled</Status><Filter><Prefix/></Filter><Expiration><Days>5</Days></Expiration><NoncurrentVersionExpiration><NoncurrentDays>5</NoncurrentDays></NoncurrentVersionExpiration></Rule><Rule><ID>earlier</ID><Status>Enabled</Status><Filter><Prefix/></Filter><Expiration><Days>1</Days></Expiration><NoncurrentVersionExpiration><NoncurrentDays>1</NoncurrentDays></NoncurrentVersionExpiration></Rule>`)
	base := ObjectOpts{Name: "item", VersionID: "old", ModTime: now.Add(-30 * 24 * time.Hour), SuccessorModTime: now.Add(-10 * 24 * time.Hour)}
	for _, latest := range []bool{false, true} {
		obj := base
		obj.IsLatest = latest
		selected := lc.SelectAt(obj, now)
		id, due := lc.PredictExpiryTime(obj)
		if id != selected.RuleID || due != selected.Due {
			t.Fatalf("predicted expiry differs from selected version rule: latest=%v id=%s due=%v selected=%+v", latest, id, due, selected)
		}
	}
	obj := base
	obj.SuccessorModTime = time.Time{}
	if id, due := lc.PredictExpiryTime(obj); id != "" || !due.IsZero() {
		t.Fatalf("missing successor produced an invented deadline: %s %v", id, due)
	}
	lc.Rules[0].NoncurrentVersionExpiration, lc.Rules[1].NoncurrentVersionExpiration = NoncurrentVersionExpiration{}, NoncurrentVersionExpiration{}
	if id, due := lc.PredictExpiryTime(base); id != "" || !due.IsZero() {
		t.Fatalf("current expiration applied to a noncurrent version: %s %v", id, due)
	}
}

func TestLifecycleRestoreExpiryWithoutRules(t *testing.T) {
	for _, versionID := range []string{"", "old-version"} {
		obj := ObjectOpts{Name: "item", ModTime: time.Now().Add(-30 * 24 * time.Hour), VersionID: versionID, TransitionStatus: TransitionComplete, RestoreExpires: time.Now().Add(-time.Hour)}
		want := DeleteRestoredAction
		if versionID != "" {
			want = DeleteRestoredVersionAction
		}
		if got := (Lifecycle{}).ComputeAction(obj); got != want {
			t.Errorf("restored version %q must expire without a lifecycle document: got %v want %v", versionID, got, want)
		}
	}
}
