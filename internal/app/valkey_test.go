// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/samber/do/v2"

	"github.com/githugt/flowi-cli/internal/cli"
)

// runWithInput is run with standard input.
func (e *sendEnv) runWithInput(input string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	env := cli.Env{Environ: e.environ, Home: e.dir, Cwd: e.dir}
	code := Run(context.Background(), args, Stdio{In: strings.NewReader(input), Out: &out, Err: &errOut}, WithProvider(func(i do.Injector) { do.OverrideValue(i, env) }))
	return code, out.String(), errOut.String()
}

func TestServeValkeyPassword_should_store_the_password_from_standard_input_without_echoing_it(t *testing.T) {
	e := newSendEnv(t)

	code, out, errOut := e.runWithInput("s3cret-pw\n", "serve", "valkey-password")

	if code != 0 || strings.Contains(out+errOut, "s3cret-pw") || !strings.Contains(errOut, "stored") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

func TestServeValkeyPassword_Given_nothing_on_standard_input_Then_usage_error(t *testing.T) {
	e := newSendEnv(t)

	code, _, errOut := e.runWithInput("\n", "serve", "valkey-password")

	if code != 2 || !strings.Contains(errOut, "standard input") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}

func TestServe_Given_a_malformed_valkey_address_Then_usage_error(t *testing.T) {
	e := newSendEnv(t)

	code, _, errOut := e.run(context.Background(), "serve", "--valkey-addr", "nonsense", "--no-replay", "--no-triggers")

	if code != 2 || !strings.Contains(errOut, "host:port") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}
