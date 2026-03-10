package cli

import (
	"fmt"
	"os"

	"github.com/ppiankov/tokencontrol/internal/telemetry"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	var (
		format string
		runner string
		since  string
		until  string
		output string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export telemetry data",
		Long:  "Export task execution telemetry as CSV or JSON.",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := telemetry.OpenDB(telemetry.DefaultPath())
			if err != nil {
				return fmt.Errorf("open telemetry: %w", err)
			}
			defer func() { _ = db.Close() }()

			sinceFilter := ""
			if since != "" {
				sinceFilter = since + "T00:00:00Z"
			}
			untilFilter := ""
			if until != "" {
				untilFilter = until + "T23:59:59Z"
			}

			data, err := telemetry.QueryExport(db, runner, sinceFilter, untilFilter)
			if err != nil {
				return err
			}

			if len(data) == 0 {
				fmt.Fprintln(os.Stderr, "No data matching filters.")
				return nil
			}

			w := os.Stdout
			if output != "" {
				f, err := os.Create(output)
				if err != nil {
					return fmt.Errorf("create output file: %w", err)
				}
				defer func() { _ = f.Close() }()
				w = f
			}

			switch format {
			case "csv":
				return telemetry.ExportCSV(w, data)
			case "json":
				return telemetry.ExportJSON(w, data)
			default:
				return fmt.Errorf("unsupported format: %s (use csv or json)", format)
			}
		},
	}

	cmd.Flags().StringVar(&format, "format", "csv", "output format (csv, json)")
	cmd.Flags().StringVar(&runner, "runner", "", "filter by runner name")
	cmd.Flags().StringVar(&since, "since", "", "filter since date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&until, "until", "", "filter until date (YYYY-MM-DD)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default: stdout)")

	return cmd
}
