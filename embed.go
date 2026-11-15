// Package embedded exposes the repository files compiled into the binary.
//
// go:embed patterns cannot reach parent directories, so the files that live at
// the repository root are embedded here while the application code lives in
// src/.
package embedded

import "embed"

//go:embed frontend/dist
var Frontend embed.FS

//go:embed icon.png
var Icon []byte

//go:embed install.sh
var Installer []byte
