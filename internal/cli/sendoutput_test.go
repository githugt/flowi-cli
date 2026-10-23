// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/engine"
)

func TestTextReport_body_should_print_received_json_indented_keeping_order_and_numbers(t *testing.T) {
	text := `{"z":1,"id":9007199254740993,"a":{"b":[1,2]}}`
	var parsed any
	_ = json.Unmarshal([]byte(text), &parsed)
	var out, errOut bytes.Buffer
	r := textReport{out: &out, err: &errOut}

	r.body(engine.SendResult{Record: &engine.Record{Body: engine.Body{Text: text, JSON: parsed}}})

	want := "{\n  \"z\": 1,\n  \"id\": 9007199254740993,\n  \"a\": {\n    \"b\": [\n      1,\n      2\n    ]\n  }\n}\n"
	if out.String() != want {
		t.Fatalf("got:\n%s", out.String())
	}
}

func TestTextReport_body_Given_trailing_newline_Then_single_newline(t *testing.T) {
	var out bytes.Buffer
	r := textReport{out: &out, err: &bytes.Buffer{}}

	r.body(engine.SendResult{Record: &engine.Record{Body: engine.Body{Text: "{\"a\":1}\n", JSON: map[string]any{"a": 1}}}})

	if out.String() != "{\n  \"a\": 1\n}\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestWriteEvent_should_use_snake_case_and_json_diagnostics(t *testing.T) {
	var out bytes.Buffer
	d := diag.New(diag.CodeRunAssertFailed, diag.Location{}, map[string]string{"check": "status == 200", "message": "", "node": "a"})

	err := writeEvent(&out, engine.HookFinished{RunID: "r1", Level: "version", Phase: "post", Step: "inline", Status: engine.StatusFailed, Diags: diag.List{d}})

	want := `{"event":"HookFinished","depth":0,"diagnostics":[{"code":"FLW-RUN-001","severity":"error","file":"","line":0,"column":0,"message":"assertion failed: status == 200"}],"duration":0,"index":0,"level":"version","path":"","phase":"post","run_id":"r1","status":"failed","step":"inline"}` + "\n"
	if err != nil || out.String() != want {
		t.Fatalf("got %s", out.String())
	}
}

// Hook flows run on the flow scheduler since M3, so a send's stream also
// carries NodeStarted/NodeFinished for nodes inside hooks. The text
// output folds them into their hook line, exactly as before.
func TestSendText_Given_hook_flow_nodes_Then_output_unchanged_from_M2(t *testing.T) {
	var out, errOut bytes.Buffer
	r := textReport{out: &out, err: &errOut}

	for _, ev := range []engine.Event{
		engine.HookStarted{Level: "request", Phase: "pre", Step: "hooks/cfg"},
		engine.NodeStarted{Path: "pre:request:0/fetch", Node: "fetch", Type: "request", Depth: 1},
		engine.NodeFinished{Path: "pre:request:0/fetch", Node: "fetch", Type: "request", Status: engine.StatusPassed, Depth: 1},
		engine.HookFinished{Level: "request", Phase: "pre", Step: "hooks/cfg", Status: engine.StatusPassed, Duration: 3 * time.Millisecond},
	} {
		r.event(ev)
	}

	if errOut.String() != "✓ pre  request  hooks/cfg  3ms\n" || out.Len() != 0 {
		t.Fatalf("stderr %q stdout %q", errOut.String(), out.String())
	}
}
