// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/githugt/flowi-lib/announce"
)

type newsFeed struct {
	srv *httptest.Server
	pub string
}

// newNewsFeed serves a signed feed with the given item titles.
func newNewsFeed(t *testing.T, titles ...string) newsFeed {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	var items []announce.Item
	for n, title := range titles {
		items = append(items, announce.Item{ID: "n" + string(rune('a'+n)), Title: title, Body: "Details of " + title, URL: "https://example.com/" + string(rune('a'+n)), Level: "info"})
	}
	doc, err := announce.Sign(priv, time.Now(), items)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(doc) }))
	t.Cleanup(srv.Close)
	return newsFeed{srv: srv, pub: base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))}
}

func (f newsFeed) env(e *sendEnv) *sendEnv {
	out := *e
	out.environ = append(append([]string(nil), e.environ...), "FLOWI_ANNOUNCEMENTS_URL="+f.srv.URL, "FLOWI_ANNOUNCEMENTS_KEYS="+f.pub)
	return &out
}

func TestNews_should_list_verified_announcements(t *testing.T) {
	e := newNewsFeed(t, "Flow-I 1.0 is out", "Maintenance window").env(newSendEnv(t))

	code, out, errOut := e.run(context.Background(), "news")

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	for _, want := range []string{"Flow-I 1.0 is out", "Details of Flow-I 1.0 is out", "https://example.com/a", "Maintenance window"} {
		if !strings.Contains(out, want) {
			t.Errorf("news lacks %q:\n%s", want, out)
		}
	}
}

func TestNews_Given_json_output_Then_a_list_of_items(t *testing.T) {
	e := newNewsFeed(t, "Hello").env(newSendEnv(t))

	code, out, _ := e.run(context.Background(), "news", "--output", "json")

	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || code != 0 || len(items) != 1 || items[0]["title"] != "Hello" {
		t.Fatalf("exit %d err %v out %s", code, err, out)
	}
}

func TestNews_Given_no_feed_configured_Then_it_says_so_and_fetches_nothing(t *testing.T) {
	e := newSendEnv(t)

	code, out, _ := e.run(context.Background(), "news")

	if code != 0 || !strings.Contains(out, "No announcements feed is configured") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestNews_Given_the_toggle_off_Then_it_says_so(t *testing.T) {
	e := newNewsFeed(t, "Hello").env(newSendEnv(t))
	e.environ = append(e.environ, "FLOWI_ANNOUNCEMENTS=false")

	code, out, _ := e.run(context.Background(), "news")

	if code != 0 || !strings.Contains(out, "turned off") || strings.Contains(out, "Hello") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestNews_Given_a_feed_signed_by_another_key_Then_nothing_is_shown_and_the_exit_is_a_system_error(t *testing.T) {
	feed := newNewsFeed(t, "Download this installer")
	stranger := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, ed25519.SeedSize))
	feed.pub = base64.StdEncoding.EncodeToString(stranger.Public().(ed25519.PublicKey))
	e := feed.env(newSendEnv(t))

	code, out, errOut := e.run(context.Background(), "news")

	if code != 4 || strings.Contains(out, "installer") || !strings.Contains(errOut, "not signed by a trusted key") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

func TestNews_Given_the_server_is_gone_but_a_cache_exists_Then_cached_items_with_a_note(t *testing.T) {
	feed := newNewsFeed(t, "Cached item")
	e := feed.env(newSendEnv(t))
	if code, _, errOut := e.run(context.Background(), "news"); code != 0 {
		t.Fatalf("priming: %d %s", code, errOut)
	}
	feed.srv.Close()

	code, out, _ := e.run(context.Background(), "news", "--refresh")

	if code != 0 || !strings.Contains(out, "Cached item") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestNews_Given_a_malformed_key_Then_usage_error(t *testing.T) {
	feed := newNewsFeed(t, "Hello")
	feed.pub = "not-a-key"
	e := feed.env(newSendEnv(t))

	code, _, errOut := e.run(context.Background(), "news")

	if code != 2 || !strings.Contains(errOut, "Ed25519") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}
