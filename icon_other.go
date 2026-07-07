//go:build !windows

package main

import _ "embed"

// macOS and Linux system trays accept PNG icon bytes directly.
//
//go:embed assets/icon.png
var iconData []byte
