//go:build !windows

package assets

import _ "embed"

// macOS and Linux system trays accept PNG icon bytes directly.
//
//go:embed icon.png
var Icon []byte
