package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ppiankov/tokencontrol/internal/runner"
)

func newQuotaCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "quota",
		Short: "Check provider API quotas and balances",
		Long: `Query LLM provider APIs for remaining quota, usage, and balances.

Checks all providers that have API keys set in environment variables:
  OPENAI_API_KEY      → OpenAI usage (7-day lookback, burn rate)
  ANTHROPIC_API_KEY   → Anthropic usage (7-day lookback, burn rate)
  DEEPSEEK_API_KEY    → DeepSeek credit balance

Providers without API keys are skipped. Providers without quota
endpoints (Gemini, Groq) are not checked.

Note: OpenAI and Anthropic usage endpoints require admin API keys.
Regular API keys will show "admin API key required" but are non-fatal.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			results := runner.CheckAllQuotas(cmd.Context(), os.Getenv)

			if jsonOutput {
				data, err := json.MarshalIndent(results, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			if len(results) == 0 {
				fmt.Println("No provider API keys found. Set OPENAI_API_KEY, ANTHROPIC_API_KEY, or DEEPSEEK_API_KEY.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "PROVIDER\tSTATUS\tUSED (7d)\tBURN RATE\tBALANCE\n")
			for _, info := range results {
				status := "available"
				if !info.Available {
					status = "unavailable"
				}
				if info.Error != "" && info.UsedTokens == 0 && info.Balance == "" {
					status = info.Error
				}

				used := "—"
				if info.UsedTokens > 0 {
					used = formatTokenCount(info.UsedTokens)
				}

				burn := "—"
				if info.BurnRatePerDay > 0 {
					burn = formatTokenCount(info.BurnRatePerDay) + "/day"
				}

				balance := "—"
				if info.Balance != "" {
					balance = "$" + info.Balance
					if info.Currency != "" && info.Currency != "USD" {
						balance = info.Balance + " " + info.Currency
					}
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", info.Provider, status, used, burn, balance)
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output in JSON format")

	return cmd
}
