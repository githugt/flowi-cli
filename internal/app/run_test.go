// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/samber/do/v2"

	"github.com/githugt/flowi-cli/internal/buildinfo"
	"github.com/githugt/flowi-cli/internal/cli"
)

// shutdownProbe records whether the injector shut it down.
type shutdownProbe struct{ called bool }

func (p *shutdownProbe) Shutdown(context.Context) error {
	p.called = true
	return nil
}

func withProbe(p *shutdownProbe) Option {
	return WithProvider(func(i do.Injector) { do.ProvideValue(i, p) })
}

func TestRun_should_print_version_and_exit_ok(t *testing.T) {
	var out, errOut bytes.Buffer

	code := Run(context.Background(), []string{"version"}, Stdio{Out: &out, Err: &errOut})

	if code != cli.ExitOK {
		t.Fatalf("exit code %d, stderr %q", code, errOut.String())
	}
	if want := "flowi " + buildinfo.Version(); !strings.Contains(out.String(), want) {
		t.Fatalf("stdout %q does not contain %q", out.String(), want)
	}
}

func TestRun_should_return_usage_exit_code_for_unknown_command(t *testing.T) {
	var out, errOut bytes.Buffer

	code := Run(context.Background(), []string{"nope"}, Stdio{Out: &out, Err: &errOut})

	if code != cli.ExitUsage {
		t.Fatalf("exit code %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("stderr %q does not mention unknown command", errOut.String())
	}
}

func TestRun_Given_cancelled_context_When_run_Then_shuts_down_injector_and_returns_cancelled(t *testing.T) {
	probe := &shutdownProbe{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer

	code := Run(ctx, []string{"version"}, Stdio{Out: &out, Err: &errOut}, withProbe(probe))

	if code != cli.ExitCancelled {
		t.Fatalf("exit code %d, want %d", code, cli.ExitCancelled)
	}
	if !probe.called {
		t.Fatal("injector was not shut down")
	}
}

func TestRun_should_shut_down_injector_after_successful_command(t *testing.T) {
	probe := &shutdownProbe{}
	var out, errOut bytes.Buffer

	code := Run(context.Background(), []string{"version"}, Stdio{Out: &out, Err: &errOut}, withProbe(probe))

	if code != cli.ExitOK {
		t.Fatalf("exit code %d", code)
	}
	if !probe.called {
		t.Fatal("injector was not shut down")
	}
}

// blockingProbe never finishes shutting down on its own.
type blockingProbe struct{ release chan struct{} }

func (p *blockingProbe) Shutdown(context.Context) error {
	<-p.release
	return nil
}

func TestRun_should_abandon_shutdown_after_timeout(t *testing.T) {
	probe := &blockingProbe{release: make(chan struct{})}
	t.Cleanup(func() { close(probe.release) })
	var out, errOut bytes.Buffer
	done := make(chan int, 1)

	go func() {
		done <- Run(context.Background(), []string{"version"}, Stdio{Out: &out, Err: &errOut},
			WithProvider(func(i do.Injector) { do.ProvideValue(i, probe) }),
			withShutdownTimeout(50*time.Millisecond))
	}()

	select {
	case code := <-done:
		if code != cli.ExitOK {
			t.Fatalf("exit code %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return: shutdown is not bounded")
	}
	if !strings.Contains(errOut.String(), "Event=ShutdownAbandoned") {
		t.Fatalf("stderr %q", errOut.String())
	}
}
