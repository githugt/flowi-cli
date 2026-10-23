// SPDX-License-Identifier: AGPL-3.0-or-later

// Package repotest holds repository-wide hygiene checks run as tests.
package repotest

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// skippedDirs are never scanned: VCS metadata, fixtures and vendored code
// are not ours to license.
func skippedDirs() map[string]bool {
	return map[string]bool{".git": true, "testdata": true, "vendor": true}
}

// CheckSPDX returns the .go files under root whose first line is not
// "// SPDX-License-Identifier: <want>".
func CheckSPDX(root, want string) ([]string, error) {
	expected := "// SPDX-License-Identifier: " + want
	skip := skippedDirs()
	var missing []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		line, err := readFirstLine(path)
		if err != nil {
			return err
		}
		if line != expected {
			missing = append(missing, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning %s for SPDX headers: %w", root, err)
	}
	return missing, nil
}

func readFirstLine(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from walking the repository tree, not from user input
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close() // read-only file: a close error cannot lose data

	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return "", sc.Err()
	}
	return strings.TrimRight(sc.Text(), "\r"), nil
}
