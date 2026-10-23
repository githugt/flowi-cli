// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// LoggerName is the injector name of the settings-aware process logger.
const LoggerName = "flowi.logger"

// Console is the process's standard streams and whether each is a
// terminal. Interactive output (live progress, prompts) is used only on
// terminals, so pipes and CI logs stay plain.
type Console struct {
	In     io.Reader
	Err    io.Writer
	InTTY  bool
	OutTTY bool
	ErrTTY bool
	// NoColor is set when NO_COLOR is present in the environment.
	NoColor bool
	// Done is closed when the command is cancelled (Ctrl-C); a prompt
	// waiting on the terminal then gives up.
	Done <-chan struct{}
}

// NewConsole inspects the streams. Only *os.File streams can be terminals.
func NewConsole(in io.Reader, out, errOut io.Writer, environ []string, done <-chan struct{}) Console {
	if in == nil {
		in = strings.NewReader("")
	}
	return Console{
		In: in, Err: errOut, Done: done,
		InTTY: isTerminal(in), OutTTY: isTerminal(out), ErrTTY: isTerminal(errOut),
		NoColor: hasEnvVar(environ, "NO_COLOR"),
	}
}

func isTerminal(s any) bool {
	f, ok := s.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) //nolint:gosec // file descriptors fit in int
}

func hasEnvVar(environ []string, name string) bool {
	for _, kv := range environ {
		if strings.HasPrefix(kv, name+"=") {
			return true
		}
	}
	return false
}

// errNoInput means a value was needed but standard input had none.
var errNoInput = errors.New("no input on standard input")

// ReadSecret reads a value without echoing it on a terminal; otherwise it
// reads one line from standard input (so scripts can pipe it in).
func (c Console) ReadSecret(prompt string) (string, error) {
	if c.InTTY {
		_, _ = fmt.Fprint(c.Err, prompt) // the prompt is best effort; reading is what matters
		fd := int(c.In.(*os.File).Fd())  //nolint:gosec // file descriptors fit in int
		state, err := term.GetState(fd)
		if err != nil {
			return "", fmt.Errorf("reading from terminal: %w", err)
		}
		value, err := readCancelable(func() ([]byte, error) { return term.ReadPassword(fd) },
			c.Done, func() { _ = term.Restore(fd, state) }) // best effort: the process is ending
		_, _ = fmt.Fprintln(c.Err)
		if err != nil && !errors.Is(err, context.Canceled) {
			return "", fmt.Errorf("reading from terminal: %w", err)
		}
		return value, err
	}
	line, err := bufio.NewReader(c.In).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading standard input: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" && errors.Is(err, io.EOF) {
		return "", errNoInput
	}
	return line, nil
}

// readCancelable runs a blocking terminal read, giving up when done is
// closed: the terminal is restored (echo back on) and context.Canceled is
// returned. Ctrl-C would otherwise only cancel after Enter.
//
// Goroutine ownership: the read runs on its own goroutine. When cancelled
// it stays blocked in the read until the process exits, which follows.
func readCancelable(read func() ([]byte, error), done <-chan struct{}, restore func()) (string, error) {
	type result struct {
		value []byte
		err   error
	}
	got := make(chan result, 1)
	go func() {
		v, err := read()
		got <- result{v, err}
	}()
	select {
	case r := <-got:
		return string(r.value), r.err
	case <-done:
		restore()
		return "", context.Canceled
	}
}

// Passphrase returns the vault passphrase prompt, or nil when there is no
// terminal to ask on (FLOWI_VAULT_KEY must be set then).
func (c Console) Passphrase() func() (string, error) {
	if !c.InTTY {
		return nil
	}
	return func() (string, error) { return c.ReadSecret("Vault passphrase: ") }
}
