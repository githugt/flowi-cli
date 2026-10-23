// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/githugt/flowi-lib/engine"
)

func feedRun(m runModel, events ...engine.Event) runModel {
	for _, ev := range events {
		next, _ := m.Update(eventMsg{ev})
		m = next.(runModel)
	}
	return m
}

func lines(m runModel) []string {
	return strings.Split(strings.TrimRight(m.View().Content, "\n"), "\n")
}

func started(path, parent, lane, typ string) engine.NodeStarted {
	id := path[strings.LastIndex(path, "/")+1:]
	return engine.NodeStarted{Path: path, Parent: parent, Lane: lane, Node: id, Type: typ}
}

func finished(path, parent, lane string, s engine.Status, d time.Duration) engine.NodeFinished {
	return engine.NodeFinished{Path: path, Parent: parent, Lane: lane, Status: s, Duration: d}
}

func TestRunModel_should_render_tree_with_running_and_finished_nodes(t *testing.T) {
	m := feedRun(newRunModel(Options{}, 40),
		engine.RunStarted{RunID: "r", Kind: "flow"},
		started("login", "", "", "request"),
		finished("login", "", "", engine.StatusPassed, 120*time.Millisecond),
		started("verify", "", "", "request"),
		engine.HookStarted{Path: "verify", Level: "request", Phase: "pre", Step: "hooks/fetch-config"},
		started("verify/pre:request:0/fetch", "verify", "pre:request:0", "request"),
		started("par", "", "", "parallel"),
		started("par/a/x", "par", "a", "note"),
		finished("par/a/x", "par", "a", engine.StatusFailed, time.Millisecond),
	)

	got := lines(m)

	want := []string{"✓ login  120ms", "verify", "  pre  request  hooks/fetch-config", "par", "  ✗ par/a/x  1ms"}
	if len(got) != len(want) {
		t.Fatalf("view:\n%s", strings.Join(got, "\n"))
	}
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("line %d %q lacks %q", i, got[i], w)
		}
	}
	if strings.Contains(strings.Join(got, "\n"), "pre:request:0/fetch") {
		t.Error("nodes inside hooks must fold into their hook line")
	}
}

func TestRunModel_should_collapse_batch_iterations_to_counts(t *testing.T) {
	m := feedRun(newRunModel(Options{}, 40),
		started("users", "", "", "batch"),
		started("users[0]/create", "users", "[0]", "fetch"),
		finished("users[0]/create", "users", "[0]", engine.StatusPassed, time.Millisecond),
		started("users[1]/create", "users", "[1]", "fetch"),
		finished("users[1]/create", "users", "[1]", engine.StatusFailed, time.Millisecond),
		started("users[2]/create", "users", "[2]", "fetch"),
	)

	got := lines(m)

	if len(got) != 2 || !strings.Contains(got[0], "users") || !strings.Contains(got[0], "[✓1 ✗1 ⋯1]") || !strings.Contains(got[1], "users[2]/create") {
		t.Fatalf("view:\n%s", strings.Join(got, "\n"))
	}
}

func TestRunModel_should_truncate_to_height(t *testing.T) {
	m := newRunModel(Options{}, 10)
	for i := range 20 {
		p := "n" + string(rune('a'+i))
		m = feedRun(m, started(p, "", "", "note"), finished(p, "", "", engine.StatusPassed, 0))
	}

	got := lines(m)

	if len(got) != 6 || !strings.Contains(got[5], "… 15 more") {
		t.Fatalf("view (%d lines):\n%s", len(got), strings.Join(got, "\n"))
	}
}

func TestRunModel_Given_cancel_and_abort_Then_says_so(t *testing.T) {
	m := feedRun(newRunModel(Options{}, 40), started("a", "", "", "note"), engine.RunCancelled{}, engine.RunAborted{Inflight: []string{"a"}})

	got := strings.Join(lines(m), "\n")

	if !strings.Contains(got, "aborted") {
		t.Fatalf("view:\n%s", got)
	}
}

func TestRunFlow_should_consume_every_event_and_return_when_stream_closes(t *testing.T) {
	events := make(chan engine.Event, 4)
	events <- engine.RunStarted{}
	events <- started("a", "", "", "note")
	events <- finished("a", "", "", engine.StatusPassed, 0)
	events <- engine.RunFinished{Status: engine.StatusPassed}
	close(events)
	var out bytes.Buffer
	done := make(chan error, 1)

	var seen []engine.Event
	go func() {
		var err error
		seen, err = RunFlow(events, &out, Options{Width: 100})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunFlow did not return")
	}
	if len(seen) != 4 {
		t.Fatalf("returned %d events", len(seen))
	}
}
