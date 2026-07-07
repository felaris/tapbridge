package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/ebfe/scard"
)

const maxReaderSlots = 8

// ── NDEF reading (tries bulk first, falls back to 16-byte chunks) ────────────

func readNdef(card *scard.Card) []byte {
	// Attempt 1: read all 48 bytes at once (pages 4–15)
	r, err := card.Transmit([]byte{0xFF, 0xB0, 0x00, 0x04, 0x30})
	if err == nil && len(r) >= 2 && r[len(r)-2] == 0x90 && r[len(r)-1] == 0x00 && len(r) > 2 {
		log.Printf("[nfc] bulk read ok (%d bytes)", len(r)-2)
		return r[:len(r)-2]
	}
	log.Printf("[nfc] bulk read failed (sw=%X), trying chunks", r)

	// Attempt 2: read 16 bytes at a time (one native MIFARE READ per call)
	var buf []byte
	for page := byte(4); page <= 12; page += 4 {
		r, err := card.Transmit([]byte{0xFF, 0xB0, 0x00, page, 0x10})
		if err != nil || len(r) < 2 || r[len(r)-2] != 0x90 {
			log.Printf("[nfc] chunk read page %d failed", page)
			break
		}
		buf = append(buf, r[:len(r)-2]...)
	}
	return buf
}

// ── NDEF writing ──────────────────────────────────────────────────────────────

// writeNdef writes NDEF data to the card starting at page 4, 4 bytes per page.
func writeNdef(card *scard.Card, data []byte) error {
	for i := 0; i < len(data); i += 4 {
		page := byte(4 + i/4)
		chunk := data[i : i+4]
		cmd := append([]byte{0xFF, 0xD6, 0x00, page, 0x04}, chunk...)
		r, err := card.Transmit(cmd)
		if err != nil {
			return fmt.Errorf("write page %d: %w", page, err)
		}
		if len(r) < 2 || r[len(r)-2] != 0x90 {
			return fmt.Errorf("write page %d: status %X", page, r)
		}
	}
	return nil
}

// ── PC/SC NFC polling ─────────────────────────────────────────────────────────

func runNFC() {
	for {
		ctx, err := scard.EstablishContext()
		if err != nil {
			setStatus("PC/SC unavailable — retrying...")
			time.Sleep(3 * time.Second)
			continue
		}
		poll(ctx)
		ctx.Release()
		time.Sleep(2 * time.Second)
	}
}

// pickReader chooses the active reader: the user's configured selection if
// it's still connected, otherwise the first available reader.
func pickReader(readers []string, selected string) string {
	if selected != "" {
		for _, r := range readers {
			if r == selected {
				return selected
			}
		}
	}
	return readers[0]
}

func poll(ctx *scard.Context) {
	var activeReader string
	lastUID := ""

	for {
		readers, err := ctx.ListReaders()
		if err != nil || len(readers) == 0 {
			updateReaderMenu(nil, "")
			if activeReader != "" {
				activeReader = ""
				lastUID = ""
				setStatus("Reader removed — plug in reader")
			}
			time.Sleep(2 * time.Second)
			continue
		}

		selected := getConfig().SelectedReader
		updateReaderMenu(readers, selected)
		target := pickReader(readers, selected)

		if target != activeReader {
			activeReader = target
			lastUID = ""
			setStatus("Reader ready — tap a card")
		}

		card, err := ctx.Connect(activeReader, scard.ShareShared, scard.ProtocolAny)
		if err != nil {
			lastUID = ""
			time.Sleep(500 * time.Millisecond)
			continue
		}

		resp, err := card.Transmit([]byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
		if err != nil || len(resp) < 2 || resp[len(resp)-2] != 0x90 {
			_ = card.Disconnect(scard.LeaveCard)
			lastUID = ""
			time.Sleep(500 * time.Millisecond)
			continue
		}

		uid := hex.EncodeToString(resp[:len(resp)-2])

		// Write mode: check for a pending write request before doing a read
		select {
		case req := <-writeCh:
			var ndefData []byte
			if req.recordType == "uri" {
				ndefData = buildUriNdef(req.id)
			} else {
				ndefData = buildTextNdef(req.id)
			}
			if err := writeNdef(card, ndefData); err != nil {
				log.Printf("[nfc] write failed: %v", err)
				setStatus("Write failed — try again")
				_ = req.conn.WriteJSON(Msg{Type: "write_error", Message: err.Error()})
			} else {
				log.Printf("[nfc] write ok: %s", req.id)
				setStatus("Write ok: " + req.id)
				_ = req.conn.WriteJSON(Msg{Type: "write_ok", ID: req.id})
			}
			lastUID = uid
			_ = card.Disconnect(scard.LeaveCard)
			time.Sleep(500 * time.Millisecond)
			continue
		default:
		}

		// Read mode
		if uid == lastUID {
			_ = card.Disconnect(scard.LeaveCard)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		lastUID = uid

		id := ""
		ndefData := readNdef(card)
		if len(ndefData) > 0 {
			id = parseNdef(ndefData)
			log.Printf("[nfc] raw ndef (%d bytes): %s", len(ndefData), hex.EncodeToString(ndefData))
		}
		if id == "" {
			log.Printf("[nfc] no ndef parsed, using uid")
			id = uid
		}

		setStatus("Card scanned: " + id)
		setLastScanned(id)
		broadcast(Msg{Type: "card", ID: id})

		_ = card.Disconnect(scard.LeaveCard)
		time.Sleep(500 * time.Millisecond)
	}
}
