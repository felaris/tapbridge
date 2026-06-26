#!/usr/bin/env bash
set -e

REPO="felaris/felaris-nfc-bridge"

# Detect architecture
ARCH=$(uname -m)
if [ "$ARCH" = "arm64" ]; then
  FILE="felaris-nfc-bridge-mac-arm64"
else
  FILE="felaris-nfc-bridge-mac-intel"
fi

# Resolve latest release tag
echo "Fetching latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
  | grep '"tag_name"' | head -1 | cut -d'"' -f4)

if [ -z "$TAG" ]; then
  echo "Error: could not determine latest release. Check your internet connection."
  exit 1
fi

URL="https://github.com/$REPO/releases/download/$TAG/$FILE"
DEST="$HOME/.local/bin/felaris-nfc-bridge"

# Ensure destination directory exists
mkdir -p "$HOME/.local/bin"

echo "Downloading Felaris NFC Bridge $TAG ($ARCH)..."
curl -fsSL "$URL" -o "$DEST"
chmod +x "$DEST"

# Remove macOS quarantine — this is why curl | bash is needed without a code-signing cert
xattr -d com.apple.quarantine "$DEST" 2>/dev/null || true

# Warn if ~/.local/bin is not in PATH
case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *)
    echo ""
    echo "NOTE: Add this to your shell profile (~/.zshrc or ~/.bash_profile):"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
    ;;
esac

echo "Installed → $DEST"
echo "Run it with: felaris-nfc-bridge"
echo "Or double-click it in Finder."
