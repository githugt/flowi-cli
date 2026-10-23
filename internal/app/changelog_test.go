// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/githugt/flowi-lib/changelog"

	flowicli "github.com/githugt/flowi-cli"
)

func TestEmbeddedChangelog_should_parse_into_at_least_one_release_with_entries(t *testing.T) {
	rs := changelog.Parse(flowicli.Changelog())

	if len(rs) == 0 || len(rs[0].Sections) == 0 || len(rs[0].Sections[0].Entries) == 0 {
		t.Fatalf("CHANGELOG.md does not parse into releases: %+v", rs)
	}
}

func TestChangelog_should_print_the_latest_release_by_default(t *testing.T) {
	e := newSendEnv(t)

	code, out, errOut := e.run(context.Background(), "changelog")

	if code != 0 || !strings.Contains(out, "Unreleased") || !strings.Contains(out, "• ") || strings.Contains(out, "**") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

func TestChangelog_Given_all_Then_every_release(t *testing.T) {
	e := newSendEnv(t)
	releases := len(changelog.Parse(flowicli.Changelog()))

	code, out, _ := e.run(context.Background(), "changelog", "--all", "--output", "json")

	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || len(got) != releases {
		t.Fatalf("exit %d err %v releases %d/%d\n%s", code, err, len(got), releases, out)
	}
}

func TestChangelog_Given_since_an_unknown_version_Then_usage_error(t *testing.T) {
	e := newSendEnv(t)

	code, _, errOut := e.run(context.Background(), "changelog", "--since", "9.9.9")

	if code != 2 || !strings.Contains(errOut, "9.9.9") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}

func TestChangelog_Given_a_bad_count_Then_usage_error(t *testing.T) {
	e := newSendEnv(t)

	code, _, errOut := e.run(context.Background(), "changelog", "-n", "0")

	if code != 2 || !strings.Contains(errOut, "-n") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}
