# Changelog

All notable changes to TapBridge are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Releases are
published automatically from `main`. See the
[releases page](https://github.com/felaris/tapbridge/releases) for builds.

## [Unreleased]

### Fixed
- **Windows:** the reader is detected again after unplug/replug. Windows stops
  its Smart Card service when the last reader is removed, and TapBridge now
  re-establishes the PC/SC context instead of polling a dead one forever.
- **Windows:** dual-slot readers (ACR1252U, ACR1281U, …) now use the
  contactless `PICC` slot instead of the `SAM` slot.
- **Windows:** taps no longer fail intermittently while Smart Card Plug and
  Play holds a freshly tapped card (connect is retried).
- **Windows:** the tray menu no longer flickers or closes itself while open.
- Crash when the NFC library panicked on an unexpected card protocol.
- Crash ("concurrent write to websocket connection") when a tap was broadcast
  while a write result was being sent.
- Long NDEF records written by TapBridge now read back in full (reads used to
  stop at 48 bytes).
- A write requested by a page that has since closed is cancelled, so it is no
  longer written onto the next card tapped.
- Writes larger than the tag's memory are refused instead of overwriting the
  tag's lock/config pages.

### Added
- Plain-English error messages with stable error `code`s on `write_error`, and
  a new `error` message type ([docs](docs/protocol.md#error-codes)).
- Pages from a disallowed origin now receive an explanation (including the
  exact `--allow-origin` flag to use) instead of an opaque connection failure.
- Log file (`tapbridge.log`) and an **Open Log File** tray item.
- Version shown in the tray menu, plus a `--version` flag.
- IPv6 loopback listener so `ws://localhost` works for IPv6-first clients.
- Plain HTTP `GET` health check.
- NDEF parsing of 3-byte TLV lengths, normal-length records and records with
  an ID field.
- Demo page in `examples/browser/`, documentation in `docs/`, a `Makefile`,
  CI on Windows, macOS and Linux, and SHA-256 checksums on releases.

### Changed
- The Windows build is a GUI app with no console window.
- Code reorganised into `cmd/` and `internal/` packages.
- A `write` with an empty `id` now returns `write_error` (`invalid_request`)
  instead of being silently ignored. Malformed or unknown messages return an
  `error` message.
