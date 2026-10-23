// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/samber/do/v2"

	"github.com/githugt/flowi-lib/examples"
	"github.com/githugt/flowi-lib/examples/demoserver"
	"github.com/githugt/flowi-lib/settings"
	"github.com/githugt/flowi-lib/workspace"

	"github.com/githugt/flowi-cli/internal/cli"
)

const demoPassword = "pw-1234567"

// sendEnv is a workspace with the example collection mounted, its local
// environment pointing at a demo server and staging at a closed port.
type sendEnv struct {
	t       *testing.T
	dir     string
	environ []string
}

func newSendEnv(t *testing.T) *sendEnv {
	t.Helper()
	srv := httptest.NewServer(demoserver.Handler(demoserver.Options{Password: demoPassword}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	writeExample(t, filepath.Join(dir, "example"), map[string]string{
		"http://localhost:8080":       srv.URL,
		"https://staging.example.com": "http://127.0.0.1:1",
	})
	writeFile(t, filepath.Join(dir, "tools", "flowi.yaml"), "format: 1\nid: 01JA00000000000000000000T1\nname: Tools\n")
	writeFile(t, filepath.Join(dir, "tools", "requests", "slow.req.yaml"), "id: 01JA00000000000000000000T3\nname: Slow\nversions:\n  \"1\":\n    method: GET\n    url: \"{{env.base_url}}/slow\"\n")
	// Environments are global: the example's become the workspace's.
	envDir := workspace.EnvironmentsDir(settings.Paths{ConfigDir: filepath.Join(dir, "config")}, workspace.DefaultName)
	if err := os.MkdirAll(filepath.Dir(envDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "example", "environments"), envDir); err != nil {
		t.Fatal(err)
	}
	e := &sendEnv{t: t, dir: dir, environ: []string{
		"FLOWI_CONFIG_DIR=" + filepath.Join(dir, "config"), "FLOWI_VAULT_KEY=test-only",
		"FLOWI_SECRET_LOCAL_PASSWORD=" + demoPassword, "FLOWI_SECRET_STAGING_PASSWORD=" + demoPassword,
	}}
	for _, c := range []string{"example", "tools"} {
		if code, _, errOut := e.run(context.Background(), "workspace", "mount", filepath.Join(dir, c)); code != 0 {
			t.Fatalf("mount %s: %d %s", c, code, errOut)
		}
	}
	return e
}

func writeExample(t *testing.T, dst string, replace map[string]string) {
	t.Helper()
	err := fs.WalkDir(examples.UserCreation(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(examples.UserCreation(), p)
		if err != nil {
			return err
		}
		text := string(data)
		for from, to := range replace {
			text = strings.ReplaceAll(text, from, to)
		}
		writeFile(t, filepath.Join(dst, p), text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *sendEnv) without(name string) *sendEnv {
	out := *e
	out.environ = nil
	for _, kv := range e.environ {
		if !strings.HasPrefix(kv, name+"=") {
			out.environ = append(out.environ, kv)
		}
	}
	return &out
}

// run executes flowi in-process with this environment.
func (e *sendEnv) run(ctx context.Context, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	env := cli.Env{Environ: e.environ, Home: e.dir, Cwd: e.dir}
	code := Run(ctx, args, Stdio{Out: &out, Err: &errOut}, WithProvider(func(i do.Injector) { do.OverrideValue(i, env) }))
	return code, out.String(), errOut.String()
}

func TestSend_exit_codes(t *testing.T) {
	e := newSendEnv(t)
	tests := []struct {
		name   string
		env    *sendEnv
		args   []string
		code   int
		stderr []string
	}{
		{"sent", e, []string{"send", "auth/login", "--env", "local", "--with", "email=a@example.com"}, 0, []string{"→ 200"}},
		{"unknown request", e, []string{"send", "nope"}, 2, []string{"FLW-REQ-001"}},
		{"undefined variable", e, []string{"send", "auth/login", "--env", "local"}, 2, []string{"FLW-TPL-001", "email", "auth/login"}},
		{"missing secret", e.without("FLOWI_SECRET_LOCAL_PASSWORD"), []string{"send", "auth/login", "--env", "local", "--with", "email=a"}, 2,
			[]string{"FLW-SEC-001", "flowi secret set local password"}},
		{"failed assertion", e, []string{"send", "auth/verify", "--env", "local", "--with", "token=x", "--with", "subject=y", "--with", "code=1"}, 1,
			[]string{"FLW-RUN-001", "status == 204"}},
		{"connection refused", e, []string{"send", "auth/login", "--env", "staging", "--with", "email=a"}, 4, []string{"FLW-HTTP-001"}},
		{"bad skip", e, []string{"send", "auth/login", "--skip", "middle"}, 2, []string{"--skip"}},
		{"bad with", e, []string{"send", "auth/login", "--with", "novalue"}, 2, []string{"--with"}},
		{"bad save", e, []string{"send", "auth/login", "--save", "always"}, 2, []string{"--save"}},
		{"bad pin name", e, []string{"send", "auth/login", "--env", "local", "--with", "email=a", "--pin=../../escape"}, 2, []string{"FLW-REQ-007"}},
		{"missing argument", e, []string{"send"}, 2, []string{"accepts 1 arg"}},
		{"extra argument", e, []string{"secret", "unset", "local"}, 2, []string{"accepts 2 arg"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := tt.env.run(context.Background(), tt.args...)

			if code != tt.code {
				t.Fatalf("exit %d, want %d\n%s", code, tt.code, errOut)
			}
			for _, want := range tt.stderr {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr lacks %q:\n%s", want, errOut)
				}
			}
			if strings.Contains(errOut, demoPassword) {
				t.Errorf("password in stderr:\n%s", errOut)
			}
		})
	}
}

func TestSend_Given_interrupt_Then_exit_3(t *testing.T) {
	e := newSendEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()

	code, _, errOut := e.run(ctx, "send", "slow", "--env", "local")

	if code != cli.ExitCancelled || time.Since(start) > 2*time.Second {
		t.Fatalf("exit %d after %v\n%s", code, time.Since(start), errOut)
	}
}

func TestSend_should_print_body_to_stdout_and_summary_to_stderr(t *testing.T) {
	e := newSendEnv(t)

	code, out, errOut := e.run(context.Background(), "send", "auth/login", "--env", "local", "--with", "email=a@example.com", "--prefer-version", "2")

	if code != 0 || !strings.Contains(out, `"token"`) || strings.Contains(out, "→") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	for _, want := range []string{"POST ", "→ 200 OK", "version 2, preferred", "seed ", "✓ pre  folder  inline"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
}

// wsServer answers every message with "echo:<message>" when reply is true,
// and stays silent otherwise.
func wsServer(t *testing.T, reply bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		for {
			_, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if reply {
				_ = c.Write(r.Context(), websocket.MessageText, append([]byte("echo:"), data...))
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestSend_async_request(t *testing.T) {
	e := newSendEnv(t)
	writeFile(t, filepath.Join(e.dir, "tools", "requests", "echo.req.yaml"), `id: 01JA00000000000000000000T4
name: Echo
versions:
  "1":
    async:
      protocol: ws
      server: `+wsServer(t, true)+`
      channel: chat
      send:
        body:
          text: ping
      receive:
        timeout: 5s
    assert:
      - body == "echo:ping"
`)
	writeFile(t, filepath.Join(e.dir, "tools", "requests", "mute.req.yaml"), `id: 01JA00000000000000000000T5
name: Mute
versions:
  "1":
    async:
      protocol: ws
      server: `+wsServer(t, false)+`
      send:
        body:
          text: ping
      receive:
        timeout: 200ms
`)

	code, out, errOut := e.run(t.Context(), "send", "echo")
	timeoutCode, _, timeoutErr := e.run(t.Context(), "send", "mute")

	if code != 0 || !strings.Contains(errOut, "WS ") || !strings.Contains(errOut, "1 message") || strings.TrimSpace(out) != "echo:ping" {
		t.Fatalf("send echo: code %d\nstdout: %q\nstderr: %s", code, out, errOut)
	}
	if timeoutCode != 4 || !strings.Contains(timeoutErr, "FLW-ASYNC-005") {
		t.Fatalf("send mute: code %d\nstderr: %s", timeoutCode, timeoutErr)
	}
}
