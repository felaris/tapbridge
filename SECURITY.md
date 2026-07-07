# Security Policy

## Reporting a vulnerability

If you discover a security vulnerability in TapBridge, please report it privately
so it can be fixed before public disclosure.

- **Preferred:** open a [private security advisory](https://github.com/felaris/tapbridge/security/advisories/new)
  on GitHub.
- **Alternatively:** email the maintainer at **felixawortwe14@gmail.com** with the
  subject line `TapBridge Security`.

Please include:

- A description of the issue and its impact.
- Steps to reproduce (a proof of concept if possible).
- The TapBridge version and your operating system.

You can expect an initial response within a few days. Please give a reasonable
amount of time for a fix to be released before disclosing publicly.

## Supported versions

TapBridge is distributed as rolling releases; only the **latest release** receives
security fixes. Always update to the newest version before reporting.

## Security model & considerations

TapBridge runs a local WebSocket server (default `ws://localhost:8765`) that reads
data from NFC cards and can write data to them. Keep the following in mind:

- **Origin allowlist.** By default, only `localhost` / `127.0.0.1` origins may
  connect. Any other web origin must be explicitly allowlisted via `--allow-origin`
  or `TAPBRIDGE_ALLOWED_ORIGINS`. Do **not** allowlist origins you do not control.
- **Origin headers are browser-enforced, not universal.** The `Origin` check
  protects against malicious *websites* in a browser, but a native/local process
  can set any header it wants. Treat the WebSocket as accessible to any local
  program running as your user.
- **No transport encryption.** The bridge listens on plain `ws://` on the
  loopback interface. Do not expose the port beyond `localhost` (e.g. via a
  reverse proxy or port forward) without adding TLS and authentication.
- **Card data is not validated.** IDs read from cards are passed through as-is.
  Treat anything read from an NFC tag as untrusted input in your web app.

If any of these defaults don't fit your deployment, please open an issue to
discuss hardening options (e.g. shared-token authentication).
