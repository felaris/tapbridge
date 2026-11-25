---
name: Bug report
about: Report something that isn't working as expected
title: "[Bug] "
labels: bug
---

**Describe the bug**
A clear and concise description of what went wrong.

**To reproduce**
Steps to reproduce the behavior:
1. ...
2. ...
3. ...

**Expected behavior**
What you expected to happen instead.

**Environment**
- OS and version: (e.g. macOS 14.5 / Windows 11)
- TapBridge version: (shown at the bottom of the tray menu, or `tapbridge --version`)
- NFC reader model: (e.g. ACR122U)
- Card / tag type, if relevant: (e.g. NTAG215)

**Logs**
Open the tray menu → **Open Log File** and paste the relevant lines
(`tapbridge.log` lives in `%AppData%\tapbridge` on Windows and
`~/Library/Application Support/tapbridge` on macOS).

**Error message**
If your web app received a `write_error` or `error` message, paste its
`code` and `message`.

**Additional context**
Anything else that might help — screenshots, the web app's console output, etc.
