// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImpact_exit_codes(t *testing.T) {
	e := newSendEnv(t)
	example := filepath.Join(e.dir, "example", "requests", "auth", "login.examples", "v2-200.json")
	tests := []struct {
		name   string
		args   []string
		edit   func(t *testing.T)
		code   int
		stderr []string
	}{
		{name: "all compatible", args: []string{"impact", "auth/login"}, code: 0},
		{name: "unknown request", args: []string{"impact", "nope"}, code: 2, stderr: []string{"FLW-REQ-001"}},
		{name: "unknown version", args: []string{"impact", "auth/login", "--version", "9"}, code: 2, stderr: []string{"FLW-VER-004"}},
		{name: "missing argument", args: []string{"impact"}, code: 2, stderr: []string{"accepts 1 arg"}},
		{name: "broken consumer", args: []string{"impact", "auth/login"}, edit: func(t *testing.T) {
			data, err := os.ReadFile(example)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, example, strings.Replace(string(data), `"token"`, `"renamed"`, 1))
		}, code: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.edit != nil {
				tt.edit(t)
			}

			code, _, errOut := e.run(context.Background(), tt.args...)

			if code != tt.code {
				t.Fatalf("exit %d, want %d\n%s", code, tt.code, errOut)
			}
			for _, want := range tt.stderr {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr lacks %q:\n%s", want, errOut)
				}
			}
		})
	}
}
