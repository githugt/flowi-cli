// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/githugt/flowi-lib/settings"
)

// GlobalOptions are the persistent flags shared by every command.
type GlobalOptions struct {
	ConfigDir string
	LogLevel  string
	LogFile   string
	Output    string
}

// Env is what the process knows about its surroundings.
type Env struct {
	Environ []string // from settings.ProcessEnviron
	Home    string
	Cwd     string
}

func addGlobalFlags(root *cobra.Command, g *GlobalOptions) {
	f := root.PersistentFlags()
	f.StringVar(&g.ConfigDir, "config-dir", "", "directory for Flow-I's own files (default: FLOWI_CONFIG_DIR or the OS config dir)")
	f.StringVar(&g.LogLevel, "log-level", "", "log level: debug, info, warn or error")
	f.StringVar(&g.LogFile, "log-file", "", "append logs to this file instead of standard error")
	f.StringVar(&g.Output, "output", OutputText, "output format: text or json")
}

func (g *GlobalOptions) validate() error {
	if g.Output != OutputText && g.Output != OutputJSON {
		return &UsageError{Err: fmt.Errorf("unknown output format %q (want text or json)", g.Output)}
	}
	return nil
}

// FlagLayer turns the logging flags into the highest settings layer.
func (g *GlobalOptions) FlagLayer() settings.LayerValues {
	values := map[string]any{}
	if g.LogLevel != "" {
		values["logging.level"] = g.LogLevel
	}
	if g.LogFile != "" {
		values["logging.file"] = g.LogFile
	}
	return settings.LayerValues{Layer: settings.LayerFlag, Source: "command line", Values: values}
}

// Paths returns Flow-I's directories, honouring --config-dir.
func (g *GlobalOptions) Paths(environ []string) (settings.Paths, error) {
	if g.ConfigDir != "" {
		return settings.Paths{ConfigDir: g.ConfigDir}, nil
	}
	return settings.DiscoverPaths(environ)
}
