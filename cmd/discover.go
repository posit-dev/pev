package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/posit-dev/pev/internal/discover"
)

func newDiscoverCmd() *cobra.Command {
	var (
		format     string
		outputFile string
	)
	c := &cobra.Command{
		Use:   "discover",
		Short: "Run discovery probes only and print what pev would assume",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			facts := discover.Gather(ctx)

			var buf bytes.Buffer
			switch format {
			case "json":
				data, err := json.MarshalIndent(facts, "", "  ")
				if err != nil {
					return err
				}
				buf.Write(data)
				buf.WriteByte('\n')
			default:
				renderFactsHuman(&buf, facts)
			}

			// Always print to stdout. If --output-file is set, also write
			// the same bytes to disk so SEs can capture the snapshot before
			// running assess.
			if _, err := io.Copy(os.Stdout, bytes.NewReader(buf.Bytes())); err != nil {
				return err
			}
			if outputFile != "" {
				if err := os.WriteFile(outputFile, buf.Bytes(), 0o600); err != nil {
					return fmt.Errorf("write %s: %w", outputFile, err)
				}
				fmt.Fprintf(os.Stderr, "wrote %s\n", outputFile)
			}
			return nil
		},
	}
	c.Flags().StringVar(&format, "format", "human", "human|json")
	c.Flags().StringVar(&outputFile, "output-file", "", "also write the snapshot to this path")
	return c
}

func renderFactsHuman(w io.Writer, f discover.HostFacts) {
	fmt.Fprintln(w, "Discovered host facts:")
	fmt.Fprintf(w, "  hostname            : %s\n", f.Hostname)
	if f.FQDN != "" && f.FQDN != f.Hostname {
		fmt.Fprintf(w, "  fqdn                : %s\n", f.FQDN)
	}
	fmt.Fprintf(w, "  os                  : %s (%s)\n", f.OSPretty, f.OS)
	fmt.Fprintf(w, "  os_family           : %s\n", f.OSFamily)
	fmt.Fprintf(w, "  arch                : %s\n", f.Arch)
	fmt.Fprintf(w, "  cpus                : %d\n", f.CPUs)
	fmt.Fprintf(w, "  mem_mb              : %d\n", f.MemMB)
	fmt.Fprintf(w, "  disk_gb_free[/]     : %d\n", f.DiskGB["/"])
	fmt.Fprintf(w, "  running as root     : %v\n", f.Root)
	fmt.Fprintf(w, "  workbench installed : %v\n", f.Products.Workbench)
	fmt.Fprintf(w, "  connect installed   : %v\n", f.Products.Connect)
	fmt.Fprintf(w, "  ppm installed       : %v\n", f.Products.PackageManager)
	for _, p := range f.R {
		fmt.Fprintf(w, "  R                   : %s\n", p)
	}
	for _, p := range f.Python {
		fmt.Fprintf(w, "  python              : %s\n", p)
	}
	for _, p := range f.Quarto {
		fmt.Fprintf(w, "  quarto              : %s\n", p)
	}
}
