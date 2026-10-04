/*
 * OtterIO Cloud Storage, (C) 2026 soulteary, https://github.com/soulteary/otterio
 * Licensed under the Apache License, Version 2.0.
 */

package cmd

import (
	"path/filepath"
	"strings"
)

// validStoragePath checks untrusted components BEFORE path.Join can erase them.
// Empty paths are allowed here: listing a volume root and Delete's root no-op
// are part of the storage contract. Volumes must additionally be non-empty.
// Nested volumes are intentional (system volumes and directory healing).
func validStoragePath(name string) bool {
	if strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." {
			return false
		}
		// Reject Windows drive-relative/absolute paths on every platform.
		if len(part) >= 2 && part[1] == ':' && ((part[0] >= 'a' && part[0] <= 'z') || (part[0] >= 'A' && part[0] <= 'Z')) {
			return false
		}
	}
	return true
}

// storagePathWithin uses path segments, not string prefixes (bucket-other is
// not inside bucket). This is a lexical fence, not a symlink/TOCTOU sandbox.
func storagePathWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validStorageFileInfoPaths(fi FileInfo) bool {
	return validStoragePath(fi.Name) && validStoragePath(fi.Volume) && validStoragePath(fi.DataDir)
}

// validStorageErasureInfo deliberately checks scalar arithmetic independently
// of distribution/index validation. Partial metadata updates/delete markers do
// not carry a full erasure layout and must not be run through this validator.
func validStorageErasureInfo(e ErasureInfo) bool {
	return isXLMetaErasureInfoValid(e.DataBlocks, e.ParityBlocks) && e.BlockSize > 0 && e.BlockSize <= blockSizeV1
}

func validStorageFileInfoData(fi FileInfo) bool {
	if fi.Deleted {
		return true
	}
	if fi.Size < 0 || !validStorageErasureInfo(fi.Erasure) || len(fi.Parts) > globalMaxPartID {
		return false
	}
	for _, part := range fi.Parts {
		if part.Number <= 0 || part.Number > globalMaxPartID || part.Size < 0 {
			return false
		}
	}
	return true
}

func validateStorageFileInfo(fi FileInfo, requireData bool) error {
	if !validStorageFileInfoPaths(fi) {
		return errFileAccessDenied
	}
	if requireData && !validStorageFileInfoData(fi) {
		return errFileCorrupt
	}
	return nil
}

func validateStorageVerifyInfo(fi FileInfo) error {
	if err := validateStorageFileInfo(fi, true); err != nil {
		return err
	}
	// A verification request must carry data metadata, not just a delete marker.
	if fi.Deleted {
		return errFileCorrupt
	}
	for _, part := range fi.Parts {
		if !fi.Erasure.GetChecksumInfo(part.Number).Algorithm.Available() {
			return errFileCorrupt
		}
	}
	return nil
}

func validateStorageVersions(versions []FileInfo) error {
	for _, fi := range versions {
		if err := validateStorageFileInfo(fi, false); err != nil {
			return err
		}
	}
	return nil
}

// The scanner accepts path-bearing cache entries from peers, not URL params.
func validStorageScannerCache(cache dataUsageCache) bool {
	if cache.Info.Name == "" || !validStoragePath(cache.Info.Name) {
		return false
	}
	for name, entry := range cache.Cache {
		if !validStoragePath(name) {
			return false
		}
		for child := range entry.Children {
			if !validStoragePath(child) {
				return false
			}
		}
	}
	return true
}
