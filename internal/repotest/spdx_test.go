// SPDX-License-Identifier: AGPL-3.0-or-later

package repotest

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const header = "// SPDX-License-Identifier: AGPL-3.0-or-later\n"

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSPDX_should_report_files_missing_header(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), header+"package a\n")
	writeFile(t, filepath.Join(root, "b.go"), "package b\n")

	got, err := CheckSPDX(root, "AGPL-3.0-or-later")

	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "b.go")}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCheckSPDX_should_skip_testdata_git_and_vendor(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"testdata", ".git", "vendor"} {
		writeFile(t, filepath.Join(root, dir, "x.go"), "package x\n")
	}

	got, err := CheckSPDX(root, "AGPL-3.0-or-later")

	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}

func TestRepository_should_have_SPDX_header_on_every_go_file(t *testing.T) {
	got, err := CheckSPDX(filepath.Join("..", ".."), "AGPL-3.0-or-later")

	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("files missing SPDX header: %v", got)
	}
}
