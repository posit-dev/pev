package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/posit-dev/pev/internal/report"
)

func newDiffCmd() *cobra.Command {
	var (
		format      string
		onlyChanges bool
	)
	c := &cobra.Command{
		Use:   "diff <baseline.json> <current.json>",
		Short: "Compare two report JSON sidecars; exit 1 on regressions",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := report.ReadJSON(args[0])
			if err != nil {
				return err
			}
			b, err := report.ReadJSON(args[1])
			if err != nil {
				return err
			}
			d, err := report.Compute(a, b)
			if err != nil {
				return err
			}
			_ = onlyChanges // v0.3 honors this; for now both formats include all sections.
			switch format {
			case "json":
				out, err := report.RenderDiffJSON(d)
				if err != nil {
					return err
				}
				fmt.Println(out)
			default:
				fmt.Println(report.RenderDiffMarkdown(d))
			}
			if d.HasRegressions() {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().StringVar(&format, "format", "markdown", "markdown|json")
	c.Flags().BoolVar(&onlyChanges, "only-changes", false, "suppress unchanged sections (v0.3+)")
	return c
}
