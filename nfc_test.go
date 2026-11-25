package main

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
		if got := pickReader(c.readers, c.selected); got != c.want {
			t.Errorf("%s: pickReader = %q, want %q", c.name, got, c.want)
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
