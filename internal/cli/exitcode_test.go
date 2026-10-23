// SPDX-License-Identifier: AGPL-3.0-or-later

package cli_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/githugt/flowi-cli/internal/cli"
)

func TestExitCodeFor_should_map_results_to_exit_codes(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want int
	}{
		{"success", context.Background(), nil, cli.ExitOK},
		{"cancelled wins over the error it caused", cancelled, errors.New("context canceled"), cli.ExitCancelled},
		{"cancelled without an error", cancelled, nil, cli.ExitCancelled},
		// Before: a server stopped with Ctrl-C always exited 3, which
		// orchestrators read as a failure.
		{"stopped on request is success even when cancelled", cancelled, &cli.StoppedError{}, cli.ExitOK},
		{"wrapped stop", cancelled, fmt.Errorf("serve: %w", &cli.StoppedError{}), cli.ExitOK},
		{"explicit exit code", context.Background(), cli.Exit(cli.ExitFailed), cli.ExitFailed},
		{"usage", context.Background(), &cli.UsageError{Err: errors.New("bad flag")}, cli.ExitUsage},
		{"anything else is a system failure", context.Background(), errors.New("disk on fire"), cli.ExitSystem},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cli.ExitCodeFor(c.ctx, c.err); got != c.want {
				t.Fatalf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestIsReported_should_cover_exit_and_stop_errors(t *testing.T) {
	if !cli.IsReported(cli.Exit(3)) || !cli.IsReported(&cli.StoppedError{}) || cli.IsReported(errors.New("x")) {
		t.Fatal("IsReported misclassifies")
	}
}
