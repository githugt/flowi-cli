// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/githugt/flowi-lib/engine"
)

func feed(m sendModel, events ...engine.Event) sendModel {
	for _, ev := range events {
		next, _ := m.Update(eventMsg{ev})
		m = next.(sendModel)
	}
	return m
}

func view(m sendModel) string { return m.View().Content }

func TestSendModel_should_render_hook_tree_in_order(t *testing.T) {
	m := feed(newSendModel(Options{}),
		engine.RunStarted{RunID: "r"},
		engine.HookStarted{Level: "collection", Phase: "pre", Step: "inline"},
		engine.HookFinished{Level: "collection", Phase: "pre", Step: "inline", Status: engine.StatusPassed, Duration: 2 * time.Millisecond},
		engine.HookStarted{Level: "request", Phase: "pre", Step: "hooks/sign"},
		engine.HookFinished{Level: "request", Phase: "pre", Step: "hooks/sign", Status: engine.StatusFailed},
		engine.RequestSent{Method: "POST", URL: "https://api.example/login", Attempt: 1},
		engine.ResponseReceived{Status: 200, Attempt: 1, Duration: 120 * time.Millisecond},
		engine.HookStarted{Level: "version", Phase: "post", Step: "inline"},
	)

	got := view(m)

	lines := strings.Split(strings.TrimSpace(got), "\n")
	want := []string{"✓ pre  collection  inline  2ms", "✗ pre  request  hooks/sign", "✓ POST https://api.example/login → 200  120ms", "post  version  inline"}
	if len(lines) != len(want) {
		t.Fatalf("view:\n%s", got)
	}
	for i, w := range want {
		if !strings.Contains(lines[i], w) {
			t.Errorf("line %d %q lacks %q", i, lines[i], w)
		}
	}
}

func TestSendModel_should_show_retry_attempts(t *testing.T) {
	m := feed(newSendModel(Options{}),
		engine.RequestSent{Method: "GET", URL: "https://api.example/x", Attempt: 1},
		engine.ResponseReceived{Status: 503, Attempt: 1, Wait: 400 * time.Millisecond},
		engine.RequestSent{Method: "GET", URL: "https://api.example/x", Attempt: 2},
	)

	got := view(m)

	if !strings.Contains(got, "attempt 2") || !strings.Contains(got, "503 after attempt 1, waited 400ms") {
		t.Fatalf("view:\n%s", got)
	}
}

func TestSendModel_Given_cancel_Then_shows_cancelled_line(t *testing.T) {
	m := feed(newSendModel(Options{}),
		engine.RequestSent{Method: "GET", URL: "https://api.example/slow", Attempt: 1},
		engine.RunCancelled{RunID: "r"},
	)

	got := view(m)

	if !strings.Contains(got, "cancelled") {
		t.Fatalf("view:\n%s", got)
	}
}

func TestSendModel_Given_stream_closed_Then_clears_and_quits(t *testing.T) {
	m := feed(newSendModel(Options{}), engine.HookStarted{Level: "collection", Phase: "pre", Step: "inline"})

	next, cmd := m.Update(closedMsg{})

	if view(next.(sendModel)) != "" || cmd == nil {
		t.Fatalf("view after close: %q", view(next.(sendModel)))
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("closing the event stream should quit")
	}
}

func TestRunSend_should_consume_every_event_and_return_when_stream_closes(t *testing.T) {
	events := make(chan engine.Event, 4)
	events <- engine.RunStarted{}
	events <- engine.RequestSent{Method: "GET", URL: "u", Attempt: 1}
	events <- engine.ResponseReceived{Status: 204, Attempt: 1}
	events <- engine.RunFinished{Status: engine.StatusPassed}
	close(events)
	var out bytes.Buffer
	done := make(chan error, 1)

	var seen []engine.Event
	go func() {
		var err error
		seen, err = RunSend(events, &out, Options{Width: 100})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunSend did not return")
	}
	if len(seen) != 4 {
		t.Fatalf("returned %d events", len(seen))
	}
}

func TestChooseRenderer(t *testing.T) {
	tests := []struct {
		output                string
		quiet, outTTY, errTTY bool
		want                  Renderer
	}{
		{"json", false, true, true, RendererJSON},
		{"text", false, true, true, RendererLive},
		{"text", true, true, true, RendererText},
		{"text", false, false, true, RendererText},
		{"text", false, true, false, RendererText},
		{"text", false, false, false, RendererText},
	}
	for _, tt := range tests {
		if got := ChooseRenderer(tt.output, tt.quiet, tt.outTTY, tt.errTTY); got != tt.want {
			t.Errorf("%+v: got %v", tt, got)
		}
	}
}

func TestSendModel_Given_hook_flow_node_events_Then_folded_into_hook_line(t *testing.T) {
	m := feed(newSendModel(Options{}),
		engine.HookStarted{Level: "request", Phase: "pre", Step: "hooks/cfg"},
		engine.NodeStarted{Path: "pre:request:0/fetch", Node: "fetch", Type: "request", Depth: 1},
		engine.NodeFinished{Path: "pre:request:0/fetch", Node: "fetch", Status: engine.StatusPassed, Depth: 1},
	)

	got := strings.Split(strings.TrimSpace(view(m)), "\n")

	if len(got) != 1 || !strings.Contains(got[0], "pre  request  hooks/cfg") {
		t.Fatalf("view:\n%s", view(m))
	}
}
