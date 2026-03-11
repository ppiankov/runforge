package cli

import (
	"fmt"

	"github.com/ppiankov/tokencontrol/internal/telemetry"
	"github.com/spf13/cobra"
)

func newBenchCmd() *cobra.Command {
	var (
		runner     string
		difficulty string
		since      string
		minTasks   int
	)

	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Show efficiency benchmarks",
		Long:  "Analyze cost-per-success, cascade effectiveness, false positive rates, and token efficiency from telemetry data.",
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

			sinceFilter := ""
			if since != "" {
				sinceFilter = since + "T00:00:00Z"
			}

			sinceStr := summary.Since.Format("2006-01-02")
			fmt.Printf("Efficiency Report (%d tasks, %d runs, since %s)\n", summary.TotalTasks, summary.TotalRuns, sinceStr)

			// Cost per success
			cps, err := telemetry.QueryCostPerSuccess(db, sinceFilter, minTasks)
			if err != nil {
				return err
			}
			filtered := filterCPS(cps, runner, difficulty)
			if len(filtered) > 0 {
				fmt.Println("\nCost per success by runner:")
				fmt.Printf("  %-12s %-20s %-9s %5s %4s %7s %7s %5s %5s\n",
					"RUNNER", "MODEL", "DIFF", "TASKS", "OK", "$/TASK", "COST", "OK%", "FP%")
				for _, s := range filtered {
					costPerTask := "$0.00"
					if s.CostPerTask > 0 {
						costPerTask = fmt.Sprintf("$%.2f", s.CostPerTask)
					}
					fmt.Printf("  %-12s %-20s %-9s %5d %4d %7s $%6.2f %4.0f%% %4.1f%%\n",
						s.Runner, truncModel(s.Model), s.Difficulty,
						s.Tasks, s.Completed, costPerTask, s.CostUSD,
						s.SuccessRate, s.FPRate)
				}
			}

			// Cascade effectiveness
			cascade, err := telemetry.QueryCascadeEffectiveness(db, sinceFilter)
			if err != nil {
				return err
			}
			if len(cascade) > 0 {
				fmt.Println("\nCascade effectiveness:")
				fmt.Printf("  %-6s %5s %9s %8s %7s\n", "STEP", "TASKS", "COMPLETED", "COST", "RESCUE")
				for _, s := range cascade {
					rescue := "—"
					if s.CascadeStep > 1 {
						rescue = fmt.Sprintf("%.0f%%", s.RescueRate)
					}
					fmt.Printf("  %-6d %5d %9d $%7.2f %7s\n",
						s.CascadeStep, s.Tasks, s.Completed, s.CostUSD, rescue)
				}
			}

			// False positive waste
			fps, err := telemetry.QueryFalsePositiveAnalysis(db, sinceFilter, minTasks)
			if err != nil {
				return err
			}
			fpFiltered := filterFP(fps, runner)
			if len(fpFiltered) > 0 {
				hasFPs := false
				for _, s := range fpFiltered {
					if s.FPs > 0 {
						hasFPs = true
						break
					}
				}
				if hasFPs {
					fmt.Println("\nFalse positive waste:")
					for _, s := range fpFiltered {
						if s.FPs == 0 {
							continue
						}
						label := s.Runner
						if s.Model != "" {
							label += ":" + truncModel(s.Model)
						}
						fmt.Printf("  %-30s %d FPs / %d tasks (%.1f%%)  $%.2f wasted\n",
							label, s.FPs, s.Tasks, s.FPRate, s.FPCost)
					}
				}
			}

			// Token efficiency
			teff, err := telemetry.QueryTokenEfficiency(db, sinceFilter, minTasks)
			if err != nil {
				return err
			}
			tFiltered := filterTE(teff, runner)
			if len(tFiltered) > 0 {
				fmt.Println("\nToken efficiency:")
				fmt.Printf("  %-12s %-20s %7s %7s %6s %8s %7s\n",
					"RUNNER", "MODEL", "AVG IN", "AVG OUT", "RATIO", "$/1K TOK", "REPORT")
				for _, s := range tFiltered {
					fmt.Printf("  %-12s %-20s %6.1fK %6.1fK %5.0f%% %8.4f %6.0f%%\n",
						s.Runner, truncModel(s.Model),
						float64(s.AvgInput)/1000, float64(s.AvgOutput)/1000,
						s.InputRatio, s.CostPer1K, s.ReportRate)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&runner, "runner", "", "filter by runner name")
	cmd.Flags().StringVar(&difficulty, "difficulty", "", "filter by difficulty (simple, medium, complex)")
	cmd.Flags().StringVar(&since, "since", "", "filter since date (YYYY-MM-DD)")
	cmd.Flags().IntVar(&minTasks, "min-tasks", 5, "minimum tasks per bucket")

	return cmd
}

func truncModel(model string) string {
	if len(model) > 20 {
		return model[:17] + "..."
	}
	return model
}

func filterCPS(data []telemetry.CostPerSuccess, runner, difficulty string) []telemetry.CostPerSuccess {
	if runner == "" && difficulty == "" {
		return data
	}
	var out []telemetry.CostPerSuccess
	for _, s := range data {
		if runner != "" && s.Runner != runner {
			continue
		}
		if difficulty != "" && s.Difficulty != difficulty {
			continue
		}
		out = append(out, s)
	}
	return out
}

func filterFP(data []telemetry.FPAnalysis, runner string) []telemetry.FPAnalysis {
	if runner == "" {
		return data
	}
	var out []telemetry.FPAnalysis
	for _, s := range data {
		if s.Runner == runner {
			out = append(out, s)
		}
	}
	return out
}

func filterTE(data []telemetry.TokenEfficiency, runner string) []telemetry.TokenEfficiency {
	if runner == "" {
		return data
	}
	var out []telemetry.TokenEfficiency
	for _, s := range data {
		if s.Runner == runner {
			out = append(out, s)
		}
	}
	return out
}
