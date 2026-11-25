# Configuration

Settings are resolved in this order, later winning:

**built-in defaults → config file → environment variable → command-line flag**

| Setting | Flag | Environment variable | Default |
|---|---|---|---|
| Bind address | `--host` | `TAPBRIDGE_HOST` | `127.0.0.1` (this computer only) |
| WebSocket port | `--port` | `TAPBRIDGE_PORT` | `8765` |
| Allowed origins | `--allow-origin` (comma-separated) | `TAPBRIDGE_ALLOWED_ORIGINS` (comma-separated) | `http://localhost`, `https://localhost`, `http://127.0.0.1`, `https://127.0.0.1` |
| Print version | `--version` | — | — |

```bash
tapbridge --port 9000 --allow-origin https://app.example.com,https://staging.example.com
```

On Windows, flags can be added to a shortcut's **Target** field, e.g.
`"C:\Tools\tapbridge-windows.exe" --allow-origin https://app.example.com`.

## Allowed origins

Browsers send an `Origin` header with every WebSocket connection. TapBridge
only accepts pages whose origin is on the allowlist, so a random website
you visit can't read your card taps.

- An entry **without a port** matches any port: `http://localhost` allows
  `http://localhost:3000`, `http://localhost:5173`, and so on.
- An entry **with a port** matches only that port.
- The scheme must match: `https://app.example.com` does not allow
  `http://app.example.com`.
- Hosts must match exactly: `https://example.com` does not allow
  `https://sub.example.com`.
- Non-browser clients (scripts, native apps) send no `Origin` and are always
  allowed. Binding to loopback is what keeps those local (see below).

If your page is rejected, TapBridge's error message tells you the exact
`--allow-origin` value to add, and the same hint is written to the log.

## Bind address

TapBridge binds to `127.0.0.1`, so nothing else on your network can
connect. When bound to `127.0.0.1` it also listens on IPv6 loopback `[::1]`,
because on Windows `localhost` resolves to `::1` first.

Only use `--host 0.0.0.0` (or a LAN IP) if you really need other machines
to connect. Any device that can reach the port can then read scanned IDs
and request writes. See [SECURITY.md](../SECURITY.md).

## Config file

Choices made in the tray menu (selected reader, Start at Login) are saved to:

| OS | Path |
|---|---|
| Windows | `%AppData%\tapbridge\config.json` |
| macOS | `~/Library/Application Support/tapbridge/config.json` |
| Linux | `$XDG_CONFIG_HOME/tapbridge/config.json` (usually `~/.config/…`) |

You can also set `host`, `port` and `allowedOrigins` there:

```json
{
  "port": "8765",
  "allowedOrigins": ["http://localhost", "https://app.example.com"],
  "selectedReader": "ACS ACR122 0",
  "autoStart": true
}
```

## Log file

`tapbridge.log` is written next to `config.json`. Open it from the tray menu
with **Open Log File**. When it grows past 1 MB, it is renamed to
`tapbridge.log.old` on the next start.
