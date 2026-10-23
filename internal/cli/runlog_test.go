// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/githugt/flowi-lib/engine"
)

func TestLogRunAborted_should_write_the_exact_line(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	logRunAborted(logger, engine.RunAborted{RunID: "R1", Reason: "grace_period_exceeded", Grace: 60 * time.Second, Inflight: []string{"verify", "form-page-2"}}, "F1")

	want := "Event=RunAborted reason=grace_period_exceeded grace=60s inflight=[verify,form-page-2] RunID=R1 FlowID=F1"
	if !strings.Contains(buf.String(), want) || !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestFormatGrace_should_print_whole_seconds_as_seconds(t *testing.T) {
	for in, want := range map[time.Duration]string{60 * time.Second: "60s", 90 * time.Second: "90s", 1500 * time.Millisecond: "1.5s"} {
		if got := formatGrace(in); got != want {
			t.Errorf("formatGrace(%v) = %q, want %q", in, got, want)
		}
	}
}
