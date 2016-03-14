// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package osutil

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"syscall"
)

// FileState describes the expected content and meta data of a single file.
type FileState struct {
	Content []byte
	Mode    os.FileMode
	UID     uint32
	GID     uint32
}

// EnsureDirState ensures that directory content matches expectations.
//
// EnsureDirState enumerates all the files in the specified directory that
// match the provided pattern (glob). Each enumerated file is checked to ensure
// that the contents, permissions and ownership are what is desired. Unexpected
// files are removed.  Missing files are created and differing files are
// corrected.  Files not matching the pattern are ignored.
//
// The content map describes each of the files that are intended to exist in
// the directory.  Map keys must be file names relative to the directory.
// Sub-directories in the name are not allowed.
//
// The function stops at the first encountered error but reports all of the
// changes performed so far. Information about the performed changes is
// returned to the caller for any extra processing that might be required (e.g.
// to run some helper program).
//
// The return value is: created, corrected, removed []string, err error
func EnsureDirState(dir, glob string, content map[string]*FileState) ([]string, []string, []string, error) {
	matches, err := filepath.Glob(path.Join(dir, glob))
	if err != nil {
		return nil, nil, nil, err
	}
	found := make(map[string]bool)
	var created, corrected, removed []string
	// Analyze files that inhabit the subset defined by our glob pattern.
	for _, name := range matches {
		baseName := path.Base(name)
		var file *os.File
		if file, err = os.OpenFile(name, os.O_RDWR, 0); err != nil {
			return created, corrected, removed, err
		}
		defer file.Close()
		var stat os.FileInfo
		if stat, err = file.Stat(); err != nil {
			return created, corrected, removed, err
		}
		// Remove files that should not be here.
		var expected *FileState
		var shouldBeHere bool
		if expected, shouldBeHere = content[baseName]; !shouldBeHere {
			if err := os.RemoveAll(name); err != nil {
				return created, corrected, removed, err
			}
			removed = append(removed, baseName)
			continue
		}
		// Verify files that should be here.
		changed := false
		// Check that file has the right content
		if stat.Size() == int64(len(expected.Content)) {
			var content []byte
			if content, err = ioutil.ReadFile(file.Name()); err != nil {
				return created, corrected, removed, err
			}
			if !bytes.Equal(content, expected.Content) {
				if err := AtomicWriteFile(file.Name(), expected.Content, expected.Mode, 0); err != nil {
					return created, corrected, removed, err
				}
				changed = true
			}
		} else {
			if err := AtomicWriteFile(file.Name(), expected.Content, expected.Mode, 0); err != nil {
				return created, corrected, removed, err
			}
			changed = true
		}
		// Check that file has the right meta-data
		currentPerm := stat.Mode().Perm()
		expectedPerm := expected.Mode.Perm()
		if currentPerm != expectedPerm {
			if err := file.Chmod(expectedPerm); err != nil {
				return created, corrected, removed, err
			}
			changed = true
		}
		if st, ok := stat.Sys().(*syscall.Stat_t); ok {
			if st.Uid != expected.UID || st.Gid != expected.GID {
				if err := file.Chown(int(expected.UID), int(expected.GID)); err != nil {
					return created, corrected, removed, err
				}
				changed = true
			}
		}
		if changed {
			corrected = append(corrected, baseName)
		}
		found[baseName] = true
	}
	// Create files that were not found but are expected
	for baseName, expected := range content {
		if baseName != path.Base(baseName) {
			err := fmt.Errorf("expected files cannot have path component: %q", baseName)
			return created, corrected, removed, err
		}
		var matched bool
		matched, err = filepath.Match(glob, baseName)
		if err != nil {
			return created, corrected, removed, err
		}
		if !matched {
			err := fmt.Errorf("expected files must match pattern: %q (pattern: %q)", baseName, glob)
			return created, corrected, removed, err
		}
		if found[baseName] {
			continue
		}
		if err := ioutil.WriteFile(path.Join(dir, baseName), expected.Content, expected.Mode); err != nil {
			return created, corrected, removed, err
		}
		created = append(created, baseName)
	}
	return created, corrected, removed, nil
}
