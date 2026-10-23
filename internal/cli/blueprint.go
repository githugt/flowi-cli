// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"path/filepath"
	"text/tabwriter"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/model"
)

func newBlueprintCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blueprint",
		Short: "List, show and create blueprints (flow templates with parameters)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newBlueprintLsCommand(i), newBlueprintShowCommand(i), newBlueprintCreateCommand(i))
	return cmd
}

// jsonBlueprint is the stable JSON shape of a blueprint.
type jsonBlueprint struct {
	Collection  string      `json:"collection"`
	Path        string      `json:"path"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Params      []jsonParam `json:"params"`
}

type jsonParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
}

func jsonBlueprintOf(c model.Collection, b model.Blueprint) jsonBlueprint {
	out := jsonBlueprint{Collection: c.Manifest.Name, Path: b.Path, Name: b.Name, Description: b.Description, Params: []jsonParam{}}
	for _, p := range b.Params {
		out.Params = append(out.Params, jsonParam{Name: p.Name, Type: string(p.Type), Default: p.Default, Description: p.Description})
	}
	return out
}

func newBlueprintLsCommand(i do.Injector) *cobra.Command {
	var collection string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List the blueprints of mounted collections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			g := do.MustInvoke[*GlobalOptions](i)
			found := blueprintsOf(s.Workspace.Collections(), collection)
			if g.Output == OutputJSON {
				return writeJSON(cmd.OutOrStdout(), found)
			}
			printBlueprints(cmd, found)
			return nil
		},
	}
	cmd.Flags().StringVar(&collection, "collection", "", "only this collection (name or id)")
	return cmd
}

func blueprintsOf(cs []model.Collection, only string) []jsonBlueprint {
	out := []jsonBlueprint{}
	for _, c := range cs {
		if only != "" && c.Manifest.Name != only && string(c.Manifest.ID) != only {
			continue
		}
		for _, b := range c.Blueprints {
			out = append(out, jsonBlueprintOf(c, b))
		}
	}
	return out
}

func printBlueprints(cmd *cobra.Command, bps []jsonBlueprint) {
	out := cmd.OutOrStdout()
	if len(bps) == 0 {
		fmt.Fprintln(out, "No blueprints. Save a flow as one with: flowi blueprint create <path> --from-flow <flow>")
		return
	}
	for _, b := range bps {
		fmt.Fprintf(out, "%s: %s  (%s)\n", b.Collection, b.Name, b.Path)
		for _, p := range b.Params {
			fmt.Fprintf(out, "    %s\n", paramLine(p))
		}
	}
}

func paramLine(p jsonParam) string {
	line := p.Name + ": " + p.Type
	if p.Default != "" {
		line += " = " + p.Default
	}
	return line
}

func newBlueprintShowCommand(i do.Injector) *cobra.Command {
	var collection string
	cmd := &cobra.Command{
		Use:   "show <blueprint>",
		Short: "Describe one blueprint and its parameters",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			target, err := s.Workspace.FindBlueprint(args[0], collection)
			if err != nil {
				return err
			}
			bp, err := s.Workspace.Blueprint(target)
			if err != nil {
				return err
			}
			return printBlueprint(cmd, i, s.Workspace, target, bp)
		},
	}
	cmd.Flags().StringVar(&collection, "collection", "", "the collection to look in (name or id)")
	return cmd
}

func printBlueprint(cmd *cobra.Command, i do.Injector, ws *flowi.Workspace, target flowi.BlueprintTarget, bp model.Blueprint) error {
	out := cmd.OutOrStdout()
	var owner model.Collection
	for _, c := range ws.Collections() {
		if c.Manifest.ID == target.Collection {
			owner = c
		}
	}
	if do.MustInvoke[*GlobalOptions](i).Output == OutputJSON {
		return writeJSON(out, jsonBlueprintOf(owner, bp))
	}
	fmt.Fprintf(out, "%s  (%s, in %s)\n", bp.Name, bp.Path, owner.Manifest.Name)
	if bp.Description != "" {
		fmt.Fprintf(out, "%s\n", bp.Description)
	}
	fmt.Fprintln(out, "\nParameters (give a value with --set name=value):")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, p := range bp.Params {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", p.Name, p.Type, p.Default, p.Description)
	}
	return tw.Flush()
}

func newBlueprintCreateCommand(i do.Injector) *cobra.Command {
	var fromFlow, name, collection string
	cmd := &cobra.Command{
		Use:   "create <path>",
		Short: "Save a flow as a blueprint under blueprints/",
		Long: "Makes a blueprint from an existing flow: each request or subflow it calls becomes a parameter whose default is the original, " +
			"and each flow input becomes a string parameter. Using the blueprint with its defaults rebuilds the flow.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if fromFlow == "" {
				return &UsageError{Err: fmt.Errorf("--from-flow is required")}
			}
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			flow, err := s.Workspace.FindFlow(fromFlow, collection)
			if err != nil {
				return err
			}
			if name == "" {
				name = filepath.Base(args[0])
			}
			target, err := s.Workspace.CreateBlueprint(flow, args[0], name)
			if err != nil {
				return err
			}
			bp, err := s.Workspace.Blueprint(target)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created blueprint %s (%s)\n", bp.Path, bp.File)
			return nil
		},
	}
	cmd.Flags().StringVar(&fromFlow, "from-flow", "", "the flow to make the blueprint from (id or path)")
	cmd.Flags().StringVar(&name, "name", "", "the blueprint's name (default: its path's last part)")
	cmd.Flags().StringVar(&collection, "collection", "", "the collection of the flow (name or id)")
	return cmd
}
