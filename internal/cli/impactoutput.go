// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/githugt/flowi-lib/analysis"
)

func impactSymbol(s analysis.Status) string {
	switch s {
	case analysis.Broken:
		return "✗"
	case analysis.Unknown:
		return "⚠"
	}
	return "✓"
}

// writeImpactText prints a report as a tree: versions, their consumers,
// and every read that is not compatible.
func writeImpactText(w io.Writer, rep analysis.Report) {
	fmt.Fprintf(w, "%s · %s\n", rep.Request, rep.Collection)
	counts := map[analysis.Status]int{}
	for _, v := range rep.Versions {
		fmt.Fprintf(w, "  version %s · contract: %s\n", v.Version, contractName(v.Contract))
		if len(v.Consumers) == 0 {
			fmt.Fprintln(w, "    no consumers")
		}
		for _, c := range v.Consumers {
			counts[c.Status]++
			fmt.Fprintf(w, "    %s %s\n", impactSymbol(c.Status), consumerName(rep, c))
			for _, r := range c.Reads {
				if r.Status != analysis.Compatible {
					fmt.Fprintf(w, "        %s %s: %s\n", impactSymbol(r.Status), readName(c, r), r.Reason)
				}
			}
		}
	}
	fmt.Fprintln(w, impactSummary(counts))
}

func contractName(c analysis.ContractInfo) string {
	if c.Source == analysis.SourceResponse {
		return "last saved response"
	}
	if c.Source != analysis.SourceExample && c.Source != analysis.SourceSchema {
		return "none"
	}
	names := make([]string, len(c.Files))
	for i, f := range c.Files {
		names[i] = filepath.Base(f)
	}
	return c.Source + " " + strings.Join(names, ", ")
}

// consumerName says where a consumer is; other collections are prefixed.
func consumerName(rep analysis.Report, c analysis.Consumer) string {
	prefix := ""
	if c.Collection != rep.Collection {
		prefix = "[" + c.Collection + "] "
	}
	switch {
	case c.Kind == analysis.KindRequest:
		return prefix + "request extract + assert"
	case c.Kind == analysis.KindHook:
		return prefix + "hook " + c.Hook
	case c.Flow != "":
		return prefix + "flow " + c.Flow + " › node " + c.Node
	}
	return prefix + "hook " + c.Hook + " › node " + c.Node
}

func readName(c analysis.Consumer, r analysis.Read) string {
	if r.Kind == analysis.ReadInput {
		return "input " + r.Target + " (" + r.Expr + ")"
	}
	name := r.Expr + " → " + r.Target
	if r.Reader != "" && r.Reader != c.Node {
		name += " (read by " + r.Reader + ")"
	}
	return name
}

// impactSummary counts consumers, worst first, skipping zeros.
func impactSummary(counts map[analysis.Status]int) string {
	var parts []string
	for _, s := range []analysis.Status{analysis.Broken, analysis.Unknown, analysis.Compatible} {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
		}
	}
	if len(parts) == 0 {
		return "no consumers"
	}
	return strings.Join(parts, ", ")
}
