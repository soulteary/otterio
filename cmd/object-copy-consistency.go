// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"maps"
	"slices"
)

// sameCopySource checks the source snapshot used to authorize and prepare a
// self-copy. Metadata and SSE-C key rotations can change without changing the
// source's ETag, size or modification time, so a raw snapshot must include them.
// Legacy internal callers provide already-edited copy metadata and can only
// supply the original data identity. Those fallbacks are explicitly marked.
func sameCopySource(current ObjectInfo, expected *ObjectInfo, identityOnly bool) bool {
	if !sameTransitionSource(current, expected) {
		return false
	}
	if expected == nil || identityOnly {
		return true
	}
	// StorageClass can be derived from lifecycle configuration for legacy
	// objects; compare its persisted metadata and transition reference instead.
	if current.IsDir != expected.IsDir || current.UserTags != expected.UserTags ||
		!current.Expires.Equal(expected.Expires) ||
		current.TransitionStatus != expected.TransitionStatus || current.VersionPurgeStatus != expected.VersionPurgeStatus ||
		!maps.Equal(current.UserDefined, expected.UserDefined) || !slices.Equal(current.Parts, expected.Parts) {
		return false
	}
	if current.TransitionedObject == nil || expected.TransitionedObject == nil {
		return current.TransitionedObject == nil && expected.TransitionedObject == nil
	}
	return *current.TransitionedObject == *expected.TransitionedObject
}
