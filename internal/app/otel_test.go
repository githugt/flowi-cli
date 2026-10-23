// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// otlpSink is a fake OTLP/HTTP collector that keeps what it receives.
type otlpSink struct {
	mu     sync.Mutex
	bodies []string
	paths  []string
}

func newOTLPSink(t *testing.T) (*otlpSink, string) {
	t.Helper()
	s := &otlpSink{}
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies, s.paths = append(s.bodies, string(data)), append(s.paths, r.URL.Path)
		s.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func (s *otlpSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func TestSend_Given_an_otel_endpoint_Then_the_trace_is_flushed_before_exit(t *testing.T) {
	e := newSendEnv(t)
	sink, url := newOTLPSink(t)
	e.environ = append(e.environ, "FLOWI_OTEL_ENDPOINT="+url)

	code, _, errOut := e.run(context.Background(), "send", "auth/login", "--env", "local", "--with", "email=a@example.com")

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if sink.count() != 1 || sink.paths[0] != "/v1/traces" || !strings.Contains(sink.bodies[0], `"name":"send"`) {
		t.Fatalf("collector got %d bodies %v", sink.count(), sink.bodies)
	}
	if strings.Contains(sink.bodies[0], demoPassword) {
		t.Fatal("password in trace")
	}
}

func TestSend_Given_no_otel_endpoint_Then_nothing_is_exported(t *testing.T) {
	e := newSendEnv(t)
	sink, _ := newOTLPSink(t)

	code, _, errOut := e.run(context.Background(), "send", "auth/login", "--env", "local", "--with", "email=a@example.com")

	if code != 0 || sink.count() != 0 {
		t.Fatalf("exit %d, collector got %d\n%s", code, sink.count(), errOut)
	}
}
