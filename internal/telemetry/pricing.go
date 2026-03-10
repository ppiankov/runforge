package telemetry

// Pricing per 1M tokens (input, output) in USD.
var modelPricing = map[string][2]float64{
	"gpt-4.1":           {2.00, 8.00},
	"gpt-4.1-mini":      {0.40, 1.60},
	"gpt-4.1-nano":      {0.10, 0.40},
	"o3":                {2.00, 8.00},
	"o4-mini":           {1.10, 4.40},
	"claude-sonnet-4-6": {3.00, 15.00},
	"claude-opus-4-6":   {15.00, 75.00},
	"claude-haiku-4-5":  {0.80, 4.00},
	"gemini-2.5-pro":    {1.25, 10.00},
	"gemini-2.5-flash":  {0.15, 0.60},
	"deepseek-chat":     {0.14, 0.28},
	"qwen-coder-plus":   {0.50, 2.00},
}

// EstimateCost returns estimated cost in USD for the given model and token counts.
// Returns 0 for unknown models.
func EstimateCost(model string, inputTokens, outputTokens int) float64 {
	prices, ok := modelPricing[model]
	if !ok {
		return 0
	}
	return (float64(inputTokens)*prices[0] + float64(outputTokens)*prices[1]) / 1_000_000
}
