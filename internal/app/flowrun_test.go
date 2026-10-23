// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/githugt/flowi-cli/internal/cli"
)

func TestRun_exit_codes(t *testing.T) {
	e := newSendEnv(t)
	writeFile(t, filepath.Join(e.dir, "tools", "flows", "fail.arazzo.yaml"), arazzoFlow("id: 01JA00000000000000000000T5\nnodes:\n  - id: never\n    type: assert\n    check: \"1 == 2\"\n"))
	unwritable := filepath.Join(e.dir, "missing-dir", "out.xml")
	tests := []struct {
		name   string
		args   []string
		code   int
		stderr []string
	}{
		{"passed", []string{"run", "user-creation", "--env", "local"}, 0, nil},
		{"unknown flow", []string{"run", "nope"}, 2, []string{"FLW-FLOW-002"}},
		{"undeclared input", []string{"run", "user-creation", "--env", "local", "--input", "nope=1"}, 2, []string{"FLW-RUN-010"}},
		{"bad input flag", []string{"run", "user-creation", "--input", "novalue"}, 2, []string{"--input"}},
		{"assertion failed", []string{"run", "fail", "--env", "local"}, 1, []string{"FLW-RUN-001"}},
		{"server down", []string{"run", "user-creation", "--env", "staging"}, 4, []string{"FLW-HTTP-001"}},
		{"bad report flag", []string{"run", "user-creation", "--report", "pdf=x.pdf"}, 2, []string{"--report"}},
		{"unwritable report", []string{"run", "user-creation", "--env", "local", "--report", "junit=" + unwritable}, 4, []string{"out.xml"}},
		{"missing argument", []string{"run"}, 2, []string{"accepts 1 arg"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := e.run(context.Background(), tt.args...)

			if code != tt.code {
				t.Fatalf("exit %d, want %d\n%s", code, tt.code, errOut)
			}
			for _, want := range tt.stderr {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr lacks %q:\n%s", want, errOut)
				}
			}
			if strings.Contains(errOut, demoPassword) {
				t.Errorf("password in stderr:\n%s", errOut)
			}
		})
	}
}

func TestRun_Given_interrupt_Then_exit_3_and_report_cancelled(t *testing.T) {
	e := newSendEnv(t)
	writeFile(t, filepath.Join(e.dir, "tools", "flows", "wait.arazzo.yaml"),
		arazzoFlow("id: 01JA00000000000000000000T6\nnodes:\n  - id: slow\n    type: request\n    request:\n      id: 01JA00000000000000000000T3\n      path: slow\n"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()

	code, out, errOut := e.run(ctx, "run", "wait", "--env", "local", "--report", "json="+filepath.Join(e.dir, "r.json"))

	if code != cli.ExitCancelled || time.Since(start) > 5*time.Second {
		t.Fatalf("exit %d after %v\n%s\n%s", code, time.Since(start), out, errOut)
	}
	data, err := os.ReadFile(filepath.Join(e.dir, "r.json"))
	if err != nil || !strings.Contains(string(data), `"status": "cancelled"`) {
		t.Fatalf("report %s %v", data, err)
	}
}

// Final review, Minor 10 (graded Important): the plan's exit table says a
// report that cannot be written is exit 4 — the saved run report too, not
// only --report files.
func TestRun_Given_run_report_cannot_be_saved_Then_exit_4(t *testing.T) {
	e := newSendEnv(t)
	runs := filepath.Join(e.dir, "config", "workspaces", "default", "data", "01JA0000000000000000000001", "runs")
	writeFile(t, runs, "a file where the runs directory should be")

	code, _, errOut := e.run(context.Background(), "run", "user-creation", "--env", "local")

	if code != cli.ExitSystem || !strings.Contains(errOut, "run report") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}

// arazzoFlow wraps a flow's own YAML in the minimal Arazzo document a flow file is.
func arazzoFlow(body string) string {
	var sb strings.Builder
	sb.WriteString("arazzo: 1.1.0\nworkflows:\n  - workflowId: w\n    x-flowi-flow:\n")
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		sb.WriteString("      " + line + "\n")
	}
	return sb.String()
}
