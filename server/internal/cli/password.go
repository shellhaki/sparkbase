package cli

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// readPassword reads a password without echo when stdin is a terminal.
func (c *CLI) readPassword(label string) string {
	if f, ok := c.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(c.Err, label)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(c.Err)
		if err == nil {
			return string(b)
		}
	}
	return c.prompt(label)
}
