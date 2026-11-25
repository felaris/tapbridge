package main

import (
	"strings"
	"testing"
)

func TestBuildTextNdefRoundTrip(t *testing.T) {
	cases := []string{"abc123", "hello-world_ID.99", "https://example.com/verify/xyz"}
	for _, text := range cases {
		data, err := buildTextNdef(text)
		if err != nil {
			t.Errorf("buildTextNdef(%q) unexpected error: %v", text, err)
			continue
		}
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
		data, err := buildUriNdef(c.uri)
		if err != nil {
			t.Errorf("buildUriNdef(%q) unexpected error: %v", c.uri, err)
			continue
		}
		if len(data)%4 != 0 {
			t.Errorf("buildUriNdef(%q) length %d not page-aligned", c.uri, len(data))
		}
		if got := parseNdef(data); got != c.want {
			t.Errorf("round trip uri %q = %q, want %q", c.uri, got, c.want)
		}
	}
}

func TestBuildUriNdefPicksLongestPrefix(t *testing.T) {
	data, err := buildUriNdef("https://www.example.com/verify/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// record layout: [0x03 len 0xD1 0x01 payLen 'U' code ...]
	code := data[6]
	if code != 2 { // index of "https://www."
		t.Errorf("expected prefix code 2 (https://www.), got %d", code)
	}
}

func TestBuildNdefRejectsOversized(t *testing.T) {
	// A payload larger than a single-byte length field can hold must be
	// rejected rather than silently truncated into a malformed record.
	long := strings.Repeat("A", 300)
	if _, err := buildTextNdef(long); err == nil {
		t.Error("buildTextNdef(300 bytes) should return an error")
	}
	if _, err := buildUriNdef("https://example.com/verify/" + long); err == nil {
		t.Error("buildUriNdef(oversized) should return an error")
	}
	// A value comfortably within range must still succeed.
	if _, err := buildTextNdef(strings.Repeat("A", 200)); err != nil {
		t.Errorf("buildTextNdef(200 bytes) unexpected error: %v", err)
	}
}

func TestParseNdefIgnoresNonNdefTlv(t *testing.T) {
	// Lock control TLV (0x01) followed by an NDEF message TLV.
	rec, err := buildTextNdef("id1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := []byte{0x01, 0x03, 0x00, 0x00, 0x00}
	msg = append(msg, rec...)
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

func TestParseNdefLongTlvAndNormalRecord(t *testing.T) {
	text := strings.Repeat("x", 300)
	payload := append([]byte{2, 'e', 'n'}, []byte(text)...)
	// MB=1 ME=1 SR=0 TNF=1, type len 1, 4-byte payload length, 'T'
	n := len(payload)
	record := []byte{0xC1, 0x01, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n), 'T'}
	record = append(record, payload...)
	data := []byte{0x03, 0xFF, byte(len(record) >> 8), byte(len(record))}
	data = append(data, record...)
	data = append(data, 0xFE)
	if got := parseNdef(data); got != text {
		t.Errorf("long TLV parse = %d chars, want %d", len(got), len(text))
	}
	if ndefComplete(data[:40]) {
		t.Error("truncated TLV must not be reported complete")
	}
	if !ndefComplete(data) {
		t.Error("full TLV must be reported complete")
	}
}

func TestParseNdefRecordWithIDField(t *testing.T) {
	// MB=1 ME=1 SR=1 IL=1 TNF=1, type len 1, payload len, id len 2, 'T', id, payload
	payload := []byte{2, 'e', 'n', 'h', 'i'}
	record := []byte{0xD9, 0x01, byte(len(payload)), 0x02, 'T', 'a', 'b'}
	record = append(record, payload...)
	if got := parseNdefRecord(record); got != "hi" {
		t.Errorf("record with ID field = %q, want %q", got, "hi")
	}
}

func TestNdefCompleteOnTerminatorOrBlank(t *testing.T) {
	if !ndefComplete([]byte{0xFE, 0, 0, 0}) {
		t.Error("terminator-only data should be complete")
	}
	if ndefComplete([]byte{0, 0, 0, 0}) {
		t.Error("all-NULL data needs more reading")
	}
}
