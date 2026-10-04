//go:build windows

/*
 * Copyright (C) 2026 soulteary, https://github.com/soulteary/otterio
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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

// Inject enumeration order so count/filter tests do not depend on NTFS ordering.
type windowsTestDirReader struct {
	entries  []os.DirEntry
	finalErr error
	limits   []int
}

func (r *windowsTestDirReader) ReadDir(n int) ([]os.DirEntry, error) {
	r.limits = append(r.limits, n)
	if n <= 0 || n > windowsReadDirBatchSize {
		return nil, errors.New("directory reads must use bounded positive batches")
	}
	if len(r.entries) == 0 {
		return nil, io.EOF
	}
	if n > len(r.entries) {
		n = len(r.entries)
	}
	entries := r.entries[:n]
	r.entries = r.entries[n:]
	if len(r.entries) == 0 {
		return entries, r.finalErr
	}
	return entries, nil
}

type windowsTestDirEntry struct {
	name  string
	typ   os.FileMode
	attrs uint32
}

func (e windowsTestDirEntry) Name() string               { return e.name }
func (e windowsTestDirEntry) Type() os.FileMode          { return e.typ }
func (e windowsTestDirEntry) IsDir() bool                { return e.typ.IsDir() }
func (e windowsTestDirEntry) Info() (os.FileInfo, error) { return windowsTestFileInfo{e}, nil }

type windowsTestFileInfo struct{ windowsTestDirEntry }

func (windowsTestFileInfo) Size() int64         { return 0 }
func (e windowsTestFileInfo) Mode() os.FileMode { return e.typ }
func (windowsTestFileInfo) ModTime() time.Time  { return time.Time{} }
func (e windowsTestFileInfo) Sys() interface{} {
	return &syscall.Win32FileAttributeData{FileAttributes: e.attrs}
}

func TestWindowsReadDirEntriesCounts(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "ignored-directory"), 0755); err != nil {
		t.Fatal(err)
	}
	// Real Stat calls resolve both surrogate entries to a directory. The fake
	// metadata distinguishes symlinks from the ModeIrregular used by junctions.
	entries := []os.DirEntry{
		windowsTestDirEntry{"ignored-directory", os.ModeSymlink, syscall.FILE_ATTRIBUTE_REPARSE_POINT},
		windowsTestDirEntry{"ignored-directory", os.ModeIrregular, syscall.FILE_ATTRIBUTE_REPARSE_POINT},
		windowsTestDirEntry{"missing-target", os.ModeSymlink, syscall.FILE_ATTRIBUTE_REPARSE_POINT},
		windowsTestDirEntry{"object-one", 0, 0},
		windowsTestDirEntry{"object-two", 0, 0},
	}
	for _, tc := range []struct {
		name  string
		count int
		want  int
	}{
		{"zero", 0, 0},
		{"one-after-filtering", 1, 1},
		{"two-after-filtering", 2, 2},
		{"more-than-available", 3, 2},
		{"all", -1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &windowsTestDirReader{entries: entries}
			var names []string
			err := readDirEntriesWindows(dir, reader, tc.count, func(name string, _ os.FileMode) error {
				names = append(names, name)
				return nil
			})
			if err != nil || len(names) != tc.want {
				t.Fatalf("got entries=%v, error=%v; want %d entries and nil error", names, err, tc.want)
			}
			if tc.count == 0 && len(reader.limits) != 0 {
				t.Fatal("a zero limit must not enumerate entries")
			}
			for i, name := range names {
				if name != fmt.Sprintf("object-%s", []string{"one", "two"}[i]) {
					t.Fatalf("unexpected entry %q", name)
				}
			}
		})
	}
}

func TestWindowsReadDirEntriesEOF(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []os.DirEntry
	}{
		{"empty", nil},
		{"entries-with-EOF", []os.DirEntry{windowsTestDirEntry{"object", 0, 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &windowsTestDirReader{entries: tc.entries, finalErr: io.EOF}
			calls := 0
			err := readDirEntriesWindows(t.TempDir(), reader, 2, func(string, os.FileMode) error {
				calls++
				return nil
			})
			if err != nil || calls != len(tc.entries) {
				t.Fatalf("got callbacks=%d, error=%v; want %d callbacks and nil error", calls, err, len(tc.entries))
			}
		})
	}
}

func TestWindowsReadDirEntriesEarlyStop(t *testing.T) {
	entries := make([]os.DirEntry, windowsReadDirBatchSize+1)
	for i := range entries {
		entries[i] = windowsTestDirEntry{fmt.Sprint(i), 0, 0}
	}
	reader := &windowsTestDirReader{entries: entries}
	calls := 0
	err := readDirEntriesWindows(t.TempDir(), reader, -1, func(string, os.FileMode) error {
		calls++
		return errDoneForNow
	})
	if err != nil || calls != 1 || len(reader.limits) != 1 || len(reader.entries) == 0 {
		t.Fatalf("got callbacks=%d, reads=%v, remaining=%d, error=%v", calls, reader.limits, len(reader.entries), err)
	}
}

func TestWindowsReadDirEntriesMultipleBatches(t *testing.T) {
	entries := make([]os.DirEntry, windowsReadDirBatchSize+1)
	for i := range entries {
		entries[i] = windowsTestDirEntry{fmt.Sprint(i), 0, 0}
	}
	reader := &windowsTestDirReader{entries: entries}
	calls := 0
	err := readDirEntriesWindows(t.TempDir(), reader, -1, func(string, os.FileMode) error {
		calls++
		// errSkipFile must continue enumeration, as it does on the other platforms.
		return errSkipFile
	})
	if err != nil || calls != len(entries) || len(reader.limits) < 2 {
		t.Fatalf("got callbacks=%d, reads=%v, error=%v; want %d callbacks", calls, reader.limits, err, len(entries))
	}
}

func TestWindowsReadDirEntriesReadError(t *testing.T) {
	reader := &windowsTestDirReader{
		entries:  []os.DirEntry{windowsTestDirEntry{"object", 0, 0}},
		finalErr: &os.PathError{Op: "readdir", Path: t.TempDir(), Err: os.ErrPermission},
	}
	err := readDirEntriesWindows(t.TempDir(), reader, -1, func(string, os.FileMode) error { return nil })
	if err != errFileAccessDenied {
		t.Fatalf("expected %v, got %v", errFileAccessDenied, err)
	}
}

func TestWindowsReadDirFnPaths(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if err := readDirFn(missing, func(string, os.FileMode) error {
		t.Fatal("callback called for a missing directory")
		return nil
	}); err != nil {
		t.Fatalf("missing directory: %v", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := readDirFn(file, func(string, os.FileMode) error { return nil }); err != errFileNotFound {
		t.Fatalf("file path: expected %v, got %v", errFileNotFound, err)
	}
	for _, p := range []string{missing, file} {
		if _, err := readDirN(p, 0); err != errFileNotFound {
			t.Fatalf("zero count with invalid directory %q: expected %v, got %v", p, errFileNotFound, err)
		}
	}
}

func TestWindowsReadDirJunction(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()
	junction := filepath.Join(dir, "a-junction")
	output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create directory junction: %v: %s", err, output)
	}
	file := filepath.Join(dir, "z-object")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, broken := range []bool{false, true} {
		if broken {
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
		}
		for _, count := range []int{1, -1} {
			entries, err := readDirN(dir, count)
			if err != nil || !reflect.DeepEqual(entries, []string{"z-object"}) {
				t.Fatalf("broken=%v, count=%d: got entries=%v, error=%v", broken, count, entries, err)
			}
		}
		var names []string
		if err := readDirFn(dir, func(name string, _ os.FileMode) error {
			names = append(names, name)
			return nil
		}); err != nil || !reflect.DeepEqual(names, []string{"z-object"}) {
			t.Fatalf("broken=%v: got callback entries=%v, error=%v", broken, names, err)
		}
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if entries, err := readDirN(dir, 1); err != nil || len(entries) != 0 {
		t.Fatalf("only a broken junction remains: got entries=%v, error=%v", entries, err)
	}
}

func TestWindowsReadDirSymlinks(t *testing.T) {
	dir := t.TempDir()
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "file")
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "file-link")); err != nil {
		if errors.Is(err, syscall.ERROR_PRIVILEGE_NOT_HELD) {
			t.Skip("Windows symlink privilege is unavailable")
		}
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"directory-link": targetDir,
		"dangling-link":  filepath.Join(targetDir, "missing"),
	} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, count := range []int{1, -1} {
		entries, err := readDirN(dir, count)
		if err != nil || !reflect.DeepEqual(entries, []string{"file-link"}) {
			t.Fatalf("count=%d: got entries=%v, error=%v", count, entries, err)
		}
	}
	calls := 0
	if err := readDirFn(dir, func(name string, typ os.FileMode) error {
		calls++
		if name != "file-link" || !typ.IsRegular() {
			t.Errorf("unexpected callback entry %q, mode %v", name, typ)
		}
		return nil
	}); err != nil || calls != 1 {
		t.Fatalf("got callbacks=%d, error=%v", calls, err)
	}
}
