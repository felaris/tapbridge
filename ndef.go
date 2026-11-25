package main

import (
	"fmt"
	"strings"
)

// ── NDEF parsing ──────────────────────────────────────────────────────────────

// parseNdefRecord extracts an ID from the first record of an NDEF message.
// It handles both short (SR=1) and normal (4-byte payload length) records and
// records carrying an ID field (IL=1).
func parseNdefRecord(msg []byte) string {
	if len(msg) < 3 {
		return ""
	}
	hdr := msg[0]
	sr := hdr&0x10 != 0
	il := hdr&0x08 != 0
	i := 1
	typeLen := int(msg[i])
	i++
	var payLen int
	if sr {
		payLen = int(msg[i])
		i++
	} else {
		if len(msg) < i+4 {
			return ""
		}
		payLen = int(msg[i])<<24 | int(msg[i+1])<<16 | int(msg[i+2])<<8 | int(msg[i+3])
		i += 4
	}
	idLen := 0
	if il {
		if len(msg) <= i {
			return ""
		}
		idLen = int(msg[i])
		i++
	}
	if payLen < 0 || len(msg) < i+typeLen+idLen+payLen {
		return ""
	}
	recType := string(msg[i : i+typeLen])
	i += typeLen + idLen
	payload := msg[i : i+payLen]

	switch recType {
	case "U":
		if len(payload) < 1 {
			return ""
		}
		prefix := ""
		if int(payload[0]) < len(uriPrefixes) {
			prefix = uriPrefixes[payload[0]]
		}
		url := prefix + string(payload[1:])
		if idx := strings.Index(url, "/verify/"); idx >= 0 {
			rest := url[idx+8:]
			if end := strings.IndexAny(rest, "/?#"); end >= 0 {
				return rest[:end]
			}
			return rest
		}
	case "T":
		if len(payload) < 1 {
			return ""
		}
		langLen := int(payload[0] & 0x3f)
		if 1+langLen >= len(payload) {
			return ""
		}
		return strings.TrimSpace(string(payload[1+langLen:]))
	}
	return ""
}

// findNdefTlv walks Type 2 tag TLV blocks and returns the NDEF message TLV
// value. complete is false when more data must be read to decide.
func findNdefTlv(data []byte) (value []byte, complete bool) {
	i := 0
	for i < len(data) {
		t := data[i]
		i++
		switch t {
		case 0x00: // NULL TLV
			continue
		case 0xFE: // terminator
			return nil, true
		}
		if i >= len(data) {
			return nil, false
		}
		l := int(data[i])
		i++
		if l == 0xFF { // 3-byte length format
			if i+2 > len(data) {
				return nil, false
			}
			l = int(data[i])<<8 | int(data[i+1])
			i += 2
		}
		if i+l > len(data) {
			return nil, false
		}
		if t == 0x03 {
			return data[i : i+l], true
		}
		i += l
	}
	return nil, false
}

// ndefComplete reports whether data already holds a complete NDEF TLV (or a
// terminator), so the reader can stop issuing READ commands.
func ndefComplete(data []byte) bool {
	_, complete := findNdefTlv(data)
	return complete
}

func parseNdef(data []byte) string {
	v, _ := findNdefTlv(data)
	if len(v) == 0 {
		return ""
	}
	return parseNdefRecord(v)
}

// ── NDEF writing ──────────────────────────────────────────────────────────────

// uriPrefixes is the standard NDEF URI record abbreviation table (index 0 = no
// abbreviation). Only the entries this bridge knows how to round-trip are
// populated; the rest read as "" per the NDEF spec.
var uriPrefixes = []string{"", "http://www.", "https://www.", "http://", "https://"}

// uriPrefixOrder lists uriPrefixes indices from most to least specific, so
// buildUriNdef picks the longest matching prefix (smallest payload).
var uriPrefixOrder = []int{2, 1, 4, 3}

// maxRecordLen is the largest NDEF record this bridge encodes. Records use the
// short-record form (1-byte payload length) inside a single-byte-length TLV, so
// the record must fit in one byte; 0xFF is reserved as the TLV 3-byte-length
// marker, leaving 0xFE (254) as the maximum. Larger inputs would overflow the
// length bytes and produce a malformed message.
const maxRecordLen = 0xFE

// wrapTlv wraps a raw NDEF record in a TLV block, terminated and padded to a
// 4-byte page boundary for writing to the card. It errors if the record is too
// large for the single-byte TLV length field.
func wrapTlv(record []byte) ([]byte, error) {
	if len(record) > maxRecordLen {
		return nil, fmt.Errorf("ndef record too large: %d bytes (max %d)", len(record), maxRecordLen)
	}
	msg := []byte{0x03, byte(len(record))}
	msg = append(msg, record...)
	msg = append(msg, 0xFE)
	for len(msg)%4 != 0 {
		msg = append(msg, 0x00)
	}
	return msg, nil
}

// buildTextNdef encodes text as a well-known Text NDEF record wrapped in TLV.
func buildTextNdef(text string) ([]byte, error) {
	lang := "en"
	payload := make([]byte, 0, 1+len(lang)+len(text))
	payload = append(payload, byte(len(lang)))
	payload = append(payload, []byte(lang)...)
	payload = append(payload, []byte(text)...)

	// MB=1 ME=1 SR=1 TNF=0x01 (Well-Known) | type_len=1 | payload_len | 'T' | payload
	record := []byte{0xD1, 0x01, byte(len(payload)), 'T'}
	record = append(record, payload...)
	return wrapTlv(record)
}

// buildUriNdef encodes a URI as a well-known URI NDEF record wrapped in TLV,
// abbreviating the scheme using the standard NDEF URI prefix table where
// possible. Note: parseNdefRecord only extracts an ID from URLs containing
// "/verify/<id>" — a plain URI without that segment writes fine but this
// bridge's own reader will fall back to the card's raw UID when it taps it.
func buildUriNdef(uri string) ([]byte, error) {
	code := byte(0)
	rest := uri
	for _, i := range uriPrefixOrder {
		if strings.HasPrefix(uri, uriPrefixes[i]) {
			code = byte(i)
			rest = uri[len(uriPrefixes[i]):]
			break
		}
	}

	payload := make([]byte, 0, 1+len(rest))
	payload = append(payload, code)
	payload = append(payload, []byte(rest)...)

	record := []byte{0xD1, 0x01, byte(len(payload)), 'U'}
	record = append(record, payload...)
	return wrapTlv(record)
}
