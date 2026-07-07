//go:build windows

package main

import _ "embed"

// Windows' system tray loads the icon via LoadImageW, which requires a real
// .ico file — a PNG will not load. See scripts/gen_icon.go for how this is
// generated from assets/icon.png.
//
//go:embed assets/icon.ico
var iconData []byte
