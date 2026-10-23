// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"testing"

	"github.com/samber/do/v2"
)

func TestCommandName_should_know_every_top_level_command(t *testing.T) {
	i := do.New()
	do.ProvideValue(i, &GlobalOptions{})

	for _, c := range NewRootCommand(i).Commands() {
		if got := commandName("flowi " + c.Name() + " sub"); got != c.Name() {
			t.Errorf("%q is not in the telemetry catalogue (mapped to %q); add it to telemetry.CommandNames", c.Name(), got)
		}
	}
}

func TestCommandName_should_not_pass_arguments_through(t *testing.T) {
	if got := commandName("flowi https://secret.example"); got != "other" {
		t.Fatalf("got %q", got)
	}
	if got := commandName("flowi"); got != "other" {
		t.Fatalf("got %q", got)
	}
}

func TestExitName(t *testing.T) {
	want := map[int]string{ExitOK: "ok", ExitFailed: "failed", ExitUsage: "usage", ExitCancelled: "cancelled", ExitSystem: "error", 99: "error"}
	for code, name := range want {
		if got := exitName(code); got != name {
			t.Errorf("exitName(%d) = %q, want %q", code, got, name)
		}
	}
}
