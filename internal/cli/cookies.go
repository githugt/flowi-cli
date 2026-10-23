// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
)

func newCookiesCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cookies",
		Short: "Manage the cookies standalone sends keep per collection",
	}
	var collection string
	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Forget the saved cookies of one collection, or of all",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			if err := s.Workspace.ClearCookies(collection); err != nil {
				return err
			}
			if collection == "" {
				cmd.Println("Cleared cookies for every collection")
				return nil
			}
			cmd.Printf("Cleared cookies for %s\n", collectionName(s.Workspace, collection))
			return nil
		},
	}
	clearCmd.Flags().StringVar(&collection, "collection", "", "collection (name or id); default: all")
	cmd.AddCommand(clearCmd)
	return cmd
}

// collectionName is the display name of the collection a command acted
// on (the only mounted one when none was named).
func collectionName(ws *flowi.Workspace, ref string) string {
	for _, c := range ws.Collections() {
		if ref == "" || c.Manifest.Name == ref || string(c.Manifest.ID) == ref {
			return c.Manifest.Name
		}
	}
	return ref
}
