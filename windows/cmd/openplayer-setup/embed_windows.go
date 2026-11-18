//go:build windows

package main

import _ "embed"

//go:embed ui.html
var uiHTML string

// payload.zip and icon.png are produced by scripts/build-installer.sh
// (small placeholders are checked in so the package always compiles).
//
//go:embed payload.zip
var payload []byte

//go:embed icon.png
var iconPNG []byte
