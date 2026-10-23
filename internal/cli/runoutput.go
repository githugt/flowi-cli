// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/engine"

	"github.com/githugt/flowi-cli/internal/tui"
)

// runText prints a run for people: a line per node as it finishes
// (indented by its place in the tree), log lines and stdout digests, then
// one summary line. Diagnostics of failures follow on stderr.
type runText struct {
	out, err io.Writer
	quiet    bool
}

func (p runText) render(run *engine.Run) engine.RunResult {
	for ev := range run.Events() {
		if !p.quiet {
			p.event(ev)
		}
	}
	res := run.Wait()
	p.summary(res, res.Report.Duration)
	return res
}

// live shows the Bubble Tea tree, then prints the plain record of what it
// showed, so the scrollback matches non-terminal output.
func (p runText) live(run *engine.Run, console Console) engine.RunResult {
	seen, err := tui.RunFlow(run.Events(), p.out, tui.Options{Color: !console.NoColor})
	if err != nil {
		drainRun(run) // the live view failed; the run still has to finish
		p.line(p.err, "warning: %v", err)
	}
	for _, ev := range seen {
		p.event(ev)
	}
	res := run.Wait()
	p.summary(res, res.Report.Duration)
	return res
}

// event prints one event's progress line, if it has one.
func (p runText) event(ev engine.Event) {
	switch e := ev.(type) {
	case engine.NodeFinished:
		p.nodeFinished(e)
	case engine.Log:
		p.line(p.out, "%s  log %s: %s", indent(e.Path), e.Path, e.Message)
	case engine.Warning:
		msg, _ := diag.English().Message(e.Diag)
		p.line(p.out, "  ! %s %s", e.Diag.Code, msg)
	case engine.Digest:
		_, _ = io.WriteString(p.out, ensureNewline(e.Text)) // stdout closed: nothing more to do
	case engine.RunCancelled:
		p.line(p.err, "cancelled — waiting for running nodes to stop")
	case engine.RunAborted:
		p.line(p.err, "aborted after %s; still running: %s", formatGrace(e.Grace), strings.Join(e.Inflight, ", "))
	}
}

// nodeFinished prints top-level and container nodes; nodes inside hooks
// belong to their request's line.
func (p runText) nodeFinished(e engine.NodeFinished) {
	if e.Depth > 0 {
		return
	}
	pad := indent(e.Path)
	p.line(p.out, "%s%s %s  %s", pad, runMark(e.Status, e.Handled), e.Path, shortDuration(e.Duration))
	if e.Status == engine.StatusPassed || e.Status == engine.StatusSkipped {
		return
	}
	for _, d := range e.Diags {
		msg, _ := diag.English().Message(d)
		p.line(p.out, "%s    %s %s", pad, d.Code, msg)
	}
}

// summary is the run's last line: status, duration, run id, seed, report.
func (p runText) summary(res engine.RunResult, took time.Duration) {
	line := fmt.Sprintf("%s %s  %s  run %s · seed %d", runMark(res.Status, false), res.Status, shortDuration(took), res.RunID, res.Seed)
	if res.ReportFile != "" {
		line += " · report " + res.ReportFile
	}
	p.line(p.out, "%s", line)
}

func (p runText) line(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", args...) // output is best effort; the exit code carries the outcome
}

func runMark(s engine.Status, handled bool) string {
	if handled {
		return "↷"
	}
	marks := map[engine.Status]string{
		engine.StatusPassed: "✓", engine.StatusFailed: "✗", engine.StatusError: "!", engine.StatusSkipped: "○",
		engine.StatusCancelled: "⊘", engine.StatusAborted: "⊘",
	}
	if m, ok := marks[s]; ok {
		return m
	}
	return "?"
}

// indent is two spaces per level of a node path (users[0]/create is one
// level down).
func indent(path string) string {
	return strings.Repeat("  ", strings.Count(path, "/"))
}

func shortDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(10 * time.Millisecond).String()
}

func ensureNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// writeRunNDJSON writes each event as one JSON object, then a final
// {"event":"RunResult", …} — a scripting interface: keep it stable.
func writeRunNDJSON(w io.Writer, run *engine.Run) (engine.RunResult, error) {
	for ev := range run.Events() {
		if err := writeEvent(w, ev); err != nil {
			drainRun(run)
			return run.Wait(), err
		}
	}
	res := run.Wait()
	return res, writeNamed(w, "RunResult", runResultJSON(res))
}

type runResultOut struct {
	RunID       string           `json:"run_id"`
	Seed        uint64           `json:"seed"`
	Status      engine.Status    `json:"status"`
	ReportFile  string           `json:"report_file"`
	Diagnostics []jsonDiagnostic `json:"diagnostics"`
}

func runResultJSON(res engine.RunResult) runResultOut {
	return runResultOut{RunID: res.RunID, Seed: res.Seed, Status: res.Status, ReportFile: res.ReportFile, Diagnostics: jsonDiagnostics(res.Diags)}
}

func drainRun(run *engine.Run) {
	for range run.Events() {
	}
}
