// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/githugt/flowi-lib/engine"
)

// Options configure the live view.
type Options struct {
	Color bool
	// Width is the screen width to assume when out is not a terminal
	// (tests); 0 uses the terminal's own size.
	Width int
}

const (
	defaultWidth  = 80
	defaultHeight = 24
)

// Renderer is how a send is shown.
type Renderer int

// Renderers.
const (
	RendererText Renderer = iota + 1 // plain lines (pipes, CI, --quiet)
	RendererLive                     // Bubble Tea progress on a terminal
	RendererJSON                     // NDJSON events (--output json)
)

// ChooseRenderer picks live progress only when both output streams are
// terminals and plain text output was asked for without --quiet.
func ChooseRenderer(output string, quiet, outTTY, errTTY bool) Renderer {
	switch {
	case output == "json":
		return RendererJSON
	case quiet || !outTTY || !errTTY:
		return RendererText
	}
	return RendererLive
}

// RunSend shows a send's events live on out until the stream closes, then
// clears the live area and returns every event it showed, so the caller
// can print the permanent (plain) record of the send: the scrollback then
// matches non-terminal output exactly.
//
// Goroutine ownership: the Bubble Tea program runs on the caller's
// goroutine. RunSend starts one goroutine that forwards every event to it
// and exits when the events channel closes; RunSend waits for it, so the
// engine is never left blocked on an unread event. Input is not read (no
// raw mode) and signals are left to the process, so Ctrl-C cancels the
// send as it does without the live view.
func RunSend(events <-chan engine.Event, out io.Writer, o Options) ([]engine.Event, error) {
	opts := []tea.ProgramOption{tea.WithOutput(out), tea.WithInput(nil), tea.WithoutSignalHandler()}
	// A send often ends before Bubble Tea has learned the terminal size,
	// and a frame rendered at width 0 is empty; so pass the size up front.
	opts = append(opts, tea.WithWindowSize(screenSize(out, o.Width)))
	p := tea.NewProgram(newSendModel(o), opts...)
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

// screenSize is the terminal's size when out is one that reports it;
// otherwise fallback (or defaultWidth) wide. Some pseudo-terminals report
// 0x0, which would render nothing.
func screenSize(out io.Writer, fallback int) (int, int) {
	if f, ok := out.(*os.File); ok {
		w, h, err := term.GetSize(int(f.Fd())) //nolint:gosec // file descriptors fit in int
		if err == nil && w > 0 && h > 0 {
			return w, h
		}
	}
	if fallback <= 0 {
		fallback = defaultWidth
	}
	return fallback, defaultHeight
}

type (
	eventMsg  struct{ ev engine.Event }
	closedMsg struct{}
)

// item is one line: a hook step or the request.
type item struct {
	hook    *hookLine
	request *requestLine
}

type hookLine struct {
	phase, level, step string
	status             engine.Status // "" while running
	duration           time.Duration
}

type requestLine struct {
	method, url string
	attempt     int
	status      int
	err         string
	duration    time.Duration
	retried     string // why the previous attempt is being retried
	done        bool
}

type sendModel struct {
	styles    styles
	spin      spinner.Model
	items     []item
	cancelled bool
	finished  bool // the stream closed: the live area is cleared
}

func newSendModel(o Options) sendModel {
	return sendModel{styles: newStyles(o.Color), spin: spinner.New(spinner.WithSpinner(spinner.Dot))}
}

func (m sendModel) Init() tea.Cmd { return m.spin.Tick }

func (m sendModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

// apply folds one event into the lines. Requests inside hook flows
// (Depth > 0) are part of their hook's line.
func (m *sendModel) apply(ev engine.Event) {
	switch e := ev.(type) {
	case engine.HookStarted:
		m.items = append(m.items, item{hook: &hookLine{phase: e.Phase, level: e.Level, step: e.Step}})
	case engine.HookFinished:
		if h := m.runningHook(); h != nil {
			h.status, h.duration = e.Status, e.Duration
		}
	case engine.RequestSent:
		if e.Depth == 0 {
			r := m.request(e.Method, e.URL)
			r.attempt, r.done = e.Attempt, false
		}
	case engine.ResponseReceived:
		if e.Depth == 0 {
			m.received(e)
		}
	case engine.RunCancelled:
		m.cancelled = true
	}
}

func (m *sendModel) runningHook() *hookLine {
	for i := len(m.items) - 1; i >= 0; i-- {
		if h := m.items[i].hook; h != nil && h.status == "" {
			return h
		}
	}
	return nil
}

// request returns the request line, adding it on first use.
func (m *sendModel) request(method, url string) *requestLine {
	for _, it := range m.items {
		if it.request != nil {
			return it.request
		}
	}
	r := &requestLine{method: method, url: url}
	m.items = append(m.items, item{request: r})
	return r
}

func (m *sendModel) received(e engine.ResponseReceived) {
	r := m.request("", "")
	outcome := e.Error
	if outcome == "" {
		outcome = fmt.Sprint(e.Status)
	}
	if e.Wait > 0 {
		r.retried = fmt.Sprintf("%s after attempt %d, waited %s", outcome, e.Attempt, round(e.Wait))
		return
	}
	r.status, r.err, r.duration, r.done = e.Status, e.Error, e.Duration, true
}

func (m sendModel) View() tea.View {
	if m.finished {
		return tea.NewView("")
	}
	var b strings.Builder
	for _, it := range m.items {
		if it.hook != nil {
			b.WriteString(m.hookText(*it.hook))
		} else {
			b.WriteString(m.requestText(*it.request))
		}
		b.WriteByte('\n')
	}
	if m.cancelled {
		b.WriteString(m.styles.fail.Render("cancelled") + "\n")
	}
	return tea.NewView(b.String())
}

func (m sendModel) hookText(h hookLine) string {
	text := h.phase + "  " + h.level + "  " + h.step
	switch h.status {
	case "":
		return m.spin.View() + " " + text
	case engine.StatusPassed:
		return m.styles.ok.Render("✓") + " " + text + "  " + m.styles.dim.Render(round(h.duration).String())
	}
	return m.styles.fail.Render("✗") + " " + text
}

func (m sendModel) requestText(r requestLine) string {
	target := r.method + " " + r.url
	if !r.done {
		text := m.spin.View() + " " + target
		if r.attempt > 1 {
			text += "  attempt " + fmt.Sprint(r.attempt)
		}
		if r.retried != "" {
			text += "  " + m.styles.dim.Render("("+r.retried+")")
		}
		return text
	}
	if r.err != "" {
		return m.styles.fail.Render("✗") + " " + target + " → " + r.err
	}
	mark := m.styles.ok.Render("✓")
	if r.status >= http.StatusBadRequest {
		mark = m.styles.fail.Render("✓")
	}
	return mark + " " + target + " → " + fmt.Sprint(r.status) + "  " + m.styles.dim.Render(round(r.duration).String())
}

func round(d time.Duration) time.Duration {
	if d >= time.Millisecond {
		return d.Round(time.Millisecond)
	}
	return d.Round(time.Microsecond)
}
