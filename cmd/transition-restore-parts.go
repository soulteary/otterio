package cmd

import (
	"encoding/json"
	"strings"

	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
)

// Keep logical multipart boundaries separate from the physical parts of a raw
// restore. Encryption and compression readers use these original boundaries.
const transitionRestorePartsKey = ReservedMetadataPrefixLower + "transition-restore-parts"

func setTransitionRestoreParts(meta map[string]string, source *ObjectInfo) error {
	if source == nil || len(source.Parts) == 0 {
		return nil
	}
	if !validTransitionRestoreParts(source.Parts, source.Size) {
		return errInvalidArgument
	}
	data, err := json.Marshal(source.Parts)
	if err != nil {
		return err
	}
	meta[transitionRestorePartsKey] = string(data)
	return nil
}

func parseTransitionRestoreParts(meta map[string]string) []ObjectPartInfo {
	data := meta[transitionRestorePartsKey]
	if data == "" {
		return nil
	}
	var parts []ObjectPartInfo
	if json.Unmarshal([]byte(data), &parts) != nil {
		return nil
	}
	if !validTransitionRestoreParts(parts, -1) {
		return nil
	}
	return parts
}

func validTransitionRestoreParts(parts []ObjectPartInfo, expectedSize int64) bool {
	if len(parts) == 0 || len(parts) > globalMaxPartID {
		return false
	}
	var size int64
	previous := 0
	for _, part := range parts {
		if part.Number <= previous || part.Number > globalMaxPartID || part.Size < 0 || part.ActualSize < 0 || part.Size > int64(^uint64(0)>>1)-size {
			return false
		}
		size += part.Size
		previous = part.Number
	}
	return expectedSize < 0 || size == expectedSize
}

func sameTransitionSource(current ObjectInfo, expected *ObjectInfo) bool {
	if expected == nil {
		return true
	}
	version := func(v string) string {
		if v == nullVersionID {
			return ""
		}
		return v
	}
	return !current.DeleteMarker && version(current.VersionID) == version(expected.VersionID) &&
		current.ModTime.Equal(expected.ModTime) && current.ETag == expected.ETag && current.Size == expected.Size
}

func removeRestoreHeader(meta map[string]string) {
	for key := range meta {
		if strings.EqualFold(key, "x-amz-restore") {
			delete(meta, key)
		}
	}
}

func clearTransitionMetadata(meta map[string]string) {
	for key := range meta {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, ReservedMetadataPrefixLower+"transition-") || strings.HasPrefix(lower, ReservedMetadataPrefixLower+"restore-") {
			delete(meta, key)
		}
	}
	removeRestoreHeader(meta)
}

func transitionHasLocalData(fi FileInfo) bool {
	if fi.TransitionStatus != lifecycle.TransitionComplete {
		return true
	}
	ongoing, expiry, err := parseRestoreHeaderFromMeta(fi.Metadata)
	return err == nil && !ongoing && !expiry.IsZero() && UTCNow().Before(expiry)
}
