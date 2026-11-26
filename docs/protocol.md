# WebSocket protocol

TapBridge listens on `ws://localhost:8765` by default. Every message, in both
directions, is a single JSON object with a `type` field.

- [Connecting](#connecting)
- [Server → client messages](#server--client-messages)
- [Client → server messages](#client--server-messages)
- [Error codes](#error-codes)
- [Examples](#examples)
- [Health check](#health-check)
- [Compatibility promise](#compatibility-promise)

## Connecting

```js
const ws = new WebSocket("ws://localhost:8765");
```

The page's origin must be on the allowlist (by default any `localhost` or
`127.0.0.1` port). See [configuration.md](configuration.md#allowed-origins).

If the origin is **not** allowed, TapBridge sends one `error` message with
code `origin_not_allowed` that tells you exactly which flag to add, then
closes the connection with WebSocket close code `1008` (policy violation).
The rejected page never receives card data.

## Server → client messages

| `type` | Fields | Sent when |
|---|---|---|
| `ready` | — | Right after a client connects successfully |
| `card` | `id` | A card is tapped (to **every** connected client) |
| `write_ok` | `id` | Your write request completed (only to the requesting client) |
| `write_error` | `code`, `message` | Your write request failed (only to the requesting client) |
| `error` | `code`, `message` | A request couldn't be understood, or the origin was rejected |

### The `id` in `card` messages

TapBridge reads the tag's NDEF data and picks the ID like this:

1. **Text record** → the text, trimmed.
2. **URI record** containing `/verify/<id>` → the `<id>` segment
   (e.g. `https://example.com/verify/abc123` → `abc123`).
3. Otherwise → the card's **UID** as lowercase hex (e.g. `04a1b2c3d4e5f6`).

Any card that responds to the reader works for UID reads — including
MIFARE Classic, DESFire, bank and ID cards. NDEF reading/writing needs an
NFC Forum Type 2 tag (NTAG21x / MIFARE Ultralight). See [hardware.md](hardware.md).

## Client → server messages

### `write`

| Field | Required | Description |
|---|---|---|
| `type` | yes | `"write"` |
| `id` | yes | The value to write |
| `recordType` | no | `"text"` (default) or `"uri"` |

The **next card tapped** is written. Only one write can be pending at a time.
If the page that asked for a write disconnects before a card is tapped, the
write is cancelled. It is never written onto someone else's card later.

## Error codes

`message` is a plain-English sentence meant to be shown to users, and it
says what to do next. `code` is stable, so use it for program logic.

| `code` | Meaning |
|---|---|
| `origin_not_allowed` | The page's origin isn't on the allowlist |
| `invalid_request` | The message wasn't valid JSON, had an unknown `type`, or a write had no `id` |
| `write_pending` | Another write is already waiting for a card |
| `value_too_long` | The value can't fit in a single NDEF record (about 240 bytes max) |
| `tag_too_small` | The value is valid but larger than this tag's memory |
| `card_removed` | The card was moved away mid-operation |
| `unsupported_card` | The card isn't an NTAG/Ultralight tag (e.g. MIFARE Classic) |
| `read_only` | The tag is locked or password-protected |
| `reader_busy` | Another program has the reader open |
| `reader_unavailable` | The reader was unplugged |
| `service_unavailable` | The OS smart-card service isn't running |
| `not_supported` | Writing is not available in this build |
| `unknown` | Anything else. The message includes the low-level detail |

## Examples

### Listen for taps

```js
const ws = new WebSocket("ws://localhost:8765");

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  switch (msg.type) {
    case "ready":
      console.log("Connected to TapBridge");
      break;
    case "card":
      console.log("Card scanned:", msg.id);
      break;
    case "error":
      alert(msg.message); // e.g. how to allow this origin
      break;
  }
};

ws.onclose = (event) => {
  if (event.code === 1006) {
    console.warn("TapBridge isn't running (or the port is wrong).");
  }
};
```

### Write a card

```js
ws.send(JSON.stringify({ type: "write", id: "abc123" }));
// or, matching the /verify/<id> read convention:
ws.send(JSON.stringify({
  type: "write",
  id: "https://example.com/verify/abc123",
  recordType: "uri",
}));

ws.addEventListener("message", (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === "write_ok") console.log("Written:", msg.id);
  if (msg.type === "write_error") console.error(msg.code, msg.message);
});
```

A complete working page is in [`examples/browser/index.html`](../examples/browser/index.html).

### Reconnect automatically

```js
function connect() {
  const ws = new WebSocket("ws://localhost:8765");
  ws.onmessage = (e) => handle(JSON.parse(e.data));
  ws.onclose = () => setTimeout(connect, 2000);
}
connect();
```

## Health check

A plain HTTP `GET` to the same address returns `200 OK` with the text
`TapBridge is running.`, which is useful for checking whether TapBridge is up:

```bash
curl http://localhost:8765
```

## Compatibility promise

Within a major version, TapBridge only **adds** to this protocol: new
message types, new optional fields, new error codes. Clients should ignore
message types and fields they don't recognise.
