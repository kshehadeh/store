// Command store keeps files, folders, and text in a secure vault so other
// computers with access to the same vault can pull them.
package main

import (
	"os"

	"github.com/kshehadeh/store/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
