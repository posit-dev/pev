package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print pev version, commit, and build date",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("pev %s (commit %s, built %s)\n", buildVersion, buildCommit, buildDate)
			return nil
		},
	}
}
