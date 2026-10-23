// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"path/filepath"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
)

func newNewCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create things in a collection",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newNewFlowCommand(i))
	return cmd
}

type newFlowFlags struct {
	from, collection string
	set              []string
}

func newNewFlowCommand(i do.Injector) *cobra.Command {
	var f newFlowFlags
	cmd := &cobra.Command{
		Use:   "flow <path>",
		Short: "Create a flow, empty or from a blueprint",
		Long: "Creates flows/<path>.arazzo.yaml in the collection. With --from the flow is made from a blueprint; --set gives the blueprint's " +
			"parameters values (flowi blueprint show lists them). The new flow is ordinary: it keeps no link to the blueprint.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runNewFlow(cmd, i, args[0], f) },
	}
	fl := cmd.Flags()
	fl.StringVar(&f.from, "from", "", "a blueprint to make the flow from (id or path)")
	fl.StringArrayVar(&f.set, "set", nil, "value for a blueprint parameter, as name=value (repeatable; needs --from)")
	fl.StringVar(&f.collection, "collection", "", "the collection to create the flow in (name or id; needed when several are mounted)")
	return cmd
}

func runNewFlow(cmd *cobra.Command, i do.Injector, path string, f newFlowFlags) error {
	if f.from == "" && len(f.set) > 0 {
		return &UsageError{Err: fmt.Errorf("--set needs --from: only a blueprint has parameters")}
	}
	values, err := parsePairs("--set", f.set)
	if err != nil {
		return err
	}
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	target, err := s.Workspace.ResolveCollection(f.collection)
	if err != nil {
		return err
	}
	if f.from == "" {
		if _, err := s.Workspace.NewFlow(target.ID, path); err != nil {
			return err
		}
		return reportNewFlow(cmd, path)
	}
	bp, err := s.Workspace.FindBlueprint(f.from, "")
	if err != nil {
		return err
	}
	if _, err := s.Workspace.NewFlowFromBlueprint(bp, target.ID, path, values); err != nil {
		return err
	}
	return reportNewFlow(cmd, path)
}

func reportNewFlow(cmd *cobra.Command, path string) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Created flow %s\n", filepath.ToSlash(path))
	return err
}
