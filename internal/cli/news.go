// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	"github.com/githugt/flowi-lib/announce"
	"github.com/githugt/flowi-lib/settings"
)

// announcementsCacheName is the cache file inside the config directory.
const announcementsCacheName = "announcements.cache.json"

func newNewsCommand(i do.Injector) *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:   "news",
		Short: "Show Flow-I announcements (only when a signed feed is configured)",
		Long: "Fetches the announcements feed named by announcements_url, checks that it is signed by one of announcements_keys, " +
			"and prints it. Nothing is fetched unless both are set, and flowi never fetches announcements by itself.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := do.MustInvoke[*GlobalOptions](i)
			env := do.MustInvoke[Env](i)
			paths, err := g.Paths(env.Environ)
			if err != nil {
				return err
			}
			resolved, err := userSettings(g, env)
			if err != nil {
				return err
			}
			return showNews(cmd, g, resolved, filepath.Join(paths.ConfigDir, announcementsCacheName), refresh)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "fetch again even if the cached feed is recent")
	return cmd
}

func showNews(cmd *cobra.Command, g *GlobalOptions, resolved settings.Resolved, cacheFile string, refresh bool) error {
	out := cmd.OutOrStdout()
	res, err := announce.Load(cmd.Context(), resolved, cacheFile, refresh)
	switch {
	case errors.Is(err, announce.ErrTurnedOff):
		_, werr := fmt.Fprintln(out, "Announcements are turned off (flowi config set announcements true).")
		return werr
	case errors.Is(err, announce.ErrNotConfigured):
		_, werr := fmt.Fprintln(out, "No announcements feed is configured (set announcements_url and announcements_keys).")
		return werr
	case errors.Is(err, announce.ErrNoKeys), errors.Is(err, announce.ErrBadKey):
		return &UsageError{Err: err}
	case err != nil && len(res.Items) == 0:
		return fmt.Errorf("fetching announcements: %w", err)
	case err != nil:
		fmt.Fprintf(cmd.ErrOrStderr(), "Showing the last announcements received; a fresh feed could not be had: %v\n", err)
	}
	if g.Output == OutputJSON {
		return writeJSON(out, res.Items)
	}
	return writeNewsText(out, res)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeNewsText(w io.Writer, res announce.Result) error {
	if len(res.Items) == 0 {
		_, err := fmt.Fprintln(w, "No announcements.")
		return err
	}
	for _, it := range res.Items {
		marker := "•"
		if it.Level == "warning" {
			marker = "!"
		}
		fmt.Fprintf(w, "%s %s\n", marker, it.Title)
		if it.Body != "" {
			fmt.Fprintf(w, "  %s\n", it.Body)
		}
		if it.URL != "" {
			fmt.Fprintf(w, "  %s\n", it.URL)
		}
	}
	return nil
}
