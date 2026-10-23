// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	"github.com/githugt/flowi-lib/model"
	"github.com/githugt/flowi-lib/version"
)

// listSections are the parts of a collection "flowi ls" can show, in order.
func listSections() []string { return []string{"requests", "flows", "blueprints", "envs"} }

func newLsCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:       "ls [requests|flows|blueprints|envs]",
		Short:     "List the contents of mounted collections",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: listSections(),
		RunE: func(cmd *cobra.Command, args []string) error {
			sections := listSections()
			if len(args) == 1 {
				if !slices.Contains(sections, args[0]) {
					return &UsageError{Err: fmt.Errorf("unknown section %q (want requests, flows, blueprints or envs)", args[0])}
				}
				sections = args
			}
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			for _, c := range s.Workspace.Collections() {
				printCollection(cmd.OutOrStdout(), c, sections)
			}
			if slices.Contains(sections, "envs") {
				printEnvironments(cmd.OutOrStdout(), s.Workspace.Environments())
			}
			return nil
		},
	}
}

func printCollection(w io.Writer, c model.Collection, sections []string) {
	fmt.Fprintf(w, "%s (%s)\n", c.Manifest.Name, c.Manifest.ID)
	if slices.Contains(sections, "requests") && len(c.Requests) > 0 {
		fmt.Fprintln(w, "  Requests")
		printTree(w, entriesOf(c.Requests, func(r model.Request) (string, string) {
			return r.Path, fmt.Sprintf("%s  (%s)  [%s]", r.Name, r.Path, versionList(r))
		}))
	}
	if slices.Contains(sections, "flows") && len(c.Flows) > 0 {
		fmt.Fprintln(w, "  Flows")
		printTree(w, entriesOf(c.Flows, func(f model.Flow) (string, string) {
			return f.Path, fmt.Sprintf("%s  (%s)", f.Name, f.Path)
		}))
	}
	if slices.Contains(sections, "blueprints") && len(c.Blueprints) > 0 {
		fmt.Fprintln(w, "  Blueprints")
		printTree(w, entriesOf(c.Blueprints, func(b model.Blueprint) (string, string) {
			return b.Path, fmt.Sprintf("%s  (%s)", b.Name, b.Path)
		}))
	}
}

// printEnvironments lists the workspace's environments, which belong to
// no collection.
func printEnvironments(w io.Writer, envs []model.Environment) {
	if len(envs) == 0 {
		return
	}
	fmt.Fprintln(w, "Environments (workspace)")
	for _, e := range envs {
		fmt.Fprintf(w, "  %s\n", e.Name)
	}
}

type entry struct{ path, line string }

func entriesOf[T any](items []T, describe func(T) (string, string)) []entry {
	out := make([]entry, 0, len(items))
	for _, it := range items {
		p, line := describe(it)
		out = append(out, entry{p, line})
	}
	return out
}

// printTree prints top-level entries first, then one block per folder.
func printTree(w io.Writer, entries []entry) {
	byDir := map[string][]string{}
	for _, e := range entries {
		dir := path.Dir(e.path)
		byDir[dir] = append(byDir[dir], e.line)
	}
	for _, line := range byDir["."] {
		fmt.Fprintf(w, "    %s\n", line)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		if d != "." {
			dirs = append(dirs, d)
		}
	}
	slices.Sort(dirs)
	for _, d := range dirs {
		fmt.Fprintf(w, "    %s/\n", d)
		for _, line := range byDir[d] {
			fmt.Fprintf(w, "      %s\n", line)
		}
	}
}

// versionList shows versions in order with the effective default starred.
func versionList(r model.Request) string {
	labels := make([]version.Label, 0, len(r.Versions))
	for _, v := range r.Versions {
		if l, err := version.Parse(v.Label); err == nil {
			labels = append(labels, l)
		}
	}
	slices.SortFunc(labels, version.Label.Compare)
	q := version.Query{Available: labels}
	if d, err := version.Parse(r.Default); err == nil {
		q.Default = &d
	}
	chosen, _, _ := version.Resolve(q) // an invalid default is a validate error; here nothing is starred
	parts := make([]string, 0, len(labels))
	for _, l := range labels {
		s := l.String()
		if l.Compare(chosen) == 0 && chosen.String() != "" {
			s += "*"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}
