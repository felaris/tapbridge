# Contributing to TapBridge

Thanks for helping! Bug reports, reader compatibility reports, docs fixes and
code are all welcome.

## Setup

You need **Go** (version in [`go.mod`](go.mod)). On macOS and Linux you also
need a C toolchain (cgo). On Linux, also install the PC/SC and tray headers:

```bash
sudo apt-get install -y libpcsclite-dev libgtk-3-dev libayatana-appindicator3-dev
```

```bash
git clone https://github.com/felaris/tapbridge.git
```

```bash
cd tapbridge && make build && ./tapbridge
```

You **don't need NFC hardware** for most work. The NDEF codec, config,
error explanations, reader selection and the whole WebSocket protocol have
unit and integration tests.

## Project layout

See [docs/architecture.md](docs/architecture.md). In short:

| Path | What lives there |
|---|---|
| `cmd/tapbridge` | Entry point and wiring |
| `internal/reader` | PC/SC polling and card I/O (`errors.go` holds the user-facing error messages) |
| `internal/server` | WebSocket protocol |
| `internal/ndef` | NDEF encode/decode |
| `internal/config` | Settings and the origin allowlist |
| `internal/tray`, `autostart`, `logging` | Desktop integration |

## Before opening a pull request

```bash
make check
```

This runs `gofmt`, `go vet` (for your OS, the headless build, and Windows),
and the tests with the race detector. CI runs the same checks on Linux,
Windows and macOS, and builds and smoke-tests the Docker image. If you have
Docker, `make docker` builds the image locally.

## Guidelines

- **One logical change per PR.**
- **Add tests** for new logic. Hardware-dependent code should keep its
  decisions in small pure functions (like `PickReader` and `cardChanged`) so
  they can be tested.
- **User-facing errors must be readable.** Say what happened and what to do,
  in plain English. Add new cases to `reader.Explain` with a stable code and
  document the code in [docs/protocol.md](docs/protocol.md#error-codes).
- **The protocol only grows.** Don't rename or remove message types, fields
  or error codes.
- **Update docs and [CHANGELOG.md](CHANGELOG.md)** for user-visible changes.
- **Security-sensitive changes** (origin checks, bind address, anything that
  widens the network surface) must be called out in the PR. See
  [SECURITY.md](SECURITY.md).

## Reader compatibility reports

Tested TapBridge with a reader or tag not listed in
[docs/hardware.md](docs/hardware.md)? Open an issue or PR with the model, OS,
and what worked (UID / NDEF read / write).

## Code of conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md).
