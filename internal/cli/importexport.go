// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/model"
)

type importFlags struct {
	collection, init             string
	dryRun, force, skip, partial bool
	overlays                     []string // OpenAPI Overlay documents, applied in order
}

// importKind describes one import subcommand.
type importKind struct {
	kind        flowi.ImportKind
	short, long string
	arg         string // the argument's name in usage
}

func importKinds() []importKind {
	return []importKind{
		{flowi.ImportOpenAPI, "Import an OpenAPI document", "Import an OpenAPI 3.0/3.1 document: one request per operation (path versions /v1/, /v2/\n" +
			"become request versions), bodies generated from schemas, response schemas kept as the\n" +
			"request's contract for impact analysis, and flows suggested from links.", "<file>"},
		{flowi.ImportAsyncAPI, "Import an AsyncAPI document", "Import an AsyncAPI 3.x document (2.x as far as it maps): one async request per operation, with the\n" +
			"protocol, server and channel and an example of the message it sends. A server whose protocol Flow-I has no\n" +
			"client for is reported (FLW-IMPORT-001) and its operations are left out. The document is kept in descriptions/.", "<file>"},
		{flowi.ImportProto, "Import a .proto file as gRPC requests", "Import a .proto file: one gRPC request per unary or server-streaming method of its services,\n" +
			"with an example message. The file and the local files it imports are copied to protos/ for the requests\n" +
			"to compile; set {{env.grpc_server}} (or edit the server) to call them. Client and bidirectional streaming\n" +
			"methods are reported (FLW-IMPORT-001) and left out.", "<file>"},
		{flowi.ImportArazzo, "Import an Arazzo document", "Import Arazzo 1.0/1.1 workflows as flows under flows/arazzo/. Steps call the collection's\n" +
			"requests by OpenAPI operationId, so import the OpenAPI document first. Anything without\n" +
			"a Flow-I equivalent is reported (FLW-IMPORT-001) and kept as a note node.", "<file>"},
		{flowi.ImportPostman, "Import a Postman collection or environment", "Import a Postman v2.0/v2.1 collection (folders, requests, auth, bodies, saved responses as\n" +
			"examples, collection variables as an environment) or a Postman environment file.\n" +
			"Variables become {{env.x}} (or {{secret.x}} for credentials), dynamic variables become\n" +
			"generators; scripts are reported.", "<file>"},
		{flowi.ImportBruno, "Import a Bruno collection folder", "Import a Bruno collection folder: .bru requests with their folders, environments,\n" +
			"inherited headers and auth, asserts and post-response variables (as extracts).", "<folder>"},
		{flowi.ImportHAR, "Import a HAR recording", "Import an HTTP Archive saved from a browser: one request per endpoint (ids in paths become\n" +
			"{{id}}), a 2xx JSON response as each request's example, and a flow replaying the\n" +
			"recording in order. Static resources are left out.", "<file>"},
		{flowi.ImportHTTP, "Import a .http / .rest file", "Import a .http or .rest file (VS Code REST Client, JetBrains HTTP Client): one request per ### block,\n" +
			"file variables (@name = value) as an environment, credentials as secret placeholders. Response\n" +
			"handlers and scripts are reported (FLW-IMPORT-001) and left out.", "<file|->"},
		{flowi.ImportCurl, "Import curl commands", "Import curl commands (bash or Windows cmd quoting, as \"Copy as cURL\" writes them), one\n" +
			"request per command. Use - to read them from standard input.", "<file|->"},
		{flowi.ImportMermaid, "Import a Mermaid flowchart as a flow", "Import a Mermaid flowchart as a flow under flows/mermaid/. Labels follow Flow-I's\n" +
			"conventions (req: auth/login@2, delay: 2s, extract: token=$.data.token, set: x=1,\n" +
			"assert: …, log: …, note: …); what does not resolve becomes a placeholder note.", "<file|->"},
	}
}

func newImportCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import requests and flows from OpenAPI, AsyncAPI, .proto, Arazzo, Postman, Bruno, HAR, .http, curl or Mermaid",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	for _, k := range importKinds() {
		cmd.AddCommand(newImportKindCommand(i, k))
	}
	return cmd
}

func newImportKindCommand(i do.Injector, k importKind) *cobra.Command {
	var f importFlags
	cmd := &cobra.Command{
		Use:   string(k.kind) + " " + k.arg,
		Short: k.short,
		Long: k.long + "\n\nExisting files are never overwritten unless --force; --skip-existing adds only new\n" +
			"files. --dry-run lists the files without writing them. When items cannot be imported,\n" +
			"nothing is written unless --partial; the command then exits 1.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runImport(cmd, i, k.kind, args[0], f) },
	}
	fl := cmd.Flags()
	fl.StringVar(&f.collection, "collection", "", "collection (name or id) to import into (default: the only one mounted)")
	fl.StringVar(&f.init, "init", "", "create a new collection in this directory and import into it")
	fl.BoolVar(&f.dryRun, "dry-run", false, "list the files without writing them")
	fl.BoolVar(&f.force, "force", false, "overwrite files that already exist")
	fl.BoolVar(&f.skip, "skip-existing", false, "write only files that do not exist yet")
	fl.BoolVar(&f.partial, "partial", false, "import what can be imported when some items cannot")
	if k.kind == flowi.ImportOpenAPI || k.kind == flowi.ImportAsyncAPI {
		fl.StringArrayVar(&f.overlays, "overlay", nil, "an OpenAPI Overlay file (or URL) applied to the document before import; repeat for several, applied in order")
	}
	return cmd
}

func (f importFlags) check() error {
	switch {
	case f.init != "" && f.collection != "":
		return &UsageError{Err: fmt.Errorf("--init and --collection cannot be used together")}
	case f.force && f.skip:
		return &UsageError{Err: fmt.Errorf("--force and --skip-existing cannot be used together")}
	}
	return nil
}

func runImport(cmd *cobra.Command, i do.Injector, kind flowi.ImportKind, arg string, f importFlags) error {
	if err := f.check(); err != nil {
		return err
	}
	env := do.MustInvoke[Env](i)
	src, err := importSource(cmd, kind, arg, env.Cwd)
	if err != nil {
		return err
	}
	if src.Overlays, err = readOverlays(cmd, f.overlays, env.Cwd); err != nil {
		return err
	}
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	f.init = initDir(f.init, env.Cwd)
	plan, err := previewImport(cmd, s.Workspace, src, f, env.Cwd)
	if err != nil {
		return err
	}
	skipped, err := reportImport(cmd, i, plan)
	if err != nil {
		return err
	}
	return finishImport(cmd, s.Workspace, plan, f, skipped, do.MustInvoke[*GlobalOptions](i).Output)
}

// importSource reads the argument: a file, an http(s) URL, standard input (-),
// or for Bruno a folder.
func importSource(cmd *cobra.Command, kind flowi.ImportKind, arg, cwd string) (flowi.ImportSource, error) {
	src := flowi.ImportSource{Kind: kind, Name: arg}
	var err error
	switch {
	case kind == flowi.ImportBruno:
		src.Dir = absPath(cwd, arg)
	case arg == "-":
		src.Name = "stdin"
		src.Data, err = io.ReadAll(cmd.InOrStdin())
	case flowi.IsImportURL(arg):
		var name string
		src.Data, name, err = flowi.FetchImportSource(cmd.Context(), arg)
		src.Name = name
	default:
		src.Data, err = os.ReadFile(absPath(cwd, arg))
	}
	if err != nil {
		return src, fmt.Errorf("reading %s: %w", arg, err)
	}
	return src, nil
}

// readOverlays reads the --overlay files (or URLs).
func readOverlays(cmd *cobra.Command, refs []string, cwd string) ([][]byte, error) {
	var out [][]byte
	for _, ref := range refs {
		var data []byte
		var err error
		if flowi.IsImportURL(ref) {
			data, _, err = flowi.FetchImportSource(cmd.Context(), ref)
		} else {
			data, err = os.ReadFile(absPath(cwd, ref))
		}
		if err != nil {
			return nil, fmt.Errorf("reading the overlay %s: %w", ref, err)
		}
		out = append(out, data)
	}
	return out, nil
}

func initDir(dir, cwd string) string {
	if dir == "" {
		return ""
	}
	return absPath(cwd, dir)
}

// reportImport prints the per-item report (or, for OpenAPI and Arazzo,
// the diagnostics) and returns how many items were skipped.
func reportImport(cmd *cobra.Command, i do.Injector, plan flowi.ImportPlan) (int, error) {
	g, env := do.MustInvoke[*GlobalOptions](i), do.MustInvoke[Env](i)
	if len(plan.Items) == 0 {
		return 0, PrintDiagnostics(cmd.ErrOrStderr(), g.Output, env.Cwd, plan.Diags)
	}
	out := cmd.OutOrStdout()
	if g.Output == OutputJSON {
		out = cmd.ErrOrStderr() // stdout carries only the JSON plan
	}
	return skippedItems(plan), writeImportReport(out, env.Cwd, plan.Items)
}

func skippedItems(plan flowi.ImportPlan) int {
	n := 0
	for _, it := range plan.Items {
		if it.Skipped {
			n++
		}
	}
	return n
}

// finishImport writes the plan (unless a dry run, or items were skipped
// without --partial) and decides the exit status.
func finishImport(cmd *cobra.Command, w *flowi.Workspace, plan flowi.ImportPlan, f importFlags, skipped int, output string) error {
	if f.dryRun {
		if err := writePlan(cmd.OutOrStdout(), output, plan); err != nil {
			return err
		}
		return exitIfSkipped(skipped)
	}
	if skipped > 0 && !f.partial {
		cmd.PrintErrf("%s skipped; nothing was written (use --partial to import the rest)\n", plural(skipped, "item"))
		return Exit(ExitFailed)
	}
	if err := w.ApplyImport(plan, flowi.ApplyOptions{Overwrite: f.force, SkipExisting: f.skip}); err != nil {
		return err
	}
	cmd.Printf("Imported %s into %s\n", plural(written(plan, f), "file"), importedInto(w, plan.Collection))
	return exitIfSkipped(skipped)
}

func exitIfSkipped(skipped int) error {
	if skipped > 0 {
		return Exit(ExitFailed)
	}
	return nil
}

// previewImport plans the import into the collection named, or for
// --init into a new one. --init plans first, so a document that cannot
// be imported leaves no empty collection behind.
func previewImport(cmd *cobra.Command, w *flowi.Workspace, src flowi.ImportSource, f importFlags, cwd string) (flowi.ImportPlan, error) {
	if f.init == "" {
		collection, err := w.FindCollection(f.collection)
		if err != nil {
			return flowi.ImportPlan{}, err
		}
		return w.PreviewImportSource(src, collection)
	}
	plan, err := w.PreviewImportSourceNew(src, f.init)
	if err != nil || f.dryRun || skippedItems(plan) > 0 && !f.partial {
		return plan, err // nothing will be written: create nothing either
	}
	m, err := w.InitCollection(f.init, collectionNameFor(f.init), true)
	if err != nil {
		return flowi.ImportPlan{}, err
	}
	cmd.Printf("Created collection %s (%s) at %s\n", m.Name, m.ID, displayPath(cwd, f.init))
	return w.PreviewImportSource(src, m.ID)
}

// written is how many files ApplyImport wrote.
func written(p flowi.ImportPlan, f importFlags) int {
	if f.force || !f.skip {
		return len(p.Files)
	}
	n := 0
	for _, file := range p.Files {
		if !file.Exists {
			n++
		}
	}
	return n
}

func writePlan(out io.Writer, output string, p flowi.ImportPlan) error {
	if output == OutputJSON {
		data, err := json.MarshalIndent(p.Files, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding import plan: %w", err)
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	}
	requests, flows := 0, 0
	for _, f := range p.Files {
		mark, note := "+", ""
		switch {
		case f.Remove:
			mark, note = "-", " (removed)"
		case f.Exists:
			mark, note = "~", " (exists)"
		}
		if _, err := fmt.Fprintf(out, "  %s %s%s\n", mark, f.Rel, note); err != nil {
			return err
		}
		requests += boolInt(strings.HasSuffix(f.Rel, ".req.yaml"))
		flows += boolInt(strings.HasSuffix(f.Rel, ".arazzo.yaml"))
	}
	_, err := fmt.Fprintf(out, "%s, %s, %s (dry run: nothing written)\n",
		plural(requests, "request"), plural(flows, "flow"), plural(len(p.Files), "file"))
	return err
}

func newExportCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export flows to other formats",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	var collection, outFile string
	arazzoCmd := &cobra.Command{
		Use:   "arazzo <flow>",
		Short: "Export a flow, and the subflows it calls, as an Arazzo 1.0.1 document",
		Long: "Write a flow as an Arazzo workflow. Request nodes become steps calling their OpenAPI\n" +
			"operation; asserts and extracts right after a request become its success criteria and\n" +
			"outputs. Nodes and values without an Arazzo equivalent are reported (FLW-EXPORT-001/002)\n" +
			"and listed in x-flowi-skipped.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExportArazzo(cmd, i, args[0], collection, outFile)
		},
	}
	arazzoCmd.Flags().StringVar(&collection, "collection", "", "collection (name or id) to look in")
	arazzoCmd.Flags().StringVarP(&outFile, "out", "o", "", "write to this file instead of standard output")
	cmd.AddCommand(arazzoCmd, newExportMermaidCommand(i))
	return cmd
}

func newExportMermaidCommand(i do.Injector) *cobra.Command {
	var collection, outFile string
	cmd := &cobra.Command{
		Use:   "mermaid <flow>",
		Short: "Export a flow as a Mermaid flowchart",
		Long: "Write a flow as a Mermaid flowchart. Nodes are labelled with Flow-I's conventions\n" +
			"(req: auth/login@2, delay: 2s, …), so `flowi import mermaid` reads them back;\n" +
			"containers are drawn as subgraphs.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			target, err := s.Workspace.FindFlow(args[0], collection)
			if err != nil {
				return err
			}
			out, err := s.Workspace.ExportMermaid(target)
			if err != nil {
				return err
			}
			return writeExport(cmd, do.MustInvoke[Env](i).Cwd, outFile, []byte(out))
		},
	}
	cmd.Flags().StringVar(&collection, "collection", "", "collection (name or id) to look in")
	cmd.Flags().StringVarP(&outFile, "out", "o", "", "write to this file instead of standard output")
	return cmd
}

// writeExport writes data to standard output, or to outFile.
func writeExport(cmd *cobra.Command, cwd, outFile string, data []byte) error {
	if outFile == "" {
		_, err := cmd.OutOrStdout().Write(data)
		return err
	}
	if err := os.WriteFile(absPath(cwd, outFile), data, 0o644); err != nil { //nolint:gosec // an export the user asked for, readable like their other files
		return fmt.Errorf("writing %s: %w", outFile, err)
	}
	return nil
}

func runExportArazzo(cmd *cobra.Command, i do.Injector, ref, collection, outFile string) error {
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	target, err := s.Workspace.FindFlow(ref, collection)
	if err != nil {
		return err
	}
	data, diags, err := s.Workspace.ExportArazzo(target)
	if err != nil {
		return err
	}
	env := do.MustInvoke[Env](i)
	if err := PrintDiagnostics(cmd.ErrOrStderr(), do.MustInvoke[*GlobalOptions](i).Output, env.Cwd, diags); err != nil {
		return err
	}
	return writeExport(cmd, env.Cwd, outFile, data)
}

func importedInto(w *flowi.Workspace, id model.ID) string {
	for _, c := range w.Collections() {
		if c.Manifest.ID == id {
			return c.Manifest.Name
		}
	}
	return string(id)
}

func absPath(cwd, p string) string {
	if filepath.IsAbs(p) || cwd == "" {
		return p
	}
	return filepath.Join(cwd, p)
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
