// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"testing"
	"time"
)

func TestPlainValue(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{10 * time.Minute, "10m"},
		{time.Hour, "1h"},
		{90 * time.Minute, "1h30m"},
		{[]string{"a.pem", "b.pem"}, "a.pem,b.pem"},
		{int64(10), "10"},
		{true, "true"},
		{nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := plainValue(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDisplayValue_should_quote_empty(t *testing.T) {
	if got := displayValue(""); got != `""` {
		t.Fatalf("got %q", got)
	}
}
