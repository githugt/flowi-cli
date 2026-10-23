// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	"github.com/githugt/flowi-lib/model"
	"github.com/githugt/flowi-lib/serve/scheduler"
)

func newScheduleCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage the cron schedules `flowi serve` runs",
		Long: "Schedules are saved in the workspace's workspace.yaml, not in a collection: they are personal to this\n" +
			"machine. `flowi serve` reads them when it starts; restart it after changing them.",
	}
	cmd.AddCommand(newScheduleAddCommand(i), newScheduleLsCommand(i), newScheduleRmCommand(i))
	return cmd
}

type scheduleFlags struct {
	flow, collection, cron, env, tz string
	inputs                          []string
	disabled                        bool
}

func newScheduleAddCommand(i do.Injector) *cobra.Command {
	var f scheduleFlags
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a schedule",
		Long: "Add a schedule that runs a flow. --cron takes five fields (minute hour day-of-month month day-of-week)\n" +
			"with lists, ranges, steps and month/weekday names, or @hourly, @daily, @weekly, @monthly, @yearly.\n" +
			"Missed runs (the machine was off or asleep) are not made up.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runScheduleAdd(cmd, i, args[0], f) },
	}
	fl := cmd.Flags()
	fl.StringVar(&f.flow, "flow", "", "flow to run (path or id)")
	fl.StringVar(&f.cron, "cron", "", "when to run, as a cron expression")
	fl.StringVar(&f.collection, "collection", "", "collection (name or id) when the flow path is ambiguous")
	fl.StringVar(&f.env, "env", "", "environment to run in (default: the defaults.environment setting)")
	fl.StringArrayVar(&f.inputs, "input", nil, "value for a flow input, as name=value (a template; repeatable)")
	fl.StringVar(&f.tz, "tz", "", "time zone of the cron expression, such as Europe/Berlin (default: this machine's)")
	fl.BoolVar(&f.disabled, "disabled", false, "save the schedule switched off")
	return cmd
}

func runScheduleAdd(cmd *cobra.Command, i do.Injector, name string, f scheduleFlags) error {
	inputs, err := orderedPairs("--input", f.inputs)
	if err != nil {
		return err
	}
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	err = s.Workspace.AddSchedule(model.Schedule{Name: name, Flow: f.flow, Collection: f.collection, Cron: f.cron,
		Env: f.env, Inputs: inputs, TZ: f.tz, Disabled: f.disabled})
	if err != nil {
		return err
	}
	cmd.Printf("Added schedule %s\n", name)
	return nil
}

// orderedPairs reads name=value flags in the order given.
func orderedPairs(flag string, pairs []string) ([]model.KeyValue, error) {
	if _, err := parsePairs(flag, pairs); err != nil {
		return nil, err
	}
	out := make([]model.KeyValue, 0, len(pairs))
	for _, p := range pairs {
		k, v, _ := strings.Cut(p, "=")
		out = append(out, model.KeyValue{Key: k, Value: v})
	}
	return out, nil
}

// scheduleRow is one schedule as `schedule ls` shows it.
type scheduleRow struct {
	Name       string            `json:"name"`
	Flow       string            `json:"flow"`
	Collection string            `json:"collection,omitempty"`
	Cron       string            `json:"cron"`
	Env        string            `json:"env,omitempty"`
	Inputs     map[string]string `json:"inputs,omitempty"`
	TZ         string            `json:"tz,omitempty"`
	Disabled   bool              `json:"disabled"`
	Next       string            `json:"next,omitempty"`
}

func newScheduleLsCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List schedules and when each runs next",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			rows := scheduleRows(s.Workspace.Schedules(), time.Now())
			if do.MustInvoke[*GlobalOptions](i).Output == OutputJSON {
				return writeJSONLines(cmd.OutOrStdout(), rows)
			}
			return writeScheduleTable(cmd.OutOrStdout(), rows)
		},
	}
}

func scheduleRows(list []model.Schedule, now time.Time) []scheduleRow {
	rows := make([]scheduleRow, 0, len(list))
	for _, s := range list {
		row := scheduleRow{Name: s.Name, Flow: s.Flow, Collection: s.Collection, Cron: s.Cron, Env: s.Env, TZ: s.TZ, Disabled: s.Disabled}
		if len(s.Inputs) > 0 {
			row.Inputs = map[string]string{}
			for _, kv := range s.Inputs {
				row.Inputs[kv.Key] = kv.Value
			}
		}
		if job, err := scheduler.NewJob(s); err == nil && !s.Disabled {
			row.Next = job.Next(now).Format("2006-01-02 15:04 MST")
		}
		rows = append(rows, row)
	}
	return rows
}

func writeScheduleTable(w io.Writer, rows []scheduleRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tCRON\tNEXT\tFLOW\tENV")
	for _, r := range rows {
		next := r.Next
		if r.Disabled {
			next = "(disabled)"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Name, r.Cron, next, r.Flow, r.Env)
	}
	return tw.Flush()
}

func newScheduleRmCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a schedule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			if err := s.Workspace.RemoveSchedule(args[0]); err != nil {
				return err
			}
			cmd.Printf("Removed schedule %s\n", args[0])
			return nil
		},
	}
}
