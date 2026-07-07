# TapBridge

A lightweight, cross-platform system tray application that bridges a USB NFC card reader (e.g. **ACR122U**) to any browser-based web app over a local WebSocket connection.

It runs quietly in your menu bar / system tray, watches for NFC cards, and streams tapped card IDs straight into your web app — no browser extensions, no drivers to configure manually, no native messaging hacks.

## Contents

- [How it works](#how-it-works)
- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Usage](#usage)
- [WebSocket protocol](#websocket-protocol)
- [NDEF format](#ndef-format)
- [Project structure](#project-structure)
- [Development](#development)
- [License](#license)

## How it works

```
 ┌──────────────────┐       PC/SC       ┌───────────────────────┐      WebSocket      ┌──────────────┐
 │  NFC card /      │ ───────────────▶  │       TapBridge        │ ──────────────────▶ │  Browser /   │
 │  ACR122U reader  │                   │  (system tray app)     │ ws://localhost:8765 │  Web app     │
 └──────────────────┘                   └───────────────────────┘ ◀────────────────── └──────────────┘
                                                                       write requests
```

The bridge polls the connected PC/SC reader for card taps, reads the card's NDEF data (or falls back to its raw UID), and broadcasts the result to every connected WebSocket client. It can also write an ID back onto a blank/rewritable card on request from the browser.

## Features

- 🖥️ **System tray app** — runs in the background on macOS and Windows, no terminal window required
- 🔌 **PC/SC based** — works with any PC/SC-compatible reader (tested with the ACR122U)
- 🌐 **WebSocket bridge** — any web page can connect to `ws://localhost:8765` and receive card taps in real time
- 📖 **NDEF read support** — parses Text and URI NDEF records (with a `/verify/<id>` URL convention), falling back to the raw card UID when no NDEF data is present
- ✍️ **NDEF write support** — write a text record to a blank card on request from the browser
- 🔁 **Resilient polling** — automatically recovers from reader disconnects/reconnects

## Requirements

- A PC/SC-compatible NFC reader (e.g. [ACR122U](https://www.acs.com.hk/en/products/3/acr122u-usb-nfc-reader/))
- macOS 11+ or Windows 10/11 (64-bit)
- PC/SC drivers for your reader (macOS has built-in PC/SC support via `pcscd`; Windows requires the manufacturer's driver)

## Installation

### macOS — one-line install (Apple Silicon & Intel)

```bash
curl -fsSL https://github.com/felaris/tapbridge/releases/latest/download/install.sh | bash
```

This downloads the correct binary for your Mac, installs it to `~/.local/bin`, and clears the macOS quarantine flag automatically (no Apple Developer certificate needed).

Then run it:

```bash
tapbridge
```

Or double-click the binary in Finder.

### macOS — manual download

Grab `tapbridge-mac-arm64` (Apple Silicon) or `tapbridge-mac-intel` (Intel) from the [latest release](https://github.com/felaris/tapbridge/releases/latest), then clear the quarantine flag before running it:

```bash
xattr -d com.apple.quarantine ~/Downloads/tapbridge-mac-arm64
chmod +x ~/Downloads/tapbridge-mac-arm64
./tapbridge-mac-arm64
```

### Windows

Download `tapbridge-windows.exe` from the [latest release](https://github.com/felaris/tapbridge/releases/latest) and run it. Windows SmartScreen may warn about an unsigned binary — choose **More info → Run anyway**.

### Building from source

See [Development](#development).

## Usage

1. Plug in your NFC reader (e.g. ACR122U).
2. Launch `tapbridge` — a tray icon appears showing the current status.
3. From your web app, open a WebSocket connection to `ws://localhost:8765`.
4. Tap a card — the bridge reads it and broadcasts the ID to every connected client.

The tray menu shows live status (`Waiting for reader...`, `Card scanned: <id>`, etc.) and the WebSocket URL for quick reference.

## WebSocket protocol

All messages are JSON with a `type` field.

### Server → client

| `type` | Fields | Sent when |
|---|---|---|
| `ready` | — | A client connects |
| `card` | `id` | A card is tapped and read (NDEF-parsed ID, or raw UID as fallback) |
| `write_ok` | `id` | A requested write completed successfully |
| `write_error` | `message` | A write failed, or a write was already pending |

### Client → server

| `type` | Fields | Effect |
|---|---|---|
| `write` | `id` | Requests that the next tapped card be written with the given ID |

**Example — listening for card taps:**

```js
const ws = new WebSocket("ws://localhost:8765");
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === "card") {
    console.log("Card scanned:", msg.id);
  }
};
```

**Example — writing an ID to a card:**

```js
ws.send(JSON.stringify({ type: "write", id: "abc123" }));
// Next tapped card receives the write; listen for "write_ok" / "write_error"
```

## NDEF format

- **URI records** (`U`): if the decoded URL contains `/verify/<id>`, the segment after it is extracted and used as the ID.
- **Text records** (`T`): the raw text (after the language code) is used as-is, trimmed of whitespace.
- **No NDEF data / unrecognized format**: the card's raw UID (hex-encoded) is used instead.

Writes always encode a plain Text NDEF record (language `en`) wrapped in a standard TLV block, padded to a 4-byte page boundary, and written starting at page 4 — compatible with MIFARE Ultralight-family tags.

## Project structure

```
.
├── main.go                    # Application entry point — tray UI, PC/SC polling, WebSocket server, NDEF read/write
├── go.mod / go.sum            # Go module definition
├── assets/
│   └── icon.png                # System tray icon (embedded into the binary at build time)
├── scripts/
│   └── install.sh              # macOS one-line installer (fetches latest release, clears quarantine)
├── .github/
│   └── workflows/
│       └── release.yml         # CI: builds Mac (arm64/intel) + Windows binaries and publishes a GitHub release on every push to main
├── LICENSE
└── README.md
```

## Development

Requires Go 1.22+ and CGO enabled (the PC/SC bindings and system tray both use cgo).

```bash
git clone https://github.com/felaris/tapbridge.git
cd tapbridge
go build -o tapbridge .
./tapbridge
```

### Release process

Every push to `main` triggers [`.github/workflows/release.yml`](.github/workflows/release.yml), which:

1. Computes the next patch version from the latest GitHub release tag
2. Builds macOS (arm64 + Intel) and Windows binaries
3. Tags the commit and publishes a new GitHub release with all binaries plus `install.sh` attached

## License

[MIT](LICENSE)
