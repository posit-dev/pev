package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/posit-dev/pev/internal/checks"
)

func newLintChecksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint-checks <file...>",
		Short: "Validate one or more YAML check packs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			all, err := checks.Load(checksFS, checksFSRoot, args, nil)
			if err != nil {
				return err
			}
			errs := checks.Lint(all)
			for _, e := range errs {
				fmt.Println(e)
			}
			if len(errs) > 0 {
				return fmt.Errorf("%d lint error(s)", len(errs))
			}
			fmt.Printf("OK: %d checks pass lint\n", len(all))
			return nil
		},
	}
}
