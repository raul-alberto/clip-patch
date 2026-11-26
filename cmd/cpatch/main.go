// Command cpatch is the short alias for clip-patch.
package main

import (
	"os"

	"clip-patch/internal/cli"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.1.0"

func main() {
	os.Exit(cli.Main(version))
}
