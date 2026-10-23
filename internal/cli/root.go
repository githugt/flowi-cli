// SPDX-License-Identifier: AGPL-3.0-or-later

// Package cli defines the flowi commands. Commands get their dependencies
// from the injector built by the app package; they hold no global state.
package cli

import (
	"errors"
	"fmt"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
)

// NewRootCommand builds the flowi command tree.
func NewRootCommand(i do.Injector) *cobra.Command {
	root := &cobra.Command{
		Use:           "flowi",
		Short:         "Flow-I: compose API requests into flows",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Cobra reports unknown subcommands as untyped errors; validating
		// arguments here lets them map to ExitUsage.
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &UsageError{Err: fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	g := do.MustInvoke[*GlobalOptions](i)
	addGlobalFlags(root, g)
	root.PersistentPreRunE = func(*cobra.Command, []string) error { return g.validate() }
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return &UsageError{Err: err} })
	root.AddCommand(
		newVersionCommand(),
		newInitCommand(i),
		newWorkspaceCommand(i),
		newLsCommand(i),
		newValidateCommand(i),
		newFmtCommand(i),
		newConfigCommand(i),
		newSendCommand(i),
		newRunCommand(i),
		newImpactCommand(i),
		newImportCommand(i),
		newExportCommand(i),
		newSecretCommand(i),
		newEnvCommand(i),
		newCookiesCommand(i),
		newServeCommand(i),
		newScheduleCommand(i),
		newTelemetryCommand(i),
		newNewCommand(i),
		newBlueprintCommand(i),
		newNewsCommand(i),
		newChangelogCommand(g),
	)
	markArgErrorsAsUsage(root)
	return root
}

// markArgErrorsAsUsage makes wrong argument counts usage errors (exit 2)
// in every command: cobra's own validators return untyped errors, which
// would otherwise be reported as system failures (exit 4).
func markArgErrorsAsUsage(cmd *cobra.Command) {
	if check := cmd.Args; check != nil {
		cmd.Args = func(c *cobra.Command, args []string) error {
			if err := check(c, args); err != nil {
				var usage *UsageError
				if errors.As(err, &usage) {
					return err
				}
				return &UsageError{Err: err}
			}
			return nil
		}
	}
	for _, sub := range cmd.Commands() {
		markArgErrorsAsUsage(sub)
	}
}
