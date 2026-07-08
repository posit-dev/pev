package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newFixCmd() *cobra.Command {
	c := &cobra.Command{
		Use:    "fix",
		Short:  "(v2) Apply targeted remediation for a specific check",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("`pev fix` is reserved for v2 — not yet implemented.")
			fmt.Println("In v1, pev is read-only by design. See README.md for the v2 design surface.")
			return nil
		},
	}
	c.Flags().String("check", "", "check ID to remediate")
	c.Flags().Bool("dry-run", false, "show planned changes without applying them")
	return c
}
