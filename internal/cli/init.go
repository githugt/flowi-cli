// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"path/filepath"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
)

func newInitCommand(i do.Injector) *cobra.Command {
	var name string
	var noMount bool
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Create a new collection",
		Long:  "Create a new collection in dir (default: the current directory) and mount it in the workspace.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			if name == "" {
				name = collectionNameFor(dir)
			}
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			m, err := s.Workspace.InitCollection(dir, name, !noMount)
			if err != nil {
				return err
			}
			cmd.Printf("Created collection %s (%s) at %s\n", m.Name, m.ID, dir)
			if !noMount {
				cmd.Printf("Mounted in workspace %q\n", s.Workspace.Model().Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "collection name (default: the directory name)")
	cmd.Flags().BoolVar(&noMount, "no-mount", false, "create the collection without mounting it")
	return cmd
}

func collectionNameFor(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Base(dir)
	}
	return filepath.Base(abs)
}
