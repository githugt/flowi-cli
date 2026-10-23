// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	"github.com/githugt/flowi-lib/diag"
)

func newValidateCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "validate [dir]",
		Short: "Check collections for errors",
		Long:  "Check every mounted collection, or the collection in dir (mounted or not), and report problems.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := do.Invoke[*Session](i)
			if err != nil {
				return err
			}
			diags, err := validateTarget(s, args)
			if err != nil {
				return err
			}
			g := do.MustInvoke[*GlobalOptions](i)
			if err := PrintDiagnostics(cmd.OutOrStdout(), g.Output, do.MustInvoke[Env](i).Cwd, diags); err != nil {
				return err
			}
			cmd.PrintErrln(summarise(diags))
			if diags.HasErrors() {
				return Exit(ExitUsage)
			}
			return nil
		},
	}
}

// validateTarget validates the given directory, or the whole workspace
// (whose results already include the problems found while opening it).
func validateTarget(s *Session, args []string) (diag.List, error) {
	if len(args) == 1 {
		return s.Workspace.ValidateDir(args[0])
	}
	return s.Workspace.Validate(), nil
}
