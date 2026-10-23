// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"slices"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/diag"
)

func newFmtCommand(i do.Injector) *cobra.Command {
	var check, force bool
	cmd := &cobra.Command{
		Use:   "fmt [dir]",
		Short: "Rewrite collection files in canonical form",
		Long: "Rewrite every mounted collection, or the collection in dir, in canonical form and repair\n" +
			"out-of-date path hints.\n\n" +
			"fmt never rewrites a file it could not fully understand (errors or unknown settings):\n" +
			"those are reported and skipped. The canonical form has no comments, so files with\n" +
			"comments are skipped too unless --force is given.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			dir := ""
			if len(args) == 1 {
				dir = args[0]
			}
			changes, blocked, err := s.Workspace.Format(dir)
			if err != nil {
				return err
			}
			r := fmtRun{cmd: cmd, cwd: do.MustInvoke[Env](i).Cwd, output: do.MustInvoke[*GlobalOptions](i).Output, check: check, force: force}
			return r.report(changes, blocked)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only list files that would change; exit 1 if any")
	cmd.Flags().BoolVar(&force, "force", false, "also rewrite files that contain comments (the comments are removed)")
	return cmd
}

type fmtRun struct {
	cmd          *cobra.Command
	cwd, output  string
	check, force bool
}

// report writes what can be written safely, then explains what was left
// alone. Exit status: 2 if any file was refused, 1 if --check found changes.
func (r fmtRun) report(changes []flowi.FileChange, blocked diag.List) error {
	if err := PrintDiagnostics(r.cmd.OutOrStdout(), r.output, r.cwd, blocked); err != nil {
		return err
	}
	for _, file := range blockedFiles(blocked) {
		r.cmd.Printf("skipped %s\n", displayPath(r.cwd, file))
	}
	write, keep := r.split(changes)
	if err := r.apply(write); err != nil {
		return err
	}
	for _, ch := range keep {
		r.cmd.Printf("skipped %s: it has comments, which fmt would remove (use --force to rewrite it anyway)\n", displayPath(r.cwd, ch.File))
	}
	switch {
	case len(blocked) > 0:
		return Exit(ExitUsage)
	case r.check && len(changes) > 0:
		return Exit(ExitFailed)
	}
	return nil
}

// split separates changes to write from files kept for their comments.
// In --check mode everything is listed and nothing is kept back.
func (r fmtRun) split(changes []flowi.FileChange) (write, keep []flowi.FileChange) {
	for _, ch := range changes {
		if ch.DropsComments && !r.force && !r.check {
			keep = append(keep, ch)
			continue
		}
		write = append(write, ch)
	}
	return write, keep
}

func (r fmtRun) apply(changes []flowi.FileChange) error {
	verb := "would format"
	if !r.check {
		// Write first, so "formatted" is only ever printed for files that were.
		if err := flowi.ApplyChanges(changes); err != nil {
			return err
		}
		verb = "formatted"
	}
	for _, ch := range changes {
		suffix := ""
		if ch.DropsComments {
			suffix = " (comments removed)"
			if r.check {
				suffix = " (removes comments)"
			}
		}
		r.cmd.Printf("%s %s%s\n", verb, displayPath(r.cwd, ch.File), suffix)
	}
	return nil
}

func blockedFiles(blocked diag.List) []string {
	var files []string
	for _, d := range blocked {
		if !slices.Contains(files, d.File) {
			files = append(files, d.File)
		}
	}
	slices.Sort(files)
	return files
}
