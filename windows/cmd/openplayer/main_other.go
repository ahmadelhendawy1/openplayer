//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "openplayer currently targets Windows only (cross-compile with GOOS=windows)")
	os.Exit(1)
}
