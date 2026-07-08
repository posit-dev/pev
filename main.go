// pev — Posit Environment Validator. See README.md for product overview.
package main

import (
	"os"

	"github.com/posit-dev/pev/cmd"
)

// Linker overrides via -ldflags at release time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := cmd.Execute(version, commit, date, embeddedChecks, embeddedRoot); err != nil {
		os.Exit(1)
	}
}
