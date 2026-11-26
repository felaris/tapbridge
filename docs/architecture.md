# Architecture

TapBridge is a single Go binary with no runtime dependencies beyond the OS's
PC/SC service.

```
            ┌──────────────────────── tapbridge ────────────────────────┐
 USB        │                                                           │   WebSocket
 reader ───▶│ internal/reader ──Events──▶ cmd/tapbridge ──▶ internal/server ├──▶ browser tabs
 (PC/SC)    │   poll loop        (wiring)        │          hub + protocol │◀── write requests
            │       ▲                            ▼                        │
            │       └──── WriteRequest ─── internal/tray (status, menu)   │
            │                                                           │
            │ internal/config   internal/ndef   internal/logging  internal/autostart │
            └───────────────────────────────────────────────────────────┘
```

## Layout

```
cmd/tapbridge/        entry point: flags, wiring, listening; ui_tray.go / headless.go front-ends
internal/config/      settings layering, persistence, origin allowlist
internal/ndef/        NDEF Text/URI encode + decode (pure, no I/O)
internal/reader/      PC/SC polling, reader selection, card I/O, readable errors
internal/server/      WebSocket protocol, client hub, origin enforcement
internal/tray/        system tray menu
internal/autostart/   Start at Login (LaunchAgent / Run registry key)
internal/logging/     log file + "Open Log File"
assets/               embedded tray icons
scripts/              install.sh (macOS), gen_icon.go
examples/browser/     a standalone demo page
Dockerfile, docker/   headless Linux image (pcscd bundled, non-root)
docs/                 user and developer documentation
```

The packages don't depend on each other. `reader` reports through
callbacks (`reader.Events`), `server` asks for writes through
`server.Options.OnWrite`, and `cmd/tapbridge` connects the two. This keeps
`ndef`, `config`, `server` and most of `reader` testable without hardware or
a GUI.

## The reader loop

`reader.Service.Run` is built to survive PC/SC's failure modes:

1. **Context lifecycle.** It establishes a PC/SC context and polls until the
   context dies, then starts over. Windows stops the Smart Card service
   when the last reader is unplugged, which invalidates the context.
2. **Waiting for a card.** It blocks in `SCardGetStatusChange` (300 ms
   timeout) instead of calling `SCardConnect` in a loop. A state change
   counts as a new card only if the present/empty/mute bits or the
   insertion counter change. The in-use bits flip every time TapBridge
   connects, so they're ignored.
3. **Connecting.** It retries briefly on sharing violations, because Windows
   Smart Card Plug and Play holds new cards for a moment.
4. **Reading.** It reads the capability container to find the tag size,
   then reads 16 bytes at a time until the NDEF TLV is complete.
5. **Writing.** A pending `WriteRequest` takes priority on the next tap. If
   the requesting tab disconnects, the request is dropped.
6. **Errors.** Every failure goes through `reader.Explain`, which turns
   PC/SC codes and card status words into a stable `code` and a
   plain-English `message`.
7. **Panics.** `transmit` and the poll loop recover from panics, because
   ebfe/scard panics on unexpected protocols on Windows.

## Concurrency

- One goroutine runs the reader loop. It's the only code that touches PC/SC.
- `net/http` runs one goroutine per WebSocket client. Each client has a
  write mutex, because gorilla/websocket allows only one writer at a time.
  Broadcasts copy the client list before sending.
- Tray callbacks run on goroutines started by `tray.New`.
- `config.Store` guards the live config with a mutex. The reader reads the
  selected reader from it on every loop, so tray changes apply immediately.
