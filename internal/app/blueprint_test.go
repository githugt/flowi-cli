// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// collectionArg is the example collection's name, which the tests pass as
// --collection because the environment mounts two collections.
const collectionArg = "User creation"

func TestBlueprintLs_should_list_blueprints_with_their_parameters(t *testing.T) {
	e := newSendEnv(t)

	code, out, errOut := e.run(context.Background(), "blueprint", "ls")

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	for _, want := range []string{"User creation", "two-calls", "Two calls", "first", "request-ref", "auth/login"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls lacks %q:\n%s", want, out)
		}
	}
}

func TestBlueprintLs_Given_json_Then_a_list_with_params(t *testing.T) {
	e := newSendEnv(t)

	code, out, _ := e.run(context.Background(), "blueprint", "ls", "--output", "json")

	var got []struct {
		Collection string
		Path       string
		Name       string
		Params     []struct{ Name, Type, Default string }
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || len(got) != 1 {
		t.Fatalf("exit %d err %v\n%s", code, err, out)
	}
	if got[0].Path != "two-calls" || len(got[0].Params) != 2 || got[0].Params[1].Name != "second" || got[0].Params[1].Default != "config" {
		t.Fatalf("got %+v", got[0])
	}
}

func TestBlueprintShow_should_describe_one_blueprint(t *testing.T) {
	e := newSendEnv(t)

	code, out, errOut := e.run(context.Background(), "blueprint", "show", "two-calls")

	if code != 0 || !strings.Contains(out, "Calls one request, then another") || !strings.Contains(out, "second  request-ref  config") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

func TestBlueprintShow_Given_unknown_Then_FLW_BP_006(t *testing.T) {
	e := newSendEnv(t)

	code, _, errOut := e.run(context.Background(), "blueprint", "show", "nope")

	if code != 2 || !strings.Contains(errOut, "FLW-BP-006") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}

func TestNewFlow_from_a_blueprint_should_create_the_flow_with_the_values_given(t *testing.T) {
	e := newSendEnv(t)

	code, out, errOut := e.run(context.Background(), "new", "flow", "team/chain", "--from", "two-calls", "--set", "second=auth/verify", "--collection", collectionArg)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	file := filepath.Join(e.dir, "example", "flows", "team", "chain.arazzo.yaml")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("flow not written: %v (stdout %q)", err, out)
	}
	if !strings.Contains(string(data), "path: auth/verify") || strings.Contains(string(data), "param:") {
		t.Fatalf("flow file:\n%s", data)
	}
	if !strings.Contains(out, "team/chain") {
		t.Fatalf("stdout %q", out)
	}
	if code, _, errOut := e.run(context.Background(), "validate"); code != 0 {
		t.Fatalf("validate after new flow: %d\n%s", code, errOut)
	}
}

func TestNewFlow_without_a_blueprint_should_create_an_empty_flow(t *testing.T) {
	e := newSendEnv(t)

	code, _, errOut := e.run(context.Background(), "new", "flow", "scratch-flow", "--collection", collectionArg)

	if _, err := os.Stat(filepath.Join(e.dir, "example", "flows", "scratch-flow.arazzo.yaml")); code != 0 || err != nil {
		t.Fatalf("exit %d err %v\n%s", code, err, errOut)
	}
}

func TestNewFlow_should_refuse_misuse(t *testing.T) {
	e := newSendEnv(t)
	tests := map[string]struct {
		args   []string
		stderr string
	}{
		"two collections, none named": {[]string{"new", "flow", "x", "--from", "two-calls"}, "FLW-WS-"},
		"set without from":            {[]string{"new", "flow", "x", "--set", "a=b", "--collection", collectionArg}, "--set"},
		"bad set":                     {[]string{"new", "flow", "x", "--from", "two-calls", "--set", "novalue", "--collection", collectionArg}, "--set"},
		"unknown parameter":           {[]string{"new", "flow", "x", "--from", "two-calls", "--set", "colour=red", "--collection", collectionArg}, "FLW-BP-005"},
		"unknown blueprint":           {[]string{"new", "flow", "x", "--from", "nope", "--collection", collectionArg}, "FLW-BP-006"},
		"path traversal":              {[]string{"new", "flow", "../escape", "--collection", collectionArg}, "FLW-WRITE-002"},
		"taken":                       {[]string{"new", "flow", "user-creation", "--collection", collectionArg}, "FLW-WRITE-002"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			code, _, errOut := e.run(context.Background(), tt.args...)

			if code != 2 || !strings.Contains(errOut, tt.stderr) {
				t.Fatalf("exit %d\n%s", code, errOut)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(e.dir, "escape.arazzo.yaml")); err == nil {
		t.Fatal("a flow was written outside the collection")
	}
}

func TestBlueprintCreate_should_save_a_flow_as_a_blueprint_that_new_flow_can_use(t *testing.T) {
	e := newSendEnv(t)

	code, _, errOut := e.run(context.Background(), "blueprint", "create", "onboarding-template", "--from-flow", "onboarding-check", "--name", "Onboarding template", "--collection", collectionArg)
	if code != 0 {
		t.Fatalf("create: exit %d\n%s", code, errOut)
	}
	listed, out, _ := e.run(context.Background(), "blueprint", "ls")
	made, _, errOut2 := e.run(context.Background(), "new", "flow", "onboarding-copy", "--from", "onboarding-template", "--collection", collectionArg)

	if listed != 0 || !strings.Contains(out, "Onboarding template") {
		t.Fatalf("ls: %d\n%s", listed, out)
	}
	if made != 0 {
		t.Fatalf("new flow from the created blueprint: exit %d\n%s", made, errOut2)
	}
}

func TestBlueprintCreate_Given_no_name_Then_the_name_defaults_to_the_path(t *testing.T) {
	e := newSendEnv(t)

	code, _, errOut := e.run(context.Background(), "blueprint", "create", "tpl", "--from-flow", "onboarding-check", "--collection", collectionArg)
	_, out, _ := e.run(context.Background(), "blueprint", "ls")

	if code != 0 || !strings.Contains(out, "tpl") {
		t.Fatalf("exit %d\n%s\n%s", code, errOut, out)
	}
}

func TestValidate_should_report_a_blueprint_that_names_an_undeclared_parameter(t *testing.T) {
	e := newSendEnv(t)
	writeFile(t, filepath.Join(e.dir, "example", "blueprints", "broken.blueprint.yaml"),
		"id: 01JA0000000000000000000031\nname: Broken\nnodes:\n  - id: a\n    type: request\n    request:\n      param: missing\n")

	code, out, errOut := e.run(context.Background(), "validate")

	if code == 0 || !strings.Contains(out, "FLW-BP-003") || !strings.Contains(out, "broken.blueprint.yaml") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

func TestFmt_should_rewrite_a_blueprint_in_canonical_form(t *testing.T) {
	e := newSendEnv(t)
	file := filepath.Join(e.dir, "example", "blueprints", "two-calls.blueprint.yaml")
	writeFile(t, file, "name: Two calls\nid: 01JA0000000000000000000024\nparams: {first: {type: request-ref, default: auth/login}}\nnodes:\n  - {id: a, type: request, request: {param: first}}\n")

	code, _, errOut := e.run(context.Background(), "fmt", filepath.Join(e.dir, "example"))

	data, _ := os.ReadFile(file)
	if code != 0 || !strings.HasPrefix(string(data), "id: 01JA0000000000000000000024\nname: Two calls\nparams:\n  first:\n    type: request-ref") {
		t.Fatalf("exit %d\n%s\nfile:\n%s", code, errOut, data)
	}
}
