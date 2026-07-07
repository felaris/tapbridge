package main

import (
	"strings"
	"testing"
)

func TestBuildTextNdefRoundTrip(t *testing.T) {
	cases := []string{"abc123", "hello-world_ID.99", "https://example.com/verify/xyz"}
	for _, text := range cases {
		data := buildTextNdef(text)
		if len(data)%4 != 0 {
			t.Errorf("buildTextNdef(%q) length %d not page-aligned", text, len(data))
		}
		if got := parseNdef(data); got != text {
			t.Errorf("round trip text %q = %q, want %q", text, got, text)
		}
	}
}

func TestBuildUriNdefRoundTrip(t *testing.T) {
	cases := []struct {
		uri  string
		want string // what this bridge's own reader extracts back
	}{
		{"https://www.example.com/verify/abc123", "abc123"},
		{"http://example.com/verify/xyz", "xyz"},
		{"https://example.com/verify/id?foo=bar", "id"},
		{"https://example.com", ""}, // no /verify/ segment: nothing to extract
		{"ftp://example.com/file", ""},
	}
	for _, c := range cases {
		data := buildUriNdef(c.uri)
		if len(data)%4 != 0 {
			t.Errorf("buildUriNdef(%q) length %d not page-aligned", c.uri, len(data))
		}
		if got := parseNdef(data); got != c.want {
			t.Errorf("round trip uri %q = %q, want %q", c.uri, got, c.want)
		}
	}
}

func TestBuildUriNdefPicksLongestPrefix(t *testing.T) {
	data := buildUriNdef("https://www.example.com/verify/abc")
	// record layout: [0x03 len 0xD1 0x01 payLen 'U' code ...]
	code := data[6]
	if code != 2 { // index of "https://www."
		t.Errorf("expected prefix code 2 (https://www.), got %d", code)
	}
}

func TestParseNdefIgnoresNonNdefTlv(t *testing.T) {
	// Lock control TLV (0x01) followed by an NDEF message TLV.
	msg := []byte{0x01, 0x03, 0x00, 0x00, 0x00}
	msg = append(msg, buildTextNdef("id1")...)
	if got := parseNdef(msg); got != "id1" {
		t.Errorf("parseNdef with leading non-NDEF TLV = %q, want %q", got, "id1")
	}
}

func TestParseNdefEmptyData(t *testing.T) {
	if got := parseNdef(nil); got != "" {
		t.Errorf("parseNdef(nil) = %q, want empty", got)
	}
}

func TestParseNdefRecordTruncated(t *testing.T) {
	if got := parseNdefRecord([]byte{0xD1, 0x01}); got != "" {
		t.Errorf("parseNdefRecord(truncated) = %q, want empty", got)
	}
}

func TestUriPrefixesTableStable(t *testing.T) {
	// The abbreviation table order is part of the NDEF wire format; make sure
	// nobody reorders it by accident.
	want := "|http://www.|https://www.|http://|https://"
	got := strings.Join(uriPrefixes, "|")
	if got != want {
		t.Errorf("uriPrefixes changed: got %q, want %q", got, want)
	}
}
