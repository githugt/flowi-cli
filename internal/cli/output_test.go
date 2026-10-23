// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/importers"
)

func sampleDiagnostics(base string) diag.List {
	return diag.List{
		diag.New(diag.CodeWSMissing, diag.Location{File: filepath.Join(base, "ws", "workspace.yaml"), Path: "$.collections[0].path", Pos: diag.Position{Line: 4, Column: 11}},
			map[string]string{"id": "01J", "path": "/gone"}),
		diag.New(diag.CodeFileUnrecognised, diag.Location{File: filepath.Join(base, "requests", "x.yaml")}, map[string]string{"name": "x.yaml"}),
	}
}

func TestPrintDiagnostics_text_should_include_hint_line(t *testing.T) {
	base := t.TempDir()
	var out bytes.Buffer

	err := PrintDiagnostics(&out, "text", base, sampleDiagnostics(base))

	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("ws", "workspace.yaml") + ":4:11: error FLW-WS-001: collection 01J not found at /gone\n" +
		"    hint: run \"flowi workspace locate\"\n" +
		filepath.Join("requests", "x.yaml") + ": warning FLW-FILE-001: unrecognised file \"x.yaml\" (expected *.req.yaml / *.arazzo.yaml / _folder.yaml)\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestPrintDiagnostics_json_should_write_one_object_per_line(t *testing.T) {
	base := t.TempDir()
	var out bytes.Buffer

	err := PrintDiagnostics(&out, "json", base, sampleDiagnostics(base))

	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines", len(lines))
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["code"] != "FLW-WS-001" || first["severity"] != "error" || first["line"] != float64(4) || first["hint"] != `run "flowi workspace locate"` || first["path"] != "$.collections[0].path" {
		t.Fatalf("got %v", first)
	}
}

func TestPrintDiagnostics_should_reject_unknown_format(t *testing.T) {
	err := PrintDiagnostics(&bytes.Buffer{}, "xml", "", nil)

	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestPrintDiagnostics_should_print_related_locations(t *testing.T) {
	base := t.TempDir()
	d := diag.New(diag.CodeIDDuplicate, diag.Location{File: filepath.Join(base, "b.yaml"), Path: "$.id", Pos: diag.Position{Line: 1, Column: 5}}, map[string]string{"id": "X"})
	d.Related = []diag.Location{{File: filepath.Join(base, "a.yaml"), Path: "$.id", Pos: diag.Position{Line: 1, Column: 5}}}
	var text, js bytes.Buffer

	textErr := PrintDiagnostics(&text, "text", base, diag.List{d})
	jsonErr := PrintDiagnostics(&js, "json", base, diag.List{d})

	if textErr != nil || jsonErr != nil {
		t.Fatal(textErr, jsonErr)
	}
	if !strings.Contains(text.String(), "\n    also at: a.yaml:1:5\n") {
		t.Fatalf("text:\n%s", text.String())
	}
	var got map[string]any
	if err := json.Unmarshal(js.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	related, _ := got["related"].([]any)
	if len(related) != 1 || related[0].(map[string]any)["file"] != "a.yaml" {
		t.Fatalf("json %v", got)
	}
}

func TestPrintDiagnostics_text_should_print_the_excerpt(t *testing.T) {
	d := diag.New(diag.CodeImportSyntax, diag.Location{File: "/w/cmd.txt", Pos: diag.Position{Line: 1, Column: 6}},
		map[string]string{"format": "curl", "detail": "unclosed quote"})
	d.Excerpt = []string{"curl 'https://x", "     ^"}
	var b bytes.Buffer

	if err := PrintDiagnostics(&b, OutputText, "/w", diag.List{d}); err != nil {
		t.Fatal(err)
	}

	want := "cmd.txt:1:6: error FLW-IMPORT-004: curl syntax error: unclosed quote\n    | curl 'https://x\n    |      ^\n"
	if b.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestWriteImportReport_should_list_items_and_their_problems(t *testing.T) {
	ok := importers.Item{Name: "auth/login"}
	warned := importers.Item{Name: "users/create"}
	warned.Unmapped("pre-request script", "left out", "scripts are JavaScript")
	skipped := importers.Item{Name: "broken", Source: diag.Location{File: "/w/c.json", Pos: diag.Position{Line: 3, Column: 2}}}
	skipped.Skip("the request has no URL")
	var b bytes.Buffer

	if err := writeImportReport(&b, "/w", []importers.Item{ok, warned, skipped}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"  ✓ auth/login\n",
		"  ⚠ users/create\n      warning FLW-IMPORT-001: pre-request script has no Flow-I equivalent",
		"  ✗ broken (skipped)\n      c.json:3:2: error FLW-IMPORT-005: broken was skipped: the request has no URL\n",
		"2 imported, 1 skipped\n",
	} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, b.String())
		}
	}
}
