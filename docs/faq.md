# FAQ

## Why not just use Web NFC?

[Web NFC](https://developer.mozilla.org/en-US/docs/Web/API/Web_NFC_API) only
works in **Chrome on Android**, using the phone's built-in NFC. It can't use a
USB reader, and it doesn't exist on desktop browsers or iOS. TapBridge is for
desktops and laptops with a USB reader, in **any** browser.

## Why not Web Serial or WebUSB?

Both let a page talk to USB hardware directly, without a helper app. Neither
works with the readers TapBridge targets:

| | Web Serial | WebUSB | TapBridge |
|---|---|---|---|
| Browsers | Chrome, Edge, Opera (desktop) | Chrome, Edge, Opera (desktop) | **All** (Chrome, Edge, Firefox, Safari, Electron, …) |
| Talks to | Serial ports (USB-to-UART chips like CH340, FTDI, CP210x) | Raw USB devices whose interface class the browser doesn't block | Any **PC/SC** reader the OS supports |
| ACR122U and other CCID readers | ❌ They aren't serial devices | ❌ Chrome blocks the smart-card (CCID) class, and the OS driver already owns the device | ✅ |
| Install needed | None | None (Windows may need a WinUSB driver swap) | Small tray app |
| User prompt | Port picker every new session | Device picker | None |

- **Web Serial** suits **serial NFC modules**, such as a PN532 breakout on a
  USB-to-serial adapter, or a 125 kHz RDM6300. You'd send the PN532
  command frames from JavaScript yourself. It's a valid approach, but for
  different hardware, in Chromium only.
- **WebUSB** can't open PC/SC readers. Chrome treats the smart-card class as
  protected, and on Windows and macOS the OS's own driver has already claimed
  the reader anyway.
- **Web Smart Card API.** Chrome has a smart-card API that would talk to
  PC/SC readers directly. At the time of writing it is only available to
  Isolated Web Apps on ChromeOS, not to regular websites.

So for USB PC/SC readers, a small local bridge like TapBridge is currently
the only approach that works in every browser.

> Many cheap readers sold as "USB NFC readers" are **keyboard wedge** devices:
> they type the card's UID as if it were typed on a keyboard. Those need no
> API or bridge at all, just a focused `<input>`. They can't read NDEF or
> write, though.

## Is it safe to run a local WebSocket server?

By default TapBridge only listens on this computer and only accepts pages from
`localhost`. A random website you visit can't read your taps. See
[SECURITY.md](../SECURITY.md) for the full threat model.

## Can several tabs or apps connect at once?

Yes. Every connected client gets every `card` message. Write results go only
to the client that requested the write.

## Can I use it from Node.js, Python, or a native app?

Yes. Any WebSocket client works. Non-browser clients send no `Origin` header,
so they're always allowed (which is why TapBridge listens on loopback only).

```bash
npx wscat -c ws://localhost:8765
```

## Does it work on Linux?

Yes. The easiest way is the Docker image (amd64 and arm64, including
Raspberry Pi), see [docker.md](docker.md). You can also build from source.
It needs `pcscd`, plus GTK and AppIndicator for the tray, or use
`--headless` / `-tags notray` to skip the tray entirely.

## Can I write raw data, or to MIFARE Classic?

Not currently. TapBridge writes a single NDEF Text or URI record to
NTAG/Ultralight tags. See [hardware.md](hardware.md).
