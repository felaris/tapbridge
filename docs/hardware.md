# Supported hardware

## Readers

TapBridge talks to readers through **PC/SC**, the operating system's standard
smart-card API (WinSCard on Windows, CryptoTokenKit/pcsc-lite on macOS and
Linux). Any USB contactless reader that supports the standard PC/SC 2.0
pseudo-APDUs should work:

| Command | APDU | Used for |
|---|---|---|
| Get UID | `FF CA 00 00 00` | Every tap |
| Read binary | `FF B0 00 <page> 10` | Reading NDEF |
| Update binary | `FF D6 00 <page> 04 <4 bytes>` | Writing NDEF |

| Reader | Status |
|---|---|
| ACS ACR122U | Tested |
| ACS ACR1252U / ACR1281U | Should work. TapBridge auto-selects the `PICC` slot over `SAM` |
| Identiv uTrust 3700 F, HID Omnikey 5022/5427 | Should work (standard PC/SC) |
| Serial/UART modules (PN532 breakouts, RDM6300) | Not supported: they are not PC/SC devices |
| "Keyboard wedge" USB readers | Not needed: they type the UID like a keyboard, no bridge required |

Tried another reader? Please open an issue or PR to update this table.

## Cards and tags

| Card | Read UID | Read NDEF | Write NDEF |
|---|:---:|:---:|:---:|
| NTAG213 / 215 / 216 | ✅ | ✅ | ✅ |
| MIFARE Ultralight / Ultralight C / EV1 | ✅ | ✅ | ✅ |
| MIFARE Classic 1K / 4K | ✅ | ❌ | ❌ |
| MIFARE DESFire, ISO-DEP (Type 4) tags | ✅ | ❌ | ❌ |
| Bank cards, phones, ID cards | ✅ (often randomised) | ❌ | ❌ |

Writes start at page 4 and are checked against the tag's capability
container, so TapBridge won't write past the data area and damage the lock
or config pages.

> Only read and write cards you own. Copying access-control or payment cards
> is illegal in many places.
