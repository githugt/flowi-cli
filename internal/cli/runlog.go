// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/samber/do/v2"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/engine"
)

// logRun logs the run's outcome, and the abort when there was one. No
// URLs, values or bodies are logged: they may hold secrets masking did not
// know about.
func logRun(i do.Injector, t flowi.FlowTarget, res engine.RunResult) {
	logger, err := do.InvokeNamed[*slog.Logger](i, LoggerName)
	if err != nil {
		return // logging problems are reported once by the app and never change the outcome
	}
	if a := res.Report.Aborted; a != nil {
		logRunAborted(logger, engine.RunAborted{RunID: res.RunID, Reason: a.Reason, Grace: a.Grace, Inflight: a.Inflight}, string(t.Flow))
	}
	logger.Debug("run finished", "Event", "RunFinished", "RunID", res.RunID, "FlowID", t.Flow, "Status", res.Status)
}

// logRunAborted writes design §7.8's line. The in-flight list is joined
// with "," (no spaces) so slog's text handler does not quote it, and the
// grace is printed in seconds as the user configured it (60s, not 1m0s).
func logRunAborted(l *slog.Logger, ev engine.RunAborted, flowID string) {
	l.Warn("run aborted",
		"Event", "RunAborted", "reason", ev.Reason, "grace", formatGrace(ev.Grace),
		"inflight", "["+strings.Join(ev.Inflight, ",")+"]", "RunID", ev.RunID, "FlowID", flowID)
}

func formatGrace(d time.Duration) string {
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", int64(d/time.Second))
	}
	return d.String()
}
