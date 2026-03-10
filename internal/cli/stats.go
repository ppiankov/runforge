package cli

import (
	"fmt"

	"github.com/ppiankov/tokencontrol/internal/telemetry"
	"github.com/spf13/cobra"
)

func newStatsCmd() *cobra.Command {
	var (
		runner string
		since  string
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show telemetry statistics",
		Long:  "Display aggregated cost, token, and success rate data from local telemetry.",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := telemetry.OpenDB(telemetry.DefaultPath())
			if err != nil {
				return fmt.Errorf("open telemetry: %w", err)
			}
			defer func() { _ = db.Close() }()

			summary, err := telemetry.QuerySummary(db)
			if err != nil {
				return err
			}
			if summary.TotalTasks == 0 {
				fmt.Println("No telemetry data yet. Run some tasks first.")
				return nil
			}

			sinceStr := summary.Since.Format("2006-01-02")
			fmt.Printf("Telemetry: %d tasks across %d runs (since %s)\n\n", summary.TotalTasks, summary.TotalRuns, sinceStr)

			// Runner breakdown
			sinceFilter := ""
			if since != "" {
				sinceFilter = since + "T00:00:00Z"
			}

			stats, err := telemetry.QueryRunnerStats(db, sinceFilter)
			if err != nil {
				return err
			}

			fmt.Println("Runner breakdown:")
			for _, s := range stats {
				if runner != "" && s.Runner != runner {
					continue
				}
				avgMin := s.AvgDuration.Minutes()
				fmt.Printf("  %-12s %4d tasks  $%6.2f   %3.0f%% success  avg %.1fm\n",
					s.Runner, s.Tasks, s.CostUSD, s.SuccessRate, avgMin)
			}

			// Cost by period
			periods, err := telemetry.QueryCostByPeriod(db)
			if err != nil {
				return err
			}
			fmt.Println("\nCost by period:")
			for _, p := range periods {
				fmt.Printf("  %-14s $%6.2f  (%d tasks)\n", p.Label+":", p.Cost, p.Tasks)
			}

			// Top models
			models, err := telemetry.QueryTopModels(db, sinceFilter, 10)
			if err != nil {
				return err
			}
			if len(models) > 0 {
				fmt.Println("\nTop models by cost:")
				for _, m := range models {
					fmt.Printf("  %-20s $%6.2f (%d tasks)\n", m.Model+":", m.Cost, m.Tasks)
				}
			}

			_ = asJSON // reserved for future JSON output mode
			return nil
		},
	}

	cmd.Flags().StringVar(&runner, "runner", "", "filter by runner name")
	cmd.Flags().StringVar(&since, "since", "", "filter since date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")

	return cmd
}
