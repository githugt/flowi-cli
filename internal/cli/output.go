// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/importers"
)

// Output formats accepted by --output.
const (
	OutputText = "text"
	OutputJSON = "json"
)

// PrintDiagnostics writes diagnostics in the chosen format. Paths are shown
// relative to base when they are inside it.
func PrintDiagnostics(w io.Writer, format, base string, l diag.List) error {
	writeOne := map[string]func(io.Writer, string, diag.Diagnostic) error{
		OutputText: printText,
		OutputJSON: printJSON,
	}[format]
	if writeOne == nil {
		return &UsageError{Err: fmt.Errorf("unknown output format %q (want text or json)", format)}
	}
	for _, d := range l {
		if err := writeOne(w, base, d); err != nil {
			return fmt.Errorf("writing diagnostics: %w", err)
		}
	}
	return nil
}

func displayPath(base, file string) string {
	if base == "" {
		return file
	}
	rel, err := filepath.Rel(base, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		return file
	}
	return rel
}

func printText(w io.Writer, base string, d diag.Diagnostic) error {
	msg, hint := diag.English().Message(d)
	if _, err := fmt.Fprintf(w, "%s%s %s: %s\n", locationPrefix(base, d), d.Severity, d.Code, msg); err != nil {
		return err
	}
	for _, line := range d.Excerpt {
		if _, err := fmt.Fprintf(w, "    | %s\n", line); err != nil {
			return err
		}
	}
	if hint != "" {
		if _, err := fmt.Fprintf(w, "    hint: %s\n", hint); err != nil {
			return err
		}
	}
	for _, rel := range d.Related {
		if _, err := fmt.Fprintf(w, "    also at: %s\n", strings.TrimSuffix(locationPrefix(base, diag.Diagnostic{Location: rel}), ": ")); err != nil {
			return err
		}
	}
	return nil
}

type jsonDiagnostic struct {
	Code     diag.Code      `json:"code"`
	Severity string         `json:"severity"`
	File     string         `json:"file"`
	Line     int            `json:"line"`
	Column   int            `json:"column"`
	Path     string         `json:"path,omitempty"`
	Message  string         `json:"message"`
	Hint     string         `json:"hint,omitempty"`
	Related  []jsonLocation `json:"related,omitempty"`
	Excerpt  []string       `json:"excerpt,omitempty"`
}

type jsonLocation struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Path   string `json:"path,omitempty"`
}

func printJSON(w io.Writer, base string, d diag.Diagnostic) error {
	return json.NewEncoder(w).Encode(newJSONDiagnostic(base, d))
}

func newJSONDiagnostic(base string, d diag.Diagnostic) jsonDiagnostic {
	msg, hint := diag.English().Message(d)
	related := make([]jsonLocation, 0, len(d.Related))
	for _, r := range d.Related {
		related = append(related, jsonLocation{File: displayPath(base, r.File), Line: r.Pos.Line, Column: r.Pos.Column, Path: r.Path})
	}
	return jsonDiagnostic{
		Code: d.Code, Severity: d.Severity.String(), File: displayPath(base, d.File),
		Line: d.Pos.Line, Column: d.Pos.Column, Path: d.Path, Message: msg, Hint: hint, Related: related,
		Excerpt: d.Excerpt,
	}
}

// writeImportReport lists each imported item: ✓ clean, ⚠ imported with
// warnings, ✗ skipped, each followed by its diagnostics, then a count.
func writeImportReport(w io.Writer, base string, items []importers.Item) error {
	skipped := 0
	for _, it := range items {
		mark, note := "✓", ""
		switch {
		case it.Skipped:
			mark, note = "✗", " (skipped)"
			skipped++
		case len(it.Diags) > 0:
			mark = "⚠"
		}
		if _, err := fmt.Fprintf(w, "  %s %s%s\n", mark, it.Name, note); err != nil {
			return err
		}
		var b strings.Builder
		if err := PrintDiagnostics(&b, OutputText, base, it.Diags); err != nil {
			return err
		}
		for _, line := range strings.SplitAfter(b.String(), "\n") {
			if line == "" {
				continue
			}
			if _, err := io.WriteString(w, "      "+line); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(w, "%d imported, %d skipped\n", len(items)-skipped, skipped)
	return err
}

// summarise returns "N errors, M warnings".
func summarise(l diag.List) string {
	errs := 0
	for _, d := range l {
		if d.Severity == diag.SeverityError {
			errs++
		}
	}
	return fmt.Sprintf("%d errors, %d warnings", errs, len(l)-errs)
}

// locationPrefix is "file:line:col: ", "file: ", or nothing when the
// diagnostic is not about a file (for example a command-line argument).
func locationPrefix(base string, d diag.Diagnostic) string {
	if d.File == "" {
		return ""
	}
	where := displayPath(base, d.File)
	if d.Pos.Line > 0 {
		where = fmt.Sprintf("%s:%d:%d", where, d.Pos.Line, d.Pos.Column)
	}
	return where + ": "
}
