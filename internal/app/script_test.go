// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/githugt/flowi-lib/examples"
	"github.com/githugt/flowi-lib/examples/demoserver"
	"github.com/githugt/flowi-lib/settings"
	"github.com/githugt/flowi-lib/workspace"
)

// scriptPassword is the demo server's password in scripts. Scripts assert
// it never appears in output.
const scriptPassword = "s3cr3t-pw"

// TestMain lets testscript run the real flowi command in-process.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"flowi": func() {
			// Like cmd/flowi: a signal cancels the command's context.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			code := Run(ctx, os.Args[1:], Stdio{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
			stop()
			os.Exit(code)
		},
	})
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(env *testscript.Env) error {
			env.Setenv("FLOWI_CONFIG_DIR", env.WorkDir+"/config")
			env.Setenv("HOME", env.WorkDir+"/home")
			// The vault uses a passphrase key, so scripts never touch the
			// real OS keychain.
			env.Setenv("FLOWI_VAULT_KEY", "script-vault-key")
			srv := httptest.NewServer(demoserver.Handler(demoserver.Options{Password: scriptPassword}))
			env.Defer(srv.Close)
			env.Setenv("SERVER", srv.URL)
			env.Setenv("PASSWORD", scriptPassword)
			if err := copyExample(env.WorkDir+"/example", srv.URL); err != nil {
				return err
			}
			// Environments are global: the example's become the workspace's.
			envDir := workspace.EnvironmentsDir(settings.Paths{ConfigDir: env.WorkDir + "/config"}, workspace.DefaultName)
			if err := os.MkdirAll(filepath.Dir(envDir), 0o755); err != nil {
				return err
			}
			return os.Rename(env.WorkDir+"/example/environments", envDir)
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			// freeport NAME: set $NAME to a TCP port nothing listens on.
			"freeport": func(ts *testscript.TestScript, _ bool, args []string) {
				l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
				ts.Check(err)
				_, port, err := net.SplitHostPort(l.Addr().String())
				ts.Check(err)
				ts.Check(l.Close())
				ts.Setenv(args[0], port)
			},
			// http-wait URL: wait until the URL answers (with any status).
			"http-wait": func(ts *testscript.TestScript, _ bool, args []string) {
				deadline := time.Now().Add(20 * time.Second)
				for {
					res, err := httpDo(http.MethodGet, args[0], nil, "")
					if err == nil {
						_ = res.Body.Close()
						return
					}
					if time.Now().After(deadline) {
						ts.Fatalf("%s never answered: %v", args[0], err)
					}
					<-time.After(50 * time.Millisecond) // polling a server that is starting in another process
				}
			},
			// http METHOD URL OUTFILE [name=value ...]: call the URL and write
			// "STATUS <code>", the headers and the body to OUTFILE. The pair
			// body=... is the request body; other pairs are request headers.
			"http": func(ts *testscript.TestScript, _ bool, args []string) {
				if len(args) < 3 {
					ts.Fatalf("usage: http METHOD URL OUTFILE [name=value ...]")
				}
				headers, body := map[string]string{}, ""
				for _, kv := range args[3:] {
					k, v, _ := strings.Cut(kv, "=")
					if k == "body" {
						body = v
					} else {
						headers[k] = v
					}
				}
				res, err := httpDo(args[0], args[1], headers, body)
				ts.Check(err)
				defer res.Body.Close()
				text, err := io.ReadAll(res.Body)
				ts.Check(err)
				var out strings.Builder
				fmt.Fprintf(&out, "STATUS %d\n", res.StatusCode)
				for name, values := range res.Header {
					for _, v := range values {
						fmt.Fprintf(&out, "%s: %s\n", name, v)
					}
				}
				fmt.Fprintf(&out, "\n%s\n", text)
				ts.Check(os.WriteFile(ts.MkAbs(args[2]), []byte(out.String()), 0o644))
			},
			// server-url FILE...: replace SERVER_URL in files with the demo
			// server's URL (txtar files cannot know it in advance).
			"server-url": func(ts *testscript.TestScript, _ bool, args []string) {
				for _, f := range args {
					data := ts.ReadFile(f)
					ts.Check(os.WriteFile(ts.MkAbs(f), []byte(strings.ReplaceAll(data, "SERVER_URL", ts.Getenv("SERVER"))), 0o644))
				}
			},
		},
	})
}

// copyExample writes the example collection to dst with its local
// environment pointing at the demo server.
func copyExample(dst, serverURL string) error {
	return fs.WalkDir(examples.UserCreation(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(examples.UserCreation(), p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, p)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, []byte(strings.ReplaceAll(string(data), "http://localhost:8080", serverURL)), 0o644)
	})
}

func httpDo(method, url string, headers map[string]string, body string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	res.Body = cancelOnClose{res.Body, cancel}
	return res, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error { defer c.cancel(); return c.ReadCloser.Close() }
