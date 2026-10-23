// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// reportSink is a fake telemetry endpoint.
type reportSink struct {
	mu      sync.Mutex
	reports []string
}

func newReportSink(t *testing.T) (*reportSink, string) {
	t.Helper()
	s := &reportSink{}
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.reports = append(s.reports, string(data))
		s.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func (s *reportSink) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reports...)
}

func TestTelemetry_Given_off_by_default_Then_nothing_is_sent(t *testing.T) {
	e := newSendEnv(t)
	sink, url := newReportSink(t)
	e.environ = append(e.environ, "FLOWI_TELEMETRY_ENDPOINT="+url)

	code, _, errOut := e.run(context.Background(), "send", "auth/login", "--env", "local", "--with", "email=a@example.com")

	if code != 0 || len(sink.all()) != 0 {
		t.Fatalf("exit %d, %d reports\n%s", code, len(sink.all()), errOut)
	}
}

func TestTelemetry_Given_minimal_Then_one_report_with_only_the_start_event(t *testing.T) {
	e := newSendEnv(t)
	sink, url := newReportSink(t)
	e.environ = append(e.environ, "FLOWI_TELEMETRY=minimal", "FLOWI_TELEMETRY_ENDPOINT="+url)

	code, _, errOut := e.run(context.Background(), "send", "auth/login", "--env", "local", "--with", "email=a@example.com")

	reports := sink.all()
	if code != 0 || len(reports) != 1 {
		t.Fatalf("exit %d, %d reports\n%s", code, len(reports), errOut)
	}
	if !strings.Contains(reports[0], `"app_started"`) || strings.Contains(reports[0], "command_run") || strings.Contains(reports[0], "run_finished") {
		t.Fatalf("report %s", reports[0])
	}
}

func TestTelemetry_Given_full_Then_command_and_run_are_reported_without_names(t *testing.T) {
	e := newSendEnv(t)
	sink, url := newReportSink(t)
	e.environ = append(e.environ, "FLOWI_TELEMETRY=full", "FLOWI_TELEMETRY_ENDPOINT="+url)

	code, _, errOut := e.run(context.Background(), "send", "auth/login", "--env", "local", "--with", "email=a@example.com")

	reports := sink.all()
	if code != 0 || len(reports) != 1 {
		t.Fatalf("exit %d, %d reports\n%s", code, len(reports), errOut)
	}
	var p struct {
		Level  string
		Client map[string]string
		Events []struct {
			Name   string
			Fields map[string]string
		}
	}
	if err := json.Unmarshal([]byte(reports[0]), &p); err != nil {
		t.Fatal(err)
	}
	names := map[string]map[string]string{}
	for _, ev := range p.Events {
		names[ev.Name] = ev.Fields
	}
	if p.Level != "full" || p.Client["app"] != "cli" || names["command_run"]["command"] != "send" || names["command_run"]["exit"] != "ok" ||
		names["run_finished"]["kind"] != "send" || names["run_finished"]["status"] != "passed" {
		t.Fatalf("report %s", reports[0])
	}
	for _, secret := range []string{"auth/login", "a@example.com", "local", demoPassword, "127.0.0.1"} {
		if strings.Contains(reports[0], secret) {
			t.Fatalf("report contains %q: %s", secret, reports[0])
		}
	}
}

func TestTelemetry_Given_full_without_endpoint_Then_nothing_is_sent_and_the_command_still_succeeds(t *testing.T) {
	e := newSendEnv(t)
	e.environ = append(e.environ, "FLOWI_TELEMETRY=full")

	code, _, errOut := e.run(context.Background(), "send", "auth/login", "--env", "local", "--with", "email=a@example.com")

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}

func TestTelemetry_Given_an_unreachable_endpoint_Then_the_command_is_unaffected(t *testing.T) {
	e := newSendEnv(t)
	e.environ = append(e.environ, "FLOWI_TELEMETRY=minimal", "FLOWI_TELEMETRY_ENDPOINT=http://127.0.0.1:1")

	code, _, errOut := e.run(context.Background(), "send", "auth/login", "--env", "local", "--with", "email=a@example.com")

	if code != 0 || strings.Contains(errOut, "telemetry") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}

func TestTelemetryShow_should_print_the_sample_payload_of_the_level(t *testing.T) {
	e := newSendEnv(t)
	e.environ = append(e.environ, "FLOWI_TELEMETRY=full")

	code, out, errOut := e.run(context.Background(), "telemetry", "show")

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	for _, want := range []string{`"level": "full"`, `"app_started"`, `"command_run"`, `"run_finished"`} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %s:\n%s", want, out)
		}
	}
}

func TestTelemetryShow_Given_level_flag_Then_that_level_is_previewed(t *testing.T) {
	e := newSendEnv(t)

	code, out, _ := e.run(context.Background(), "telemetry", "show", "--level", "minimal")

	if code != 0 || !strings.Contains(out, `"level": "minimal"`) || strings.Contains(out, "command_run") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestTelemetryShow_Given_bad_level_Then_usage_error(t *testing.T) {
	e := newSendEnv(t)

	code, _, errOut := e.run(context.Background(), "telemetry", "show", "--level", "everything")

	if code != 2 || !strings.Contains(errOut, "everything") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}

func TestTelemetryStatus_should_explain_and_say_what_is_set(t *testing.T) {
	e := newSendEnv(t)

	code, out, _ := e.run(context.Background(), "telemetry", "status")

	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"Telemetry is off", "level: off", "endpoint: (none)", "flowi config set telemetry"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

func TestTelemetryEvents_should_list_the_catalogue(t *testing.T) {
	e := newSendEnv(t)

	code, out, _ := e.run(context.Background(), "telemetry", "events")

	if code != 0 || !strings.Contains(out, "command_run") || !strings.Contains(out, "run_finished") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestTelemetry_Given_the_command_turns_it_off_Then_nothing_is_sent(t *testing.T) {
	e := newSendEnv(t)
	sink, url := newReportSink(t)
	writeFile(t, filepath.Join(e.dir, "config", "settings.yaml"), "telemetry: full\ntelemetry_endpoint: "+url+"\n")

	code, _, errOut := e.run(context.Background(), "config", "set", "telemetry", "off")

	if code != 0 || len(sink.all()) != 0 {
		t.Fatalf("exit %d, %d reports sent by the command that opts out\n%s", code, len(sink.all()), errOut)
	}
}
