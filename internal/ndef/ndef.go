// Package ndef encodes and decodes the small subset of NFC Data Exchange
// Format (NDEF) that TapBridge uses: a single Text or URI record stored in an
// NFC Forum Type 2 tag (NTAG21x / MIFARE Ultralight) TLV block.
package ndef

import (
	"fmt"
	"strings"
)

// TLV block types used in the Type 2 tag data area.
const (
	tlvNull       = 0x00
	tlvMessage    = 0x03
	tlvTerminator = 0xFE
)

// MaxRecordLen is the largest NDEF record TapBridge encodes. Records use the
// short-record form (1-byte payload length) inside a single-byte-length TLV,
// so the record must fit in one byte; 0xFF is reserved as the TLV 3-byte
// length marker, leaving 0xFE (254) as the maximum.
const MaxRecordLen = 0xFE

// uriPrefixes is the start of the standard NDEF URI abbreviation table
// (index 0 = no abbreviation). Only the entries TapBridge round-trips are
// populated; unknown codes decode with no prefix.
var uriPrefixes = []string{"", "http://www.", "https://www.", "http://", "https://"}

// uriPrefixOrder lists uriPrefixes indices from most to least specific, so
// BuildURI picks the longest matching prefix (smallest payload).
var uriPrefixOrder = []int{2, 1, 4, 3}

// ── Decoding ─────────────────────────────────────────────────────────────────

// ParseID extracts an ID from the raw data area of a Type 2 tag (starting at
// page 4). It returns "" if the data holds no NDEF message TapBridge
// understands; callers then fall back to the card's UID.
//
//   - Text record: the text, trimmed of whitespace.
//   - URI record:  the path segment after "/verify/", if present.
func ParseID(data []byte) string {
	msg, _ := findMessage(data)
	if len(msg) == 0 {
		return ""
	}
	return ParseRecord(msg)
}

// Complete reports whether data already contains a full NDEF message TLV or a
// terminator, so a reader can stop issuing READ commands early.
func Complete(data []byte) bool {
	_, complete := findMessage(data)
	return complete
}

// ParseRecord extracts an ID from the first record of an NDEF message. It
// handles short (SR=1) and normal (4-byte payload length) records, and
// records carrying an ID field (IL=1).
func ParseRecord(msg []byte) string {
	if len(msg) < 3 {
		return ""
	}
	hdr := msg[0]
	shortRecord := hdr&0x10 != 0
	hasID := hdr&0x08 != 0

	i := 1
	typeLen := int(msg[i])
	i++

	var payLen int
	if shortRecord {
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
	if hasID {
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
		return parseURIPayload(payload)
	case "T":
		return parseTextPayload(payload)
	}
	return ""
}

func parseURIPayload(payload []byte) string {
	if len(payload) < 1 {
		return ""
	}
	prefix := ""
	if int(payload[0]) < len(uriPrefixes) {
		prefix = uriPrefixes[payload[0]]
	}
	uri := prefix + string(payload[1:])
	idx := strings.Index(uri, "/verify/")
	if idx < 0 {
		return ""
	}
	rest := uri[idx+len("/verify/"):]
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		return rest[:end]
	}
	return rest
}

func parseTextPayload(payload []byte) string {
	if len(payload) < 1 {
		return ""
	}
	langLen := int(payload[0] & 0x3f)
	if 1+langLen >= len(payload) {
		return ""
	}
	return strings.TrimSpace(string(payload[1+langLen:]))
}

// findMessage walks the TLV blocks and returns the NDEF message TLV's value.
// complete is false when more data must be read before deciding.
func findMessage(data []byte) (value []byte, complete bool) {
	i := 0
	for i < len(data) {
		t := data[i]
		i++
		switch t {
		case tlvNull:
			continue
		case tlvTerminator:
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
		if t == tlvMessage {
			return data[i : i+l], true
		}
		i += l
	}
	return nil, false
}

// ── Encoding ─────────────────────────────────────────────────────────────────

// BuildText encodes text as a well-known Text record ("en") wrapped in a TLV
// block, terminated and padded to a 4-byte page boundary.
func BuildText(text string) ([]byte, error) {
	const lang = "en"
	payload := make([]byte, 0, 1+len(lang)+len(text))
	payload = append(payload, byte(len(lang)))
	payload = append(payload, lang...)
	payload = append(payload, text...)
	return wrap(shortRecord('T', payload))
}

// BuildURI encodes uri as a well-known URI record wrapped in a TLV block,
// abbreviating the scheme with the standard NDEF prefix table where possible.
//
// Note that ParseID only extracts an ID from URIs containing "/verify/<id>";
// other URIs write fine but read back as the card's UID.
func BuildURI(uri string) ([]byte, error) {
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
	payload = append(payload, rest...)
	return wrap(shortRecord('U', payload))
}

// shortRecord builds a single well-known short record:
// MB=1 ME=1 SR=1 TNF=0x01 | type length 1 | payload length | type | payload.
// Oversized payloads are caught by wrap before the length byte matters.
func shortRecord(recType byte, payload []byte) []byte {
	record := []byte{0xD1, 0x01, byte(len(payload)), recType}
	return append(record, payload...)
}

// TooLargeError is returned when a value does not fit in one NDEF record.
type TooLargeError struct {
	Size int // encoded record size in bytes
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("ndef record too large: %d bytes (max %d)", e.Size, MaxRecordLen)
}

func wrap(record []byte) ([]byte, error) {
	if len(record) > MaxRecordLen {
		return nil, &TooLargeError{Size: len(record)}
	}
	msg := []byte{tlvMessage, byte(len(record))}
	msg = append(msg, record...)
	msg = append(msg, tlvTerminator)
	for len(msg)%4 != 0 {
		msg = append(msg, tlvNull)
	}
	return msg, nil
}
