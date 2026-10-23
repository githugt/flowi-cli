// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReadCancelable_Given_done_while_reading_Then_cancelled_and_terminal_restored(t *testing.T) {
	done := make(chan struct{})
	blocked := make(chan struct{})
	restored := false
	time.AfterFunc(50*time.Millisecond, func() { close(done) })

	_, err := readCancelable(func() ([]byte, error) { <-blocked; return nil, nil }, done, func() { restored = true })

	if !errors.Is(err, context.Canceled) || !restored {
		t.Fatalf("err %v restored %v", err, restored)
	}
	close(blocked)
}

func TestReadCancelable_Given_input_Then_returns_it(t *testing.T) {
	got, err := readCancelable(func() ([]byte, error) { return []byte("pw"), nil }, make(chan struct{}), func() { t.Error("restored without cancel") })

	if err != nil || got != "pw" {
		t.Fatalf("got %q %v", got, err)
	}
}
