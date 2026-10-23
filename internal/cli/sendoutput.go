// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/engine"
)

// textReport prints a send for people: progress and summary on stderr,
// the response body on stdout.
type textReport struct {
	out, err io.Writer
	cwd      string
	headers  bool
	quiet    bool
}

func (r textReport) render(send *engine.Send) (engine.SendResult, error) {
	for ev := range send.Events() {
		if !r.quiet {
			r.event(ev)
		}
	}
	res := send.Wait()
	return res, r.finish(res)
}

// finish prints the summary and the body, after progress was shown.
func (r textReport) finish(res engine.SendResult) error {
	if err := r.summary(res); err != nil {
		return err
	}
	if !r.quiet {
		r.body(res)
	}
	return nil
}

// event prints progress lines; diagnostics are printed once, in the summary.
func (r textReport) event(ev engine.Event) {
	switch e := ev.(type) {
	case engine.HookFinished:
		r.line("%s %s  %s  %s  %s", statusMark(e.Status), e.Phase, e.Level, e.Step, roundDuration(e.Duration))
	case engine.Log:
		r.line("  log %s: %s", e.Node, e.Message)
	case engine.MessageReceived:
		r.line("  message %d received", e.Seq)
	case engine.ResponseReceived:
		if e.Wait > 0 {
			r.line("  attempt %d: %s; retrying in %s", e.Attempt, attemptOutcome(e), roundDuration(e.Wait))
		}
	case engine.RunCancelled:
		r.line("cancelled")
	}
}

func statusMark(s engine.Status) string {
	if s == engine.StatusPassed {
		return "✓"
	}
	return "✗"
}

func attemptOutcome(e engine.ResponseReceived) string {
	if e.Error != "" {
		return e.Error
	}
	return fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status))
}

func (r textReport) summary(res engine.SendResult) error {
	if rec := res.Record; rec != nil {
		r.line("%s", statusLine(rec))
		if r.headers {
			for _, h := range rec.Header {
				r.line("  %s: %s", h.Name, h.Value)
			}
		}
	}
	if r.quiet {
		return nil
	}
	if err := PrintDiagnostics(r.err, OutputText, r.cwd, res.Diags); err != nil {
		return err
	}
	r.line("seed %d", res.Seed)
	for _, f := range res.Saved {
		r.line("saved %s", displayPath(r.cwd, f))
	}
	return nil
}

// statusLine is "POST <url> → 200 OK  HTTP/2.0  123ms  1.2 kB  (version 2, preferred)".
func statusLine(rec *engine.Record) string {
	if rec.Kind == engine.KindGRPC {
		return grpcStatusLine(rec)
	}
	if rec.Kind == engine.KindAsync {
		return asyncStatusLine(rec)
	}
	if rec.Status == 0 {
		return fmt.Sprintf("%s %s → no response", rec.Request.Method, rec.Request.URL)
	}
	return fmt.Sprintf("%s %s → %d %s  %s  %s  %s  (version %s, %s)", rec.Request.Method, rec.Request.URL,
		rec.Status, http.StatusText(rec.Status), rec.Proto, roundDuration(rec.Timings.Total), humanSize(rec.Body.Size),
		rec.Version, rec.VersionSource)
}

// asyncStatusLine is "MQTT mqtt://h/topic → ok  2 messages  123ms  (version 1, default)".
func asyncStatusLine(rec *engine.Record) string {
	received := 0
	for _, m := range rec.Messages {
		if m.Direction == "received" {
			received++
		}
	}
	noun := "messages"
	if received == 1 {
		noun = "message"
	}
	return fmt.Sprintf("%s %s → %s  %d %s  %s  (version %s, %s)", rec.Request.Method, rec.Request.URL,
		rec.Outcome, received, noun, roundDuration(rec.Timings.Total), rec.Version, rec.VersionSource)
}

// grpcStatusLine is "GRPC grpc://h/pkg.Svc/Get → OK  1 message  123ms  (version 1, default)",
// with the status message after a code that is not OK.
func grpcStatusLine(rec *engine.Record) string {
	received := 0
	for _, m := range rec.Messages {
		if m.Direction == "received" {
			received++
		}
	}
	noun := "messages"
	if received == 1 {
		noun = "message"
	}
	code := rec.GRPCCode
	if rec.GRPCMessage != "" && rec.GRPCCode != "OK" {
		code += " (" + rec.GRPCMessage + ")"
	}
	return fmt.Sprintf("%s %s → %s  %d %s  %s  (version %s, %s)", rec.Request.Method, rec.Request.URL,
		code, received, noun, roundDuration(rec.Timings.Total), rec.Version, rec.VersionSource)
}

func roundDuration(d time.Duration) time.Duration {
	if d >= time.Millisecond {
		return d.Round(time.Millisecond)
	}
	return d.Round(time.Microsecond)
}

func humanSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n), []string{"kB", "MB", "GB", "TB"}
	i := -1
	for value >= unit && i < len(suffix)-1 {
		value /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", value, suffix[i])
}

// body writes the response body to stdout: JSON indented, text as is.
// Streamed and binary bodies are described on stderr instead.
func (r textReport) body(res engine.SendResult) {
	rec := res.Record
	if rec == nil {
		return
	}
	if rec.HasMessages() && (rec.Kind != engine.KindGRPC || receivedMessages(rec) > 1) {
		r.messages(rec) // a gRPC call with one response prints it as a body, indented
		return
	}
	b := rec.Body
	switch {
	case b.File != "":
		r.line("body streamed to %s (%s, sha256 %s)", displayPath(r.cwd, b.File), humanSize(b.Size), b.SHA256)
	case b.Base64 != "":
		r.line("binary body (%s, %s) not printed", humanSize(b.Size), b.ContentType)
	case b.JSON != nil:
		// Indent the received text rather than re-encoding the parsed value,
		// which would reorder keys and could round large numbers.
		var buf bytes.Buffer
		if err := json.Indent(&buf, bytes.TrimSpace([]byte(b.Text)), "", "  "); err == nil {
			buf.WriteByte('\n')
			_, _ = r.out.Write(buf.Bytes()) // stdout closed: nothing more to do
			return
		}
		_, _ = io.WriteString(r.out, b.Text+"\n")
	case b.Text != "":
		_, _ = io.WriteString(r.out, b.Text)
		if !strings.HasSuffix(b.Text, "\n") {
			_, _ = io.WriteString(r.out, "\n")
		}
	}
}

// messages writes each received message to stdout, one per line (a message
// that has line breaks is written as is); binary ones are described on stderr.
func (r textReport) messages(rec *engine.Record) {
	for _, m := range rec.Messages {
		if m.Direction != "received" {
			continue
		}
		if m.Body.Base64 != "" {
			r.line("binary message (%s) not printed", humanSize(m.Body.Size))
			continue
		}
		_, _ = io.WriteString(r.out, strings.TrimSuffix(m.Body.Text, "\n")+"\n")
	}
}

func (r textReport) line(format string, args ...any) {
	_, _ = fmt.Fprintf(r.err, format+"\n", args...) // stderr is best effort
}

// writeNDJSON writes each event as one JSON object with an "event" key,
// then a final {"event":"Result", …}. Keys are snake_case throughout, as in
// saved records, and diagnostics have the shape `validate --output json`
// prints. This is a scripting interface: keep it stable.
func writeNDJSON(w io.Writer, send *engine.Send) (engine.SendResult, error) {
	for ev := range send.Events() {
		if err := writeEvent(w, ev); err != nil {
			drainEvents(send)
			return send.Wait(), err
		}
	}
	res := send.Wait()
	return res, writeNamed(w, "Result", resultJSON(res))
}

func writeEvent(w io.Writer, ev engine.Event) error {
	fields, err := eventFields(ev)
	if err != nil {
		return err
	}
	return writeNamed(w, ev.EventName(), fields)
}

// eventFields is an event as a snake_case map, with diagnostics rendered.
func eventFields(ev engine.Event) (map[string]any, error) {
	data, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("encoding %s: %w", ev.EventName(), err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // keep seeds and durations exact
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("encoding %s: %w", ev.EventName(), err)
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		out[snakeCase(k)] = v
	}
	switch e := ev.(type) {
	case engine.HookFinished:
		delete(out, "diags")
		out["diagnostics"] = jsonDiagnostics(e.Diags)
	case engine.NodeFinished:
		delete(out, "diags")
		out["diagnostics"] = jsonDiagnostics(e.Diags)
	case engine.Warning:
		delete(out, "diag")
		out["diagnostic"] = newJSONDiagnostic("", e.Diag)
	}
	return out, nil
}

func jsonDiagnostics(l diag.List) []jsonDiagnostic {
	out := make([]jsonDiagnostic, 0, len(l))
	for _, d := range l {
		out = append(out, newJSONDiagnostic("", d))
	}
	return out
}

// snakeCase turns Go field names into JSON keys: RunID → run_id.
func snakeCase(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		upper := unicode.IsUpper(r)
		if upper && i > 0 && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// writeNamed writes {"event":name, …fields} on one line.
func writeNamed(w io.Writer, name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", name, err)
	}
	head := `{"event":` + string(mustQuote(name))
	line := head + "}"
	if len(data) > 2 {
		line = head + "," + string(data[1:])
	}
	_, err = io.WriteString(w, line+"\n")
	return err
}

func mustQuote(s string) []byte {
	b, _ := json.Marshal(s) // marshalling a string cannot fail
	return b
}

func drainEvents(send *engine.Send) {
	for range send.Events() {
	}
}

type resultOut struct {
	RunID  string           `json:"run_id"`
	Seed   uint64           `json:"seed"`
	Status engine.Status    `json:"status"`
	Record *engine.Record   `json:"record,omitempty"`
	Vars   map[string]any   `json:"vars,omitempty"`
	Diags  []jsonDiagnostic `json:"diagnostics,omitempty"`
	Saved  []string         `json:"saved,omitempty"`
	Error  string           `json:"error,omitempty"`
}

func resultJSON(res engine.SendResult) resultOut {
	out := resultOut{RunID: res.RunID, Seed: res.Seed, Status: res.Status, Record: res.Record, Vars: res.Vars, Saved: res.Saved}
	if len(res.Diags) > 0 {
		out.Diags = jsonDiagnostics(res.Diags)
	}
	if res.Err != nil {
		out.Error = res.Err.Error()
	}
	return out
}

func receivedMessages(rec *engine.Record) int {
	n := 0
	for _, m := range rec.Messages {
		if m.Direction == "received" {
			n++
		}
	}
	return n
}
