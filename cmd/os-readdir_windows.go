//go:build windows
// +build windows

/*
 * MinIO Cloud Storage, (C) 2016-2020 MinIO, Inc.
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

package cmd

import (
	"io"
	"os"
	"syscall"
)

const windowsReadDirBatchSize = 128

func access(name string) error {
	_, err := os.Lstat(name)
	return err
}

// Return all the entries at the directory dirPath.
func readDir(dirPath string) (entries []string, err error) {
	return readDirN(dirPath, -1)
}

// readDirFn applies the fn() function on each entries at dirPath, doesn't recurse into
// the directory itself, if the dirPath doesn't exist this function doesn't return
// an error.
func readDirFn(dirPath string, filter func(name string, typ os.FileMode) error) error {
	f, err := os.Open(dirPath)
	if err != nil {
		if osErrToFileErr(err) == errFileNotFound {
			return nil
		}
		return osErrToFileErr(err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		if osErrToFileErr(err) == errFileNotFound {
			return nil
		}
		return osErrToFileErr(err)
	}
	if !fi.IsDir() {
		return errFileNotFound
	}

	err = readDirEntriesWindows(dirPath, f, -1, filter)
	if osErrToFileErr(err) == errFileNotFound {
		// The directory may disappear while the scanner is reading it.
		return nil
	}
	return err
}

// Return N entries at the directory dirPath. If count is -1, return all entries
func readDirN(dirPath string, count int) (entries []string, err error) {
	f, err := os.Open(dirPath)
	if err != nil {
		return nil, osErrToFileErr(err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, osErrToFileErr(err)
	}
	if !fi.IsDir() {
		return nil, errFileNotFound
	}

	err = readDirEntriesWindows(dirPath, f, count, func(name string, typ os.FileMode) error {
		if typ.IsDir() {
			name += SlashSeparator
		}
		entries = append(entries, name)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// readDirEntriesWindows uses ReadDir's directory handle rather than passing an
// os.Open handle to FindNextFile, which requires a FindFirstFile search handle.
// Batches bound memory use and allow callbacks to stop without reading the whole
// directory. Count applies to accepted entries, after resolving reparse points.
func readDirEntriesWindows(dirPath string, reader interface {
	ReadDir(int) ([]os.DirEntry, error)
}, count int, filter func(name string, typ os.FileMode) error) error {
	for count != 0 {
		batchSize := windowsReadDirBatchSize
		if count > 0 && count < batchSize {
			batchSize = count
		}
		entries, readErr := reader.ReadDir(batchSize)
		if readErr != nil && readErr != io.EOF {
			return osErrToFileErr(readErr)
		}

		for _, entry := range entries {
			name := entry.Name()
			if name == "" || name == "." || name == ".." {
				continue
			}
			typ, skip, err := windowsDirEntryType(dirPath, entry)
			if err != nil {
				return err
			}
			if skip {
				continue
			}
			if filter(name, typ) == errDoneForNow {
				return nil
			}
			if count > 0 {
				count--
				if count == 0 {
					return nil
				}
			}
		}
		// ReadDir returns EOF for an empty directory when its limit is positive.
		// Process any entries first, then report normal exhaustion as success.
		if readErr == io.EOF {
			return nil
		}
	}
	return nil
}

func windowsDirEntryType(dirPath string, entry os.DirEntry) (typ os.FileMode, skip bool, err error) {
	// Windows ReadDir caches this FileInfo. Inspect the attributes to recognize
	// every reparse point: junctions are ModeIrregular, not ModeSymlink, in Go 1.23+.
	fi, err := entry.Info()
	if err != nil {
		return 0, false, err
	}
	attrs, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if ok && attrs.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		fi, err = os.Stat(pathJoin(dirPath, entry.Name()))
		if err != nil {
			if osIsNotExist(err) || isSysErrPathNotFound(err) || isSysErrTooManySymlinks(err) {
				return 0, true, nil
			}
			return 0, false, err
		}
		if fi.IsDir() {
			// Ignore directory symlinks and junctions, preserving the storage policy.
			return 0, true, nil
		}
		return fi.Mode(), false, nil
	}
	if entry.IsDir() {
		return os.ModeDir, false, nil
	}
	return 0, false, nil
}

func globalSync() {
	// no-op on windows
}
