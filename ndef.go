package main

import "strings"

// ── NDEF parsing ──────────────────────────────────────────────────────────────

func parseNdefRecord(msg []byte) string {
	if len(msg) < 3 {
		return ""
	}
	typeLen := int(msg[1])
	payLen := int(msg[2])
	if len(msg) < 3+typeLen+payLen {
		return ""
	}
	recType := string(msg[3 : 3+typeLen])
	payload := msg[3+typeLen : 3+typeLen+payLen]

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

func parseNdef(data []byte) string {
	i := 0
	for i < len(data) {
		t := data[i]
		i++
		if t == 0xfe || i >= len(data) {
			break
		}
		if t == 0x00 {
			continue
		}
		l := int(data[i])
		i++
		if t == 0x03 && l > 0 && i+l <= len(data) {
			return parseNdefRecord(data[i : i+l])
		}
		i += l
	}
	return ""
}

// ── NDEF writing ──────────────────────────────────────────────────────────────

// uriPrefixes is the standard NDEF URI record abbreviation table (index 0 = no
// abbreviation). Only the entries this bridge knows how to round-trip are
// populated; the rest read as "" per the NDEF spec.
var uriPrefixes = []string{"", "http://www.", "https://www.", "http://", "https://"}

// uriPrefixOrder lists uriPrefixes indices from most to least specific, so
// buildUriNdef picks the longest matching prefix (smallest payload).
var uriPrefixOrder = []int{2, 1, 4, 3}

// wrapTlv wraps a raw NDEF record in a TLV block, terminated and padded to a
// 4-byte page boundary for writing to the card.
func wrapTlv(record []byte) []byte {
	msg := []byte{0x03, byte(len(record))}
	msg = append(msg, record...)
	msg = append(msg, 0xFE)
	for len(msg)%4 != 0 {
		msg = append(msg, 0x00)
	}
	return msg
}

// buildTextNdef encodes text as a well-known Text NDEF record wrapped in TLV.
func buildTextNdef(text string) []byte {
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
func buildUriNdef(uri string) []byte {
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
