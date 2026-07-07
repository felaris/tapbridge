# Contributing to TapBridge

Thanks for your interest in improving TapBridge! Contributions of all kinds are
welcome — bug reports, feature ideas, documentation fixes, and code.

## Getting started

TapBridge is a small Go application. You'll need:

- **Go 1.26+**
- **CGO enabled** (the default) — the PC/SC bindings and system tray use cgo on
  macOS and Linux.
- A C toolchain and, on Linux, the PC/SC + GTK development headers:

  ```bash
  # Debian / Ubuntu
  sudo apt-get install -y libpcsclite-dev libgtk-3-dev libayatana-appindicator3-dev
  ```

Build and run:

```bash
git clone https://github.com/felaris/tapbridge.git
cd tapbridge
go build -o tapbridge .
./tapbridge
```

You do **not** need physical NFC hardware to work on most of the code — the NDEF
encoding/parsing and configuration logic are pure functions with unit tests.

## Before you open a pull request

Please make sure the following all pass locally:

```bash
go vet ./...
go test ./...
go build ./...
```

If your change touches Windows-specific code, verify it still cross-compiles:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null .
```

CI runs these same checks on every pull request.

## Guidelines

- **Keep changes focused.** One logical change per pull request makes review easier.
- **Match the existing style.** Run `gofmt` (most editors do this on save).
- **Add tests** for new pure-logic functions (NDEF handling, config, origin checks).
- **Update the README** if you add or change a user-facing flag, message type, or
  behavior.
- **Security-sensitive changes** (the WebSocket origin check, anything touching the
  network surface) should be called out explicitly in the PR description. See
  [SECURITY.md](SECURITY.md).

## Reporting bugs & requesting features

Use the issue templates under **New Issue**. For security vulnerabilities, please
follow [SECURITY.md](SECURITY.md) instead of opening a public issue.

## Code of conduct

Be respectful and constructive. Harassment or abuse of any kind will not be
tolerated.
