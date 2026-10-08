/*
 * MinIO Cloud Storage, (C) 2019 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
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

package lifecycle

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	errLifecycleTooManyRules = Errorf("Lifecycle configuration allows a maximum of 1000 rules")
	errLifecycleNoRule       = Errorf("Lifecycle configuration should have at least one rule")
	errLifecycleDuplicateID  = Errorf("Lifecycle configuration has rule with the same ID. Rule ID must be unique.")
	errXMLNotWellFormed      = Errorf("The XML you provided was not well-formed or did not validate against our published schema")
)

const (
	// TransitionComplete marks completed transition
	TransitionComplete = "complete"
	// TransitionPending - transition is yet to be attempted
	TransitionPending = "pending"
)

// Action represents a delete action or other transition
// actions that will be implemented later.
type Action int

//go:generate stringer -type Action $GOFILE

const (
	// NoneAction means no action required after evaluating lifecycle rules
	NoneAction Action = iota
	// DeleteAction means the object needs to be removed after evaluating lifecycle rules
	DeleteAction
	// DeleteVersionAction deletes a particular version
	DeleteVersionAction
	// TransitionAction transitions a particular object after evaluating lifecycle transition rules
	TransitionAction
	//TransitionVersionAction transitions a particular object version after evaluating lifecycle transition rules
	TransitionVersionAction
	// DeleteRestoredAction means the temporarily restored object needs to be removed after evaluating lifecycle rules
	DeleteRestoredAction
	// DeleteRestoredVersionAction deletes a particular version that was temporarily restored
	DeleteRestoredVersionAction
)

// Lifecycle - Configuration for bucket lifecycle.
type Lifecycle struct {
	XMLName xml.Name `xml:"LifecycleConfiguration"`
	Rules   []Rule   `xml:"Rule"`
}

// UnmarshalXML - decodes XML data.
func (lc *Lifecycle) UnmarshalXML(d *xml.Decoder, start xml.StartElement) (err error) {
	switch start.Name.Local {
	case "LifecycleConfiguration", "BucketLifecycleConfiguration":
	default:
		return xml.UnmarshalError(fmt.Sprintf("expected element type <LifecycleConfiguration>/<BucketLifecycleConfiguration> but have <%s>",
			start.Name.Local))
	}
	for {
		// Read tokens from the XML document in a stream.
		t, err := d.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		switch se := t.(type) {
		case xml.StartElement:
			switch se.Name.Local {
			case "Rule":
				var r Rule
				if err = d.DecodeElement(&r, &se); err != nil {
					return err
				}
				lc.Rules = append(lc.Rules, r)
			default:
				return xml.UnmarshalError(fmt.Sprintf("expected element type <Rule> but have <%s>", se.Name.Local))
			}
		}
	}
	return nil
}

// HasActiveRules - returns whether policy has active rules for.
// Optionally a prefix can be supplied.
// If recursive is specified the function will also return true if any level below the
// prefix has active rules. If no prefix is specified recursive is effectively true.
func (lc Lifecycle) HasActiveRules(prefix string, recursive bool) bool {
	if len(lc.Rules) == 0 {
		return false
	}
	for _, rule := range lc.Rules {
		if rule.Status == Disabled {
			continue
		}

		if len(prefix) > 0 && len(rule.GetPrefix()) > 0 {
			if !recursive {
				// If not recursive, incoming prefix must be in rule prefix
				if !strings.HasPrefix(prefix, rule.GetPrefix()) {
					continue
				}
			}
			if recursive {
				// If recursive, we can skip this rule if it doesn't match the tested prefix.
				if !strings.HasPrefix(prefix, rule.GetPrefix()) && !strings.HasPrefix(rule.GetPrefix(), prefix) {
					continue
				}
			}
		}

		if rule.NoncurrentVersionExpiration.NoncurrentDays > 0 {
			return true
		}
		if rule.NoncurrentVersionTransition.NoncurrentDays > 0 {
			return true
		}
		if rule.Expiration.DeleteMarker.val {
			return true
		}
		if rule.Expiration.IsNull() && rule.Transition.IsNull() {
			continue
		}
		if !rule.Expiration.IsDateNull() && rule.Expiration.Date.Before(time.Now()) {
			return true
		}
		if !rule.Transition.IsDateNull() && rule.Transition.Date.Before(time.Now()) {
			return true
		}
		if !rule.Expiration.IsDaysNull() || !rule.Transition.IsDaysNull() {
			return true
		}
	}
	return false
}

// ParseLifecycleConfig - parses data in given reader to Lifecycle.
func ParseLifecycleConfig(reader io.Reader) (*Lifecycle, error) {
	var lc Lifecycle
	if err := xml.NewDecoder(reader).Decode(&lc); err != nil {
		return nil, err
	}
	return &lc, nil
}

// Validate - validates the lifecycle configuration
func (lc Lifecycle) Validate() error {
	// Lifecycle config can't have more than 1000 rules
	if len(lc.Rules) > 1000 {
		return errLifecycleTooManyRules
	}
	// Lifecycle config should have at least one rule
	if len(lc.Rules) == 0 {
		return errLifecycleNoRule
	}
	// Validate all the rules in the lifecycle config
	for _, r := range lc.Rules {
		if err := r.Validate(); err != nil {
			return err
		}
	}
	// Make sure Rule ID is unique
	for i := range lc.Rules {
		if i == len(lc.Rules)-1 {
			break
		}
		otherRules := lc.Rules[i+1:]
		for _, otherRule := range otherRules {
			if lc.Rules[i].ID == otherRule.ID {
				return errLifecycleDuplicateID
			}
		}
	}
	return nil
}

// FilterActionableRules returns the rules actions that need to be executed
// after evaluating prefix/tag filtering
func (lc Lifecycle) FilterActionableRules(obj ObjectOpts) []Rule {
	if obj.Name == "" {
		return nil
	}
	var rules []Rule
	for _, rule := range lc.Rules {
		if rule.Status == Disabled {
			continue
		}
		if !strings.HasPrefix(obj.Name, rule.GetPrefix()) {
			continue
		}
		// Every action in a rule shares its filter. Selecting transitions or
		// noncurrent actions before checking tags can also expose an expiration
		// in that same rule to objects outside the intended tag scope.
		if !rule.Filter.TestTags(strings.Split(obj.UserTags, "&")) {
			continue
		}
		rules = append(rules, rule)
	}
	return rules
}

// ObjectOpts provides information to deduce the lifecycle actions
// which can be triggered on the resultant object.
type ObjectOpts struct {
	Name             string
	UserTags         string
	ModTime          time.Time
	VersionID        string
	IsLatest         bool
	DeleteMarker     bool
	NumVersions      int
	SuccessorModTime time.Time
	TransitionStatus string
	RestoreOngoing   bool
	RestoreExpires   time.Time
}

// ExpiredObjectDeleteMarker returns true if an object version referred to by o
// is the only version remaining and is a delete marker. It returns false
// otherwise.
func (o ObjectOpts) ExpiredObjectDeleteMarker() bool {
	return o.DeleteMarker && o.NumVersions == 1
}

// Selection binds a lifecycle action to the rule, deadline and transition target
// that selected it. Callers must not independently choose a matching target.
type Selection struct {
	Action       Action
	StorageClass string
	RuleID       string
	Due          time.Time
}

// Select evaluates the object using a single clock reading.
func (lc Lifecycle) Select(obj ObjectOpts) Selection {
	return lc.SelectAt(obj, time.Now())
}

// SelectAt evaluates all matching rules at now. Expiration takes precedence over
// restoring local space, which takes precedence over transition. Within each
// class, the earliest deadline wins; equal deadlines preserve document order.
// Expiring a restored copy does not depend on the lifecycle document that first
// transitioned it, and applies to current and noncurrent data versions alike.
func (lc Lifecycle) SelectAt(obj ObjectOpts, now time.Time) Selection {
	var selected Selection
	if obj.Name == "" {
		return selected
	}

	priority := func(action Action) int {
		switch action {
		case DeleteAction, DeleteVersionAction:
			return 3
		case DeleteRestoredAction, DeleteRestoredVersionAction:
			return 2
		case TransitionAction, TransitionVersionAction:
			return 1
		}
		return 0
	}
	consider := func(candidate Selection) {
		if priority(candidate.Action) > priority(selected.Action) ||
			priority(candidate.Action) == priority(selected.Action) && candidate.Due.Before(selected.Due) {
			selected = candidate
		}
	}
	considerDue := func(action Action, due time.Time, rule Rule, storageClass string) {
		if !due.IsZero() && !now.Before(due) {
			consider(Selection{Action: action, StorageClass: storageClass, RuleID: rule.ID, Due: due})
		}
	}

	if !obj.DeleteMarker && obj.TransitionStatus == TransitionComplete && !obj.RestoreOngoing &&
		!obj.RestoreExpires.IsZero() && !now.Before(obj.RestoreExpires) {
		action := DeleteRestoredAction
		if obj.VersionID != "" {
			action = DeleteRestoredVersionAction
		}
		consider(Selection{Action: action, Due: obj.RestoreExpires})
	}
	if obj.ModTime.IsZero() {
		return selected
	}

	for _, rule := range lc.FilterActionableRules(obj) {
		if obj.ExpiredObjectDeleteMarker() && rule.Expiration.DeleteMarker.val {
			consider(Selection{Action: DeleteVersionAction, RuleID: rule.ID, Due: obj.ModTime})
		}
		if !rule.NoncurrentVersionExpiration.IsDaysNull() {
			if obj.VersionID != "" && !obj.IsLatest && !obj.SuccessorModTime.IsZero() {
				considerDue(DeleteVersionAction, ExpectedExpiryTime(obj.SuccessorModTime, int(rule.NoncurrentVersionExpiration.NoncurrentDays)), rule, "")
			}
			// Once a marker is the only remaining version, its own age determines
			// expiration; it no longer has a successor timestamp.
			if obj.VersionID != "" && obj.ExpiredObjectDeleteMarker() {
				considerDue(DeleteVersionAction, ExpectedExpiryTime(obj.ModTime, int(rule.NoncurrentVersionExpiration.NoncurrentDays)), rule, "")
			}
		}
		if !rule.NoncurrentVersionTransition.IsDaysNull() && rule.NoncurrentVersionTransition.StorageClass != "" &&
			obj.VersionID != "" && !obj.IsLatest && !obj.SuccessorModTime.IsZero() &&
			!obj.DeleteMarker && obj.TransitionStatus != TransitionComplete {
			considerDue(TransitionVersionAction, ExpectedExpiryTime(obj.SuccessorModTime, int(rule.NoncurrentVersionTransition.NoncurrentDays)), rule, rule.NoncurrentVersionTransition.StorageClass)
		}

		// Expiration of a current data version may create a delete marker. A
		// marker itself must only use the marker/noncurrent rules above.
		if !obj.DeleteMarker && (obj.VersionID == "" || obj.IsLatest) {
			switch {
			case !rule.Expiration.IsDateNull():
				considerDue(DeleteAction, rule.Expiration.Date.Time, rule, "")
			case !rule.Expiration.IsDaysNull():
				considerDue(DeleteAction, ExpectedExpiryTime(obj.ModTime, int(rule.Expiration.Days)), rule, "")
			}
			if obj.TransitionStatus != TransitionComplete && rule.Transition.StorageClass != "" {
				switch {
				case !rule.Transition.IsDateNull():
					considerDue(TransitionAction, rule.Transition.Date.Time, rule, rule.Transition.StorageClass)
				case !rule.Transition.IsDaysNull():
					considerDue(TransitionAction, ExpectedExpiryTime(obj.ModTime, int(rule.Transition.Days)), rule, rule.Transition.StorageClass)
				}
			}
		}
	}
	return selected
}

// ComputeAction is the action-only compatibility form of Select.
func (lc Lifecycle) ComputeAction(obj ObjectOpts) Action {
	return lc.Select(obj).Action
}

// ExpectedExpiryTime calculates the expiry, transition or restore date/time based on a object modtime.
// The expected transition or restore time is always a midnight time following the the object
// modification time plus the number of transition/restore days.
//
//	e.g. If the object modtime is `Thu May 21 13:42:50 GMT 2020` and the object should
//	    transition in 1 day, then the expected transition time is `Fri, 23 May 2020 00:00:00 GMT`
func ExpectedExpiryTime(modTime time.Time, days int) time.Time {
	t := modTime.UTC().Truncate(24 * time.Hour)
	// Duration multiplication wraps for delays above about 292 years. Treat
	// delays beyond the last RFC3339 year as a distant deadline instead of
	// accidentally expiring an object in the past (including max-int days).
	last := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	if days >= 0 && t.Before(last) && int64(days) >= (last.Unix()-t.Unix())/86400 {
		return last
	}
	if days >= 0 && !t.Before(last) {
		return t
	}
	return t.AddDate(0, 0, days+1)
}

// PredictExpiryTime returns the expiry date/time of a given object
// after evaluating the current lifecycle document.
func (lc Lifecycle) PredictExpiryTime(obj ObjectOpts) (string, time.Time) {
	if obj.DeleteMarker || obj.Name == "" || obj.ModTime.IsZero() {
		// We don't need to send any x-amz-expiration for delete marker.
		return "", time.Time{}
	}

	var finalExpiryDate time.Time
	var finalExpiryRuleID string
	noncurrent := obj.VersionID != "" && !obj.IsLatest
	if noncurrent && obj.SuccessorModTime.IsZero() {
		return "", time.Time{}
	}
	consider := func(ruleID string, due time.Time) {
		if finalExpiryDate.IsZero() || due.Before(finalExpiryDate) {
			finalExpiryRuleID, finalExpiryDate = ruleID, due
		}
	}

	// Iterate over all actionable rules and find the earliest
	// expiration date and its associated rule ID.
	for _, rule := range lc.FilterActionableRules(obj) {
		if noncurrent {
			if !rule.NoncurrentVersionExpiration.IsDaysNull() {
				consider(rule.ID, ExpectedExpiryTime(obj.SuccessorModTime, int(rule.NoncurrentVersionExpiration.NoncurrentDays)))
			}
			continue
		}

		if !rule.Expiration.IsDateNull() {
			consider(rule.ID, rule.Expiration.Date.Time)
		}
		if !rule.Expiration.IsDaysNull() {
			consider(rule.ID, ExpectedExpiryTime(obj.ModTime, int(rule.Expiration.Days)))
		}
	}
	return finalExpiryRuleID, finalExpiryDate
}
