// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/githugt/flowi-lib/engine"
)

// RunFlow shows a flow run's events live on out as a tree until the stream
// closes, then clears the live area and returns every event it showed, so
// the caller can print the permanent (plain) record — as RunSend does.
//
// Goroutine ownership: the Bubble Tea program runs on the caller's
// goroutine; RunFlow starts one goroutine that forwards every event to it
// and exits when the events channel closes, and waits for it. Input is
// not read and signals are left to the process, so Ctrl-C cancels the run.
func RunFlow(events <-chan engine.Event, out io.Writer, o Options) ([]engine.Event, error) {
	width, height := screenSize(out, o.Width)
	opts := []tea.ProgramOption{tea.WithOutput(out), tea.WithInput(nil), tea.WithoutSignalHandler(), tea.WithWindowSize(width, height)}
	p := tea.NewProgram(newRunModel(o, height), opts...)
	forwarded := make(chan []engine.Event, 1)
	go func() {
		var seen []engine.Event
		for ev := range events {
			seen = append(seen, ev)
			p.Send(eventMsg{ev})
		}
		p.Send(closedMsg{})
		forwarded <- seen
	}()
	_, err := p.Run()
	seen := <-forwarded
	if err != nil {
		return seen, fmt.Errorf("live view: %w", err)
	}
	return seen, nil
}

// runNode is one node instance as the tree shows it.
type runNode struct {
	path, parent, lane, typ string
	status                  engine.Status // "" while running
	duration                time.Duration
}

type runModel struct {
	styles   styles
	spin     spinner.Model
	height   int
	order    []string // node paths in start order
	nodes    map[string]*runNode
	hooks    map[string][]*hookLine // running request node → its hook steps
	stopped  string                 // "", "cancelled" or "aborted"
	finished bool                   // the stream closed: the live area is cleared
}

func newRunModel(o Options, height int) runModel {
	return runModel{
		styles: newStyles(o.Color), spin: spinner.New(spinner.WithSpinner(spinner.Dot)), height: height,
		nodes: map[string]*runNode{}, hooks: map[string][]*hookLine{},
	}
}

func (m runModel) Init() tea.Cmd { return m.spin.Tick }

func (m runModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case eventMsg:
		m.apply(msg.ev)
		return m, nil
	case closedMsg:
		m.finished = true
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *runModel) apply(ev engine.Event) {
	switch e := ev.(type) {
	case engine.NodeStarted:
		m.order = append(m.order, e.Path)
		m.nodes[e.Path] = &runNode{path: e.Path, parent: e.Parent, lane: e.Lane, typ: e.Type}
	case engine.NodeFinished:
		if n, ok := m.nodes[e.Path]; ok {
			n.status, n.duration = e.Status, e.Duration
		}
	case engine.HookStarted:
		m.hooks[e.Path] = append(m.hooks[e.Path], &hookLine{phase: e.Phase, level: e.Level, step: e.Step})
	case engine.HookFinished:
		for _, h := range m.hooks[e.Path] {
			if h.phase == e.Phase && h.level == e.Level && h.step == e.Step && h.status == "" {
				h.status, h.duration = e.Status, e.Duration
			}
		}
	case engine.RunCancelled:
		m.stopped = "cancelled"
	case engine.RunAborted:
		m.stopped = "aborted"
	}
}

func (m runModel) View() tea.View {
	if m.finished {
		return tea.NewView("")
	}
	var lines []string
	for _, p := range m.order {
		n := m.nodes[p]
		if m.hidden(n) {
			continue
		}
		lines = append(lines, m.nodeLine(n))
		if n.status == "" {
			lines = append(lines, m.hookLines(n)...)
		}
	}
	lines = truncate(lines, m.height-4)
	if m.stopped != "" {
		lines = append(lines, m.styles.fail.Render(m.stopped))
	}
	return tea.NewView(strings.Join(lines, "\n") + "\n")
}

// hidden: nodes inside hooks fold into their hook's line, and finished
// nodes of repeated bodies (batch, run_till) into their container's
// counts.
func (m runModel) hidden(n *runNode) bool {
	for cur := n; cur != nil; cur = m.nodes[cur.parent] {
		if strings.HasPrefix(cur.lane, "pre:") || strings.HasPrefix(cur.lane, "post:") {
			return true
		}
		if parent, ok := m.nodes[cur.parent]; ok && repeats(parent.typ) && n.status != "" {
			return true
		}
	}
	return false
}

func repeats(typ string) bool { return typ == "batch" || typ == "run_till" }

func (m runModel) nodeLine(n *runNode) string {
	pad := strings.Repeat("  ", strings.Count(n.path, "/"))
	text := n.path
	if repeats(n.typ) {
		text += "  " + m.styles.dim.Render(m.laneCounts(n.path))
	}
	switch n.status {
	case "":
		return pad + m.spin.View() + " " + text
	case engine.StatusPassed, engine.StatusSkipped:
		return pad + m.styles.ok.Render(mark(n.status)) + " " + text + "  " + m.styles.dim.Render(round(n.duration).String())
	}
	return pad + m.styles.fail.Render(mark(n.status)) + " " + text + "  " + m.styles.dim.Render(round(n.duration).String())
}

func (m runModel) hookLines(n *runNode) []string {
	pad := strings.Repeat("  ", strings.Count(n.path, "/")+1)
	var out []string
	for _, h := range m.hooks[n.path] {
		text := h.phase + "  " + h.level + "  " + h.step
		if h.status == "" {
			out = append(out, pad+text+" "+m.spin.View())
			continue
		}
		out = append(out, pad+mark(h.status)+" "+text)
	}
	return out
}

// laneCounts summarises a repeating container's iterations: passed,
// failed and running.
func (m runModel) laneCounts(container string) string {
	state := map[string]string{} // lane → running | failed | passed
	var lanes []string
	for _, p := range m.order {
		n := m.nodes[p]
		if n.parent != container {
			continue
		}
		if _, seen := state[n.lane]; !seen {
			lanes = append(lanes, n.lane)
			state[n.lane] = "passed"
		}
		switch {
		case n.status == "":
			state[n.lane] = "running"
		case n.status != engine.StatusPassed && n.status != engine.StatusSkipped && state[n.lane] != "running":
			state[n.lane] = "failed"
		}
	}
	counts := map[string]int{}
	for _, l := range lanes {
		counts[state[l]]++
	}
	return fmt.Sprintf("[✓%d ✗%d ⋯%d]", counts["passed"], counts["failed"], counts["running"])
}

func mark(s engine.Status) string {
	marks := map[engine.Status]string{engine.StatusPassed: "✓", engine.StatusSkipped: "○", engine.StatusError: "!", engine.StatusCancelled: "⊘", engine.StatusAborted: "⊘"}
	if m, ok := marks[s]; ok {
		return m
	}
	return "✗"
}

// truncate keeps at most limit lines, the last saying how many were left
// out.
func truncate(lines []string, limit int) []string {
	if limit < 2 || len(lines) <= limit {
		return lines
	}
	kept := lines[:limit-1]
	return append(kept, fmt.Sprintf("… %d more", len(lines)-len(kept)))
}
