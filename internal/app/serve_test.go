// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The script tests stop a running serve with Ctrl-C, which they cannot send on
// Windows; cancelling the command's context is what a signal does, so this runs
// the same drain on every platform.
func TestServe_Given_a_running_server_When_the_context_is_cancelled_Then_it_stops_cleanly_and_exits_0(t *testing.T) {
	e := newSendEnv(t)
	addr := freeLocalAddr(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type outcome struct {
		code     int
		out, err string
	}
	done := make(chan outcome, 1)
	go func() {
		code, out, errOut := e.run(ctx, "serve", "--replay-addr", addr, "--trigger-addr", "127.0.0.1:0")
		done <- outcome{code, out, errOut}
	}()
	waitUntilAnswers(t, "http://"+addr+"/__flowi/routes")

	cancel()
	var got outcome
	select {
	case got = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not stop within 30s of the cancel")
	}

	if got.code != 0 || !strings.Contains(got.out, "Stopped") || !strings.Contains(got.out, "triggers are off") {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", got.code, got.out, got.err)
	}
}

// freeLocalAddr is a 127.0.0.1 address nothing listens on.
func freeLocalAddr(t *testing.T) string {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// waitUntilAnswers polls url until it answers with any status.
func waitUntilAnswers(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res, err := http.DefaultClient.Do(req); err == nil {
			_ = res.Body.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never answered", url)
		}
		<-time.After(50 * time.Millisecond) // the server starts in another goroutine
	}
}
