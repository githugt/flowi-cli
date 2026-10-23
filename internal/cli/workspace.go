// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"slices"
	"text/tabwriter"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	"github.com/githugt/flowi-lib/model"
)

func newWorkspaceCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage the collections mounted in your workspace",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		newWorkspaceLsCommand(i),
		newWorkspaceMountCommand(i),
		newWorkspaceUnmountCommand(i),
		newWorkspaceLocateCommand(i),
	)
	return cmd
}

func newWorkspaceLsCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List mounted collections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			return printMounts(cmd, s)
		},
	}
}

func printMounts(cmd *cobra.Command, s *Session) error {
	names := map[model.ID]string{}
	for _, c := range s.Workspace.Collections() {
		names[c.Manifest.ID] = c.Manifest.Name
	}
	missing := s.Workspace.Missing()
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPATH\tSTATUS")
	for _, m := range s.Workspace.Model().Collections {
		name, status := names[m.ID], "ok"
		if slices.Contains(missing, m.ID) {
			name, status = "-", "missing"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", m.ID, name, m.Path, status)
	}
	return tw.Flush()
}

func newWorkspaceMountCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "mount <dir>",
		Short: "Mount the collection in dir",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			m, err := s.Workspace.Mount(args[0])
			if err != nil {
				return err
			}
			cmd.Printf("Mounted %s (%s) from %s\n", m.Name, m.ID, args[0])
			return nil
		},
	}
}

func newWorkspaceUnmountCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "unmount <id>",
		Short: "Unmount a collection (its files are not touched)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			if err := s.Workspace.Unmount(model.ID(args[0])); err != nil {
				return err
			}
			cmd.Printf("Unmounted %s\n", args[0])
			return nil
		},
	}
}

func newWorkspaceLocateCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "locate <id> <dir>",
		Short: "Point a moved collection at its new directory",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			if err := s.Workspace.Relocate(model.ID(args[0]), args[1]); err != nil {
				return err
			}
			cmd.Printf("Located %s at %s\n", args[0], args[1])
			return nil
		},
	}
}
