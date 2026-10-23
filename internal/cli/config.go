// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/settings"
)

func newConfigCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and change Flow-I settings",
		Long: "Read and change settings. Values resolve from, highest first: flags, FLOWI_* environment\n" +
			"variables, node, request, folder, collection, workspace, user, built-in default.",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(
		newConfigListCommand(i), newConfigGetCommand(i), newConfigSetCommand(i),
		newConfigResetCommand(i), newConfigEditCommand(i), newConfigExplainCommand(i),
	)
	return cmd
}

// parseLayer accepts the two layers a user edits directly.
func parseLayer(name string) (settings.Layer, error) {
	switch name {
	case "user":
		return settings.LayerUser, nil
	case "workspace":
		return settings.LayerWorkspace, nil
	}
	return 0, &UsageError{Err: fmt.Errorf("unknown layer %q (want user or workspace)", name)}
}

// resolvedFor resolves settings, scoped to a mounted collection when dir is set.
func resolvedFor(s *Session, dir string) (settings.Resolved, error) {
	if dir == "" {
		return s.Workspace.Settings(flowi.Scope{}), nil
	}
	want, err := filepath.Abs(dir)
	if err != nil {
		return settings.Resolved{}, fmt.Errorf("resolving %s: %w", dir, err)
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	for _, c := range s.Workspace.Collections() {
		if c.Root == want {
			return s.Workspace.Settings(flowi.Scope{Collection: c.Manifest.ID}), nil
		}
	}
	return settings.Resolved{}, &UsageError{Err: fmt.Errorf("%s is not a mounted collection", dir)}
}

func lookupKey(key string) (settings.Key, error) {
	k, ok := settings.Lookup(settings.Schema(), key)
	if !ok {
		d := diag.New(diag.CodeSettingUnknown, diag.Location{}, map[string]string{"key": key})
		d.Severity = diag.SeverityError
		return k, diag.List{d}.Err()
	}
	return k, nil
}

func newConfigListCommand(i do.Injector) *cobra.Command {
	var collection string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show every setting, its value and where it comes from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			r, err := resolvedFor(s, collection)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "KEY\tVALUE\tSOURCE")
			for _, k := range settings.Schema() {
				v, _ := r.Get(k.Name)
				fmt.Fprintf(tw, "%s\t%s\t%s\n", k.Name, displayValue(v.Value), describeSource(v))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&collection, "collection", "", "include the settings of the mounted collection in this directory")
	return cmd
}

func newConfigGetCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print the value of a setting",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := lookupKey(args[0]); err != nil {
				return err
			}
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			v, _ := s.Workspace.Settings(flowi.Scope{}).Get(args[0])
			cmd.Println(plainValue(v.Value))
			return nil
		},
	}
}

func newConfigSetCommand(i do.Injector) *cobra.Command {
	var layer string
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Store a setting in your user settings (or the workspace)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := parseLayer(layer)
			if err != nil {
				return err
			}
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			return s.Workspace.SetSetting(l, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&layer, "layer", "user", "where to store it: user or workspace")
	return cmd
}

func newConfigResetCommand(i do.Injector) *cobra.Command {
	var layer string
	cmd := &cobra.Command{
		Use:   "reset <key>",
		Short: "Remove a setting from your user settings (or the workspace)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := parseLayer(layer)
			if err != nil {
				return err
			}
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			return s.Workspace.ResetSetting(l, args[0])
		},
	}
	cmd.Flags().StringVar(&layer, "layer", "user", "where to remove it from: user or workspace")
	return cmd
}

func newConfigExplainCommand(i do.Injector) *cobra.Command {
	var collection string
	cmd := &cobra.Command{
		Use:   "explain <key>",
		Short: "Show every layer that sets a key, and which one wins",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := lookupKey(args[0]); err != nil {
				return err
			}
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			r, err := resolvedFor(s, collection)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for n, v := range r.Explain(args[0]) {
				marker := "  "
				if n == 0 {
					marker = "→ "
				}
				fmt.Fprintf(tw, "%s%s\t%s\t%s\n", marker, v.Layer, displayValue(v.Value), v.Source)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&collection, "collection", "", "include the settings of the mounted collection in this directory")
	return cmd
}

func newConfigEditCommand(i do.Injector) *cobra.Command {
	var layer string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Open your settings file (or workspace.yaml) in $EDITOR, then check it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			l, err := parseLayer(layer)
			if err != nil {
				return err
			}
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			file, err := layerFile(i, s, l)
			if err != nil {
				return err
			}
			if err := runEditor(cmd, do.MustInvoke[Env](i), file); err != nil {
				return err
			}
			return recheckFile(cmd, i, file)
		},
	}
	cmd.Flags().StringVar(&layer, "layer", "user", "which file: user or workspace")
	return cmd
}

// layerFile returns the file behind a layer, creating an empty user
// settings file so the editor has something to open.
func layerFile(i do.Injector, s *Session, l settings.Layer) (string, error) {
	if l == settings.LayerWorkspace {
		return s.Workspace.Model().File, nil
	}
	env := do.MustInvoke[Env](i)
	paths, err := do.MustInvoke[*GlobalOptions](i).Paths(env.Environ)
	if err != nil {
		return "", err
	}
	file := flowi.UserSettingsFile(paths)
	if _, err := os.Stat(file); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			return "", fmt.Errorf("creating %s: %w", file, err)
		}
	}
	return file, nil
}

func runEditor(cmd *cobra.Command, env Env, file string) error {
	parts := strings.Fields(settings.Editor(env.Environ))
	if len(parts) == 0 {
		return &UsageError{Err: errors.New("no editor configured (set VISUAL or EDITOR)")}
	}
	// The editor is the user's own choice of program, run on their own file.
	editor := exec.CommandContext(cmd.Context(), parts[0], append(parts[1:], file)...) //nolint:gosec // see above
	editor.Stdin, editor.Stdout, editor.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := editor.Run(); err != nil {
		return fmt.Errorf("running editor %s: %w", parts[0], err)
	}
	return nil
}

// recheckFile reopens the workspace and reports problems in the edited file.
func recheckFile(cmd *cobra.Command, i do.Injector, file string) error {
	g := do.MustInvoke[*GlobalOptions](i)
	env := do.MustInvoke[Env](i)
	fresh, err := OpenSession(cmd.Context(), g, env, nil, nil) // only re-reads files; no vault use
	if err != nil {
		return err
	}
	var inFile diag.List
	for _, d := range fresh.OpenDiags {
		if d.File == file {
			inFile = append(inFile, d)
		}
	}
	if err := PrintDiagnostics(cmd.OutOrStdout(), g.Output, env.Cwd, inFile); err != nil {
		return err
	}
	if inFile.HasErrors() {
		return Exit(ExitUsage)
	}
	return nil
}

func describeSource(v settings.Value) string {
	switch v.Layer {
	case settings.LayerUser, settings.LayerWorkspace, settings.LayerCollection, settings.LayerFolder:
		return fmt.Sprintf("%s (%s)", v.Layer, v.Source)
	}
	return v.Layer.String()
}

// displayValue renders a value for tables: empty text shows as "".
func displayValue(v any) string {
	if s := plainValue(v); s != "" {
		return s
	}
	return `""`
}

// plainValue renders a value the way it would be written in a settings file.
func plainValue(v any) string {
	switch x := v.(type) {
	case time.Duration:
		// Written the way people write them: "10m", not Go's "10m0s".
		s := x.String()
		if strings.HasSuffix(s, "m0s") {
			s = strings.TrimSuffix(s, "0s")
		}
		if strings.HasSuffix(s, "h0m") {
			s = strings.TrimSuffix(s, "0m")
		}
		return s
	case []string:
		return strings.Join(x, ",")
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}
