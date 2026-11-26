# Troubleshooting

First step for any problem: open the tray menu and choose **Open Log File**.
Every error is logged in plain English along with the low-level detail.

- [Windows](#windows)
- [macOS](#macos)
- [Browser / web app](#browser--web-app)
- [Cards and tags](#cards-and-tags)
- [Reporting a bug](#reporting-a-bug)

## Windows

### I can't see TapBridge after starting it

It runs in the notification area, not as a window. Click the **^** arrow
next to the clock. To keep it visible, drag the icon onto the taskbar.

### Stuck on "Waiting for reader..."

1. Install the reader's driver from the manufacturer (for the ACR122U, the
   ACS **PC/SC Driver** package), then unplug and replug the reader.
2. Open `services.msc` and check that **Smart Card** (`SCardSvr`) isn't
   *Disabled*. Its startup type should be *Manual (Trigger Start)* or *Automatic*.
3. Look in Device Manager under **Smart card readers**. A yellow warning
   icon means the driver isn't installed correctly.

Windows stops the Smart Card service when the last reader is unplugged.
TapBridge detects this and reconnects automatically when you plug the reader
back in.

### My reader shows up twice ("PICC" and "SAM")

Readers such as the ACR1252U and ACR1281U expose a contactless slot (`PICC`)
and a secure-module slot (`SAM`). TapBridge picks the contactless slot
automatically. To choose another, use **Select Reader** in the tray menu.

### Taps are slow, or Windows says "Installing device driver" on every tap

That's the Smart Card Plug and Play service probing each tag for a driver.
TapBridge retries through it, but you can turn it off:

- **Windows Pro/Enterprise:** `gpedit.msc` → *Computer Configuration →
  Administrative Templates → Windows Components → Smart Card →
  Turn on Smart Card Plug and Play service* → **Disabled**.
- **Windows Home:** in an administrator command prompt run
  `reg add "HKLM\SOFTWARE\Policies\Microsoft\Windows\ScPnP" /v EnableScPnP /t REG_DWORD /d 0 /f`,
  then reboot.

### "Another program is using the reader"

Another app has the reader open in exclusive mode, such as vendor tools,
some password managers, or a second copy of TapBridge. Close it and tap again.

### "Port 8765 is already in use"

TapBridge is probably already running (check the tray). If another app owns
the port, start TapBridge with `--port 9000` and connect to `ws://localhost:9000`.

### SmartScreen blocks the download

TapBridge isn't code-signed yet. Choose **More info → Run anyway**, or build
it from source (see [CONTRIBUTING.md](../CONTRIBUTING.md)). You can verify the
download against `SHA256SUMS.txt` on the release page:

```powershell
Get-FileHash .\tapbridge-windows.exe -Algorithm SHA256
```

## macOS

### "tapbridge can't be opened because Apple cannot check it"

Use the one-line installer, or remove the quarantine flag:

```bash
xattr -d com.apple.quarantine ~/Downloads/tapbridge-mac-arm64
```

### Reader not detected

macOS includes PC/SC support. Most CCID readers need no driver, but the
ACR122U works more reliably with ACS's macOS driver. Unplug and replug the
reader after installing it.

## Browser / web app

### The page gets an `origin_not_allowed` error

Your page isn't served from `localhost`. Add its origin, exactly as shown in
the error message:

```bash
tapbridge --allow-origin https://app.example.com
```

See [configuration.md](configuration.md#allowed-origins).

### The WebSocket closes immediately with code 1006

Nothing is listening: TapBridge isn't running, or you're using the wrong
port. Run `curl http://localhost:8765`. If TapBridge is up, it replies
`TapBridge is running.`

### It works in Chrome but not in a Node/.NET/Electron client

Those clients may resolve `localhost` to IPv6 `::1` only. TapBridge listens
on `::1` too, but if IPv6 is disabled on your machine, connect to
`ws://127.0.0.1:8765` instead.

### My HTTPS page can't connect to `ws://localhost`

Modern Chrome, Edge and Firefox allow `ws://localhost` from HTTPS pages,
because loopback counts as a secure context. Some older browsers and embedded
WebViews block it as mixed content. Test with a current browser first.

## Cards and tags

| Message | What to do |
|---|---|
| "The card was moved away too soon" | Hold the card flat and still until the reader beeps |
| "This card type isn't supported" | You're using a MIFARE Classic, DESFire, bank or ID card. These can be **read by UID** only; writing needs NTAG21x / Ultralight tags |
| "This tag is too small for that value" | Use a shorter value or a bigger tag (NTAG215: 496 bytes, NTAG216: 868 bytes) |
| "The tag refused the write: it is locked" | The tag was permanently locked or has a password. Use a fresh tag |

### The ID I get is a hex string, not my text

The tag has no NDEF Text/URI record that TapBridge understands, so it fell
back to the UID. Write the tag with TapBridge (or any NFC app, using a Text
record) and tap again.

## Reporting a bug

[Open an issue](https://github.com/felaris/tapbridge/issues/new/choose) and
include:

- your OS, TapBridge version (shown at the bottom of the tray menu), reader model and tag type
- the relevant lines from `tapbridge.log`
