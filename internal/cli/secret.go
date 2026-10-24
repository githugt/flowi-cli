// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"text/tabwriter"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
)

func newSecretCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage secret values in the workspace vault",
		Long: "Secret variables are declared in environment files as { secret: true }; their values live only in the\n" +
			"encrypted workspace vault. FLOWI_SECRET_<ENV>_<NAME> environment variables override the vault (for CI).",
	}
	var env string
	set := &cobra.Command{
		Use:   "set <env> <name>",
		Short: "Store a secret (the value is read from the terminal without echo, or from standard input)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretSet(cmd, i, args[0], args[1])
		},
	}
	unset := &cobra.Command{
		Use:   "unset <env> <name>",
		Short: "Remove a secret from the vault",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretUnset(cmd, i, args[0], args[1])
		},
	}
	ls := &cobra.Command{
		Use:   "ls",
		Short: "List secret variables and whether each is set (values are never shown)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runSecretLs(cmd, i, env, "") },
	}
	ls.Flags().StringVar(&env, "env", "", "only this environment")
	var reveal bool
	get := &cobra.Command{
		Use:   "get <env> <name>",
		Short: "Say whether a secret is set; with --reveal, print its value",
		Long: "Without --reveal this prints only the secret's state. With --reveal the value is printed on its own,\n" +
			"so $(flowi secret get local password --reveal) works in a script. A secret that FLOWI_SECRET_<ENV>_<NAME>\n" +
			"provides is not revealed: the variable is already in your environment.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretGet(cmd, i, args[0], args[1], reveal)
		},
	}
	get.Flags().BoolVar(&reveal, "reveal", false, "print the secret's value")
	cmd.AddCommand(set, unset, ls, get)
	return cmd
}

func runSecretSet(cmd *cobra.Command, i do.Injector, env, name string) error {
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	if err := s.Workspace.CheckSecret(env, name); err != nil {
		return err // before prompting: do not ask for a value that cannot be stored
	}
	value, err := do.MustInvoke[Console](i).ReadSecret(fmt.Sprintf("Value for %s/%s: ", env, name))
	if errors.Is(err, errNoInput) || (err == nil && value == "") {
		return &UsageError{Err: errors.New("no value given: type it at the prompt, or pipe it on standard input")}
	}
	if err != nil {
		return err
	}
	if err := s.Workspace.SetSecret(env, name, value); err != nil {
		return err
	}
	cmd.Printf("Set secret %s for %s\n", name, env)
	return nil
}

func runSecretUnset(cmd *cobra.Command, i do.Injector, env, name string) error {
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	if err := s.Workspace.UnsetSecret(env, name); err != nil {
		return err
	}
	cmd.Printf("Removed secret %s for %s\n", name, env)
	return nil
}

func runSecretGet(cmd *cobra.Command, i do.Injector, env, name string, reveal bool) error {
	if !reveal {
		return runSecretLs(cmd, i, env, name)
	}
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	value, err := s.Workspace.RevealSecret(env, name)
	if err != nil {
		return err
	}
	slog.Info("a secret was revealed", "Event", "SecretRevealed", "Environment", env, "Name", name)
	if do.MustInvoke[*GlobalOptions](i).Output == OutputJSON {
		return writeJSONLines(cmd.OutOrStdout(), []secretValue{{Environment: env, Name: name, Value: value}})
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), value)
	return err
}

type secretValue struct {
	Environment string `json:"environment"`
	Name        string `json:"name"`
	Value       string `json:"value"`
}

type secretRow struct {
	Environment string `json:"environment"`
	Name        string `json:"name"`
	State       string `json:"state"` // set | missing | env override
}

func runSecretLs(cmd *cobra.Command, i do.Injector, env, name string) error {
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	infos, err := s.Workspace.Secrets()
	if err != nil {
		return err
	}
	rows := secretRows(infos, env, name)
	if do.MustInvoke[*GlobalOptions](i).Output == OutputJSON {
		return writeJSONLines(cmd.OutOrStdout(), rows)
	}
	return writeSecretTable(cmd.OutOrStdout(), rows)
}

func secretRows(infos []flowi.SecretInfo, env, name string) []secretRow {
	var rows []secretRow
	for _, s := range infos {
		if (env != "" && s.Environment != env) || (name != "" && s.Name != name) {
			continue
		}
		state := "missing"
		switch {
		case s.Override:
			state = "env override"
		case s.Set:
			state = "set"
		}
		rows = append(rows, secretRow{Environment: s.Environment, Name: s.Name, State: state})
	}
	return rows
}

func writeSecretTable(w io.Writer, rows []secretRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ENV\tNAME\tSTATE")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Environment, r.Name, r.State)
	}
	return tw.Flush()
}

// writeJSONLines writes one JSON object per row.
func writeJSONLines[T any](w io.Writer, rows []T) error {
	enc := json.NewEncoder(w)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("writing JSON: %w", err)
		}
	}
	return nil
}
