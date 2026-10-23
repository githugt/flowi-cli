// SPDX-License-Identifier: AGPL-3.0-or-later

// Command flowi is the Flow-I command-line tool.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/githugt/flowi-cli/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first signal, restore default handling so a second Ctrl-C
	// ends the process at once instead of waiting for shutdown.
	context.AfterFunc(ctx, stop)
	code := app.Run(ctx, os.Args[1:], app.Stdio{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	stop()
	os.Exit(code)
}
