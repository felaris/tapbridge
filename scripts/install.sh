#!/usr/bin/env bash
# TapBridge installer for macOS.
#
#   curl -fsSL https://github.com/felaris/tapbridge/releases/latest/download/install.sh | bash
#
# Downloads the latest release for this Mac into ~/.local/bin and clears the
# Gatekeeper quarantine flag (TapBridge is not notarised yet).
set -euo pipefail

REPO="felaris/tapbridge"
DEST_DIR="${TAPBRIDGE_INSTALL_DIR:-$HOME/.local/bin}"
DEST="$DEST_DIR/tapbridge"

fail() { echo "Error: $*" >&2; exit 1; }

[ "$(uname -s)" = "Darwin" ] || fail "this installer is for macOS. On Windows, download tapbridge-windows.exe from https://github.com/$REPO/releases/latest"

case "$(uname -m)" in
  arm64)  FILE="tapbridge-mac-arm64" ;;
  x86_64) FILE="tapbridge-mac-intel" ;;
  *)      fail "unsupported CPU architecture: $(uname -m)" ;;
esac

URL="https://github.com/$REPO/releases/latest/download/$FILE"

mkdir -p "$DEST_DIR"
echo "Downloading $FILE..."
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
curl -fSL --progress-bar "$URL" -o "$TMP" || fail "download failed. Check your internet connection, or download manually from https://github.com/$REPO/releases/latest"
chmod +x "$TMP"
mv "$TMP" "$DEST"
trap - EXIT

# Remove the quarantine flag so Gatekeeper doesn't block the unsigned binary.
xattr -d com.apple.quarantine "$DEST" 2>/dev/null || true

echo "Installed: $DEST ($("$DEST" --version 2>/dev/null || echo "version unknown"))"

case ":$PATH:" in
  *":$DEST_DIR:"*) echo "Run it with: tapbridge" ;;
  *)
    echo ""
    echo "$DEST_DIR is not on your PATH. Add this line to ~/.zshrc (or ~/.bash_profile):"
    echo "  export PATH=\"$DEST_DIR:\$PATH\""
    echo "Or run it directly: $DEST"
    ;;
esac
