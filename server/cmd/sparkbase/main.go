// Command sparkbase is the command line client for a Sparkbase install.
package main

import (
	"fmt"
	"os"

	"github.com/shellhaki/sparkbase/server/internal/cli"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("sparkbase", version)
		return
	}
	if err := cli.New().Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
