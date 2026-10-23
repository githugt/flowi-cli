// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/githugt/flowi-lib/changelog"

	flowicli "github.com/githugt/flowi-cli"
)

func newChangelogCommand(g *GlobalOptions) *cobra.Command {
	var (
		count int
		all   bool
		since string
	)
	cmd := &cobra.Command{
		Use:   "changelog",
		Short: "Show what changed in flowi",
		Long:  "Prints the newest release of the changelog built into this binary. Use -n for more, --all for everything, or --since to see what is new after a version.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if count < 1 {
				return &UsageError{Err: fmt.Errorf("-n must be at least 1, got %d", count)}
			}
			releases, err := selectReleases(changelog.Parse(flowicli.Changelog()), count, all, since)
			if err != nil {
				return err
			}
			if g.Output == OutputJSON {
				return writeJSON(cmd.OutOrStdout(), jsonReleases(releases))
			}
			for n, r := range releases {
				if n > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				fmt.Fprint(cmd.OutOrStdout(), r.Text())
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&count, "count", "n", 1, "how many of the newest releases to show")
	cmd.Flags().BoolVar(&all, "all", false, "show every release")
	cmd.Flags().StringVar(&since, "since", "", "show the releases newer than this version")
	return cmd
}

func selectReleases(all []changelog.Release, count int, everything bool, since string) ([]changelog.Release, error) {
	switch {
	case since != "":
		newer, ok := changelog.Since(all, since)
		if !ok {
			return nil, &UsageError{Err: fmt.Errorf("version %q is not in the changelog", since)}
		}
		return newer, nil
	case everything:
		return all, nil
	}
	return changelog.Latest(all, count), nil
}

// jsonRelease is the stable JSON shape of a release.
type jsonRelease struct {
	Version    string        `json:"version"`
	Date       string        `json:"date,omitempty"`
	Unreleased bool          `json:"unreleased"`
	Sections   []jsonSection `json:"sections"`
}

type jsonSection struct {
	Title   string   `json:"title"`
	Entries []string `json:"entries"`
}

func jsonReleases(rs []changelog.Release) []jsonRelease {
	out := make([]jsonRelease, 0, len(rs))
	for _, r := range rs {
		j := jsonRelease{Version: r.Version, Unreleased: r.Unreleased, Sections: []jsonSection{}}
		if !r.Date.IsZero() {
			j.Date = r.Date.Format("2006-01-02")
		}
		for _, s := range r.Sections {
			j.Sections = append(j.Sections, jsonSection{Title: s.Title, Entries: s.Entries})
		}
		out = append(out, j)
	}
	return out
}
