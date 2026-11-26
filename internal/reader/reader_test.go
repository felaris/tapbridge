package reader

import (
	"testing"

	"github.com/ebfe/scard"
)

func TestPickReader(t *testing.T) {
	cases := []struct {
		name     string
		readers  []string
		selected string
		want     string
	}{
		{"empty", nil, "", ""},
		{"single", []string{"ACS ACR122 0"}, "", "ACS ACR122 0"},
		{"selected wins", []string{"A", "B"}, "B", "B"},
		{"stale selection falls back", []string{"A", "B"}, "gone", "A"},
		{"skip SAM listed first (Windows)", []string{"ACS ACR1252 1S CL Reader SAM 0", "ACS ACR1252 1S CL Reader PICC 0"}, "", "ACS ACR1252 1S CL Reader PICC 0"},
		{"prefer contactless", []string{"Generic Smart Card Reader 0", "Identiv uTrust 3700 F Contactless Reader 0"}, "", "Identiv uTrust 3700 F Contactless Reader 0"},
		{"non-SAM over SAM", []string{"Foo SAM 0", "Foo Reader 0"}, "", "Foo Reader 0"},
		{"only SAM", []string{"Foo SAM 0"}, "", "Foo SAM 0"},
		{"explicit SAM selection respected", []string{"Foo SAM 0", "Foo PICC 0"}, "Foo SAM 0", "Foo SAM 0"},
	}
	for _, c := range cases {
		if got := PickReader(c.readers, c.selected); got != c.want {
			t.Errorf("%s: PickReader = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCardChangedIgnoresInUse(t *testing.T) {
	base := scard.StatePresent | 0x00010000
	if cardChanged(base, base|scard.StateInuse) {
		t.Error("toggling InUse (our own connect) must not count as a new card")
	}
	if cardChanged(base, base|scard.StateExclusive) {
		t.Error("toggling Exclusive must not count as a new card")
	}
	if !cardChanged(base, scard.StatePresent|0x00020000) {
		t.Error("event counter bump (card swapped) must count as a change")
	}
	if !cardChanged(scard.StateEmpty, scard.StatePresent) {
		t.Error("empty -> present must count as a change")
	}
}

func TestRequestWriteQueuesOnlyOne(t *testing.T) {
	s := New(Events{}, nil)
	if err := s.RequestWrite(WriteRequest{ID: "a"}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := s.RequestWrite(WriteRequest{ID: "b"}); err != ErrWritePending {
		t.Fatalf("second write = %v, want ErrWritePending", err)
	}
}

func TestReportReadersOnlyOnChange(t *testing.T) {
	calls := 0
	var gotActive string
	s := New(Events{Readers: func(r []string, active string) { calls++; gotActive = active }}, nil)
	var m readerMenuState
	m.selected = "\x00"

	readers := []string{"X SAM 0", "X PICC 0"}
	s.reportReaders(&m, readers, "")
	s.reportReaders(&m, readers, "")
	if calls != 1 {
		t.Fatalf("Readers called %d times for an unchanged list, want 1", calls)
	}
	if gotActive != "X PICC 0" {
		t.Errorf("active = %q, want the PICC slot", gotActive)
	}
	s.reportReaders(&m, readers, "X SAM 0")
	if calls != 2 || gotActive != "X SAM 0" {
		t.Errorf("selection change not reported (calls=%d active=%q)", calls, gotActive)
	}
}

func TestCanceledWriteIsReplacedAndDropped(t *testing.T) {
	s := New(Events{}, nil)
	gone := make(chan struct{})
	if err := s.RequestWrite(WriteRequest{ID: "a", Canceled: gone}); err != nil {
		t.Fatal(err)
	}
	close(gone) // the requesting tab closed
	if s.hasWrite() {
		t.Error("a canceled write must not count as pending")
	}
	if _, ok := s.takeWrite(); ok {
		t.Error("a canceled write must never be performed")
	}

	gone2 := make(chan struct{})
	_ = s.RequestWrite(WriteRequest{ID: "b", Canceled: gone2})
	close(gone2)
	if err := s.RequestWrite(WriteRequest{ID: "c"}); err != nil {
		t.Fatalf("a new request must replace a canceled one, got %v", err)
	}
	if req, ok := s.takeWrite(); !ok || req.ID != "c" {
		t.Errorf("takeWrite = %+v, %v; want c", req, ok)
	}
}
