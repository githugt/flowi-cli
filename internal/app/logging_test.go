// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githugt/flowi-lib/settings"
)

func resolved(t *testing.T, values map[string]any) settings.Resolved {
	t.Helper()
	return settings.Resolve(settings.Schema(), settings.LayerValues{Layer: settings.LayerFlag, Source: "test", Values: values})
}

func TestNewLogger_should_write_json_to_file(t *testing.T) {
	file := filepath.Join(t.TempDir(), "flowi.log")
	r := resolved(t, map[string]any{"logging.format": "json", "logging.file": file, "logging.level": "debug"})

	logger, closeLog, err := NewLogger(r, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("hello", "Event", "Test")
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(file)
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &line); err != nil {
		t.Fatalf("not JSON: %q", data)
	}
	if line["Event"] != "Test" || line["level"] != "DEBUG" {
		t.Fatalf("got %v", line)
	}
}

func TestNewLogger_should_default_to_text_on_stderr_at_info(t *testing.T) {
	var stderr bytes.Buffer
	logger, closeLog, err := NewLogger(resolved(t, nil), &stderr)
	if err != nil {
		t.Fatal(err)
	}

	logger.Debug("hidden")
	logger.Info("shown", "Event", "Visible")
	_ = closeLog()

	if strings.Contains(stderr.String(), "hidden") || !strings.Contains(stderr.String(), "Event=Visible") {
		t.Fatalf("got %q", stderr.String())
	}
}
