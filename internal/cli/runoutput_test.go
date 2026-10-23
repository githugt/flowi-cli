// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/engine"
)

func TestParseReportFlags(t *testing.T) {
	tests := []struct {
		in      []string
		want    []reportSpec
		wantErr bool
	}{
		{in: []string{"junit=out.xml", "json=r.json", "markdown=r.md", "text=r.txt"},
			want: []reportSpec{{"junit", "out.xml"}, {"json", "r.json"}, {"markdown", "r.md"}, {"text", "r.txt"}}},
		{in: []string{"pdf=r.pdf"}, wantErr: true},
		{in: []string{"junit"}, wantErr: true},
		{in: []string{"junit="}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.in, ","), func(t *testing.T) {
			got, err := parseReports(tt.in)

			var usage *UsageError
			if tt.wantErr != errors.As(err, &usage) {
				t.Fatalf("err %v", err)
			}
			if !tt.wantErr && len(got) != len(tt.want) {
				t.Fatalf("got %v", got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v", got)
				}
			}
		})
	}
}

func TestPlainRunOutput_should_indent_by_path_and_end_with_summary(t *testing.T) {
	var out, errOut bytes.Buffer
	p := runText{out: &out, err: &errOut}
	fail := diag.New(diag.CodeRunAssertFailed, diag.Location{}, map[string]string{"check": "x", "message": "", "node": "n"})
	for _, ev := range []engine.Event{
		engine.NodeFinished{Path: "login", Node: "login", Status: engine.StatusPassed, Duration: 120 * time.Millisecond},
		engine.NodeFinished{Path: "login/pre:request:0/sign", Node: "sign", Status: engine.StatusPassed, Depth: 1},
		engine.NodeFinished{Path: "users[0]/create", Node: "create", Status: engine.StatusFailed, Duration: time.Millisecond, Diags: diag.List{fail}},
		engine.NodeFinished{Path: "users", Node: "users", Status: engine.StatusFailed, Duration: 5 * time.Millisecond},
		engine.Log{Path: "say", Node: "say", Message: "hello"},
		engine.Digest{Node: "d", Format: "markdown", Text: "# Digest\n"},
	} {
		p.event(ev)
	}
	p.summary(engine.RunResult{RunID: "R1", Seed: 7, Status: engine.StatusFailed, ReportFile: "/r.json"}, 2*time.Second)

	want := "✓ login  120ms\n" +
		"  ✗ users[0]/create  1ms\n" +
		"      FLW-RUN-001 assertion failed: x\n" +
		"✗ users  5ms\n" +
		"  log say: hello\n" +
		"# Digest\n" +
		"✗ failed  2s  run R1 · seed 7 · report /r.json\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}
