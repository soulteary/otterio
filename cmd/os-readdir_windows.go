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
	"os"
)

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

	// Verify it's a directory.
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

	// Use ReadDir (Go std library) instead of raw FindNextFile,
	// because FindNextFile with a handle from os.Open can
	// cause access violations on some Windows versions.
	entries, err := f.ReadDir(-1)
	if err != nil {
		return osErrToFileErr(err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}

		var typ os.FileMode = 0 // regular file
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			// Reparse point is a symlink
			fi, err := os.Stat(pathJoin(dirPath, name))
			if err != nil {
				if osIsNotExist(err) || isSysErrPathNotFound(err) ||
					isSysErrTooManySymlinks(err) {
					continue
				}
				return err
			}

			if fi.IsDir() {
				// Ignore symlinked directories.
				continue
			}

			typ = fi.Mode()
		case entry.IsDir():
			typ = os.ModeDir
		}

		if e := filter(name, typ); e == errDoneForNow {
			return nil
		}
	}

	return nil
}

// Return N entries at the directory dirPath. If count is -1, return all entries
func readDirN(dirPath string, count int) (entries []string, err error) {
	f, err := os.Open(dirPath)
	if err != nil {
		return nil, osErrToFileErr(err)
	}
	defer f.Close()

	// Verify it's a directory.
	fi, err := f.Stat()
	if err != nil {
		return nil, osErrToFileErr(err)
	}
	if !fi.IsDir() {
		return nil, errFileNotFound
	}

	// Use f.ReadDir (Go std library) which handles Windows
	// directories reliably, unlike raw FindNextFile with
	// os.Open handles which can cause access violations
	// on some Windows versions.
	dirEntries, err := f.ReadDir(count)
	if err != nil {
		return nil, osErrToFileErr(err)
	}

	entries = make([]string, 0, len(dirEntries))
	for _, entry := range dirEntries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}

		switch {
		case entry.Type()&os.ModeSymlink != 0:
			// Reparse point is a symlink
			fi, err := os.Stat(pathJoin(dirPath, name))
			if err != nil {
				if osIsNotExist(err) || isSysErrPathNotFound(err) ||
					isSysErrTooManySymlinks(err) {
					continue
				}
				return nil, err
			}

			if fi.IsDir() {
				// directory symlinks are ignored.
				continue
			}
		case entry.IsDir():
			name = name + SlashSeparator
		}

		entries = append(entries, name)
	}

	return entries, nil
}

func globalSync() {
	// no-op on windows
}
