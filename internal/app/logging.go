// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/githugt/flowi-lib/settings"
)

const logFileMode = 0o600 // logs may contain request details; keep them private

// NewLogger builds the process logger from resolved settings: level and
// format from logging.level / logging.format, destination logging.file
// (appended) or stderr. The returned func closes the log file.
func NewLogger(r settings.Resolved, stderr io.Writer) (*slog.Logger, func() error, error) {
	level := slog.LevelInfo
	if v, ok := r.Get("logging.level"); ok {
		if err := level.UnmarshalText([]byte(fmt.Sprint(v.Value))); err != nil {
			return nil, nil, fmt.Errorf("logging.level: %w", err)
		}
	}
	out, closeOut, err := logDestination(r, stderr)
	if err != nil {
		return nil, nil, err
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler = slog.NewTextHandler(out, opts)
	if v, _ := r.Get("logging.format"); v.Value == "json" {
		handler = slog.NewJSONHandler(out, opts)
	}
	return slog.New(handler), closeOut, nil
}

func logDestination(r settings.Resolved, stderr io.Writer) (io.Writer, func() error, error) {
	v, _ := r.Get("logging.file")
	file, _ := v.Value.(string)
	if file == "" {
		return stderr, func() error { return nil }, nil
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFileMode)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file %s: %w", file, err)
	}
	return f, f.Close, nil
}
