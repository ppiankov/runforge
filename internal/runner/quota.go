package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const quotaHTTPTimeout = 10 * time.Second

// QuotaInfo holds the result of a provider quota check.
type QuotaInfo struct {
	Provider       string `json:"provider"`
	Available      bool   `json:"available"`
	UsedTokens     int    `json:"used_tokens,omitempty"`
	BurnRatePerDay int    `json:"burn_rate_per_day,omitempty"`
	Balance        string `json:"balance,omitempty"`
	Currency       string `json:"currency,omitempty"`
	Error          string `json:"error,omitempty"`
}

// CheckQuota queries a provider's API for remaining budget.
// Returns nil (not error) when the provider has no quota endpoint.
func CheckQuota(ctx context.Context, provider, apiKey string) (*QuotaInfo, error) {
	if apiKey == "" {
		return &QuotaInfo{
			Provider:  provider,
			Available: true,
			Error:     "no API key available",
		}, nil
	}

	switch provider {
	case "openai":
		return checkOpenAIQuota(ctx, apiKey)
	case "anthropic":
		return checkAnthropicQuota(ctx, apiKey)
	case "deepseek":
		return checkDeepSeekQuota(ctx, apiKey)
	default:
		return nil, nil
	}
}

// checkOpenAIQuota queries the OpenAI usage API for 7-day token consumption.
func checkOpenAIQuota(ctx context.Context, apiKey string) (*QuotaInfo, error) {
	now := time.Now()
	startTime := now.AddDate(0, 0, -7)

	query := url.Values{
		"start_time":   {strconv.FormatInt(startTime.Unix(), 10)},
		"bucket_width": {"1d"},
		"limit":        {"100"},
	}

	u := "https://api.openai.com/v1/organization/usage/completions?" + query.Encode()

	body, err := quotaGet(ctx, u, map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		// 403 = not an admin key — non-fatal
		if isHTTPStatus(err, http.StatusForbidden) {
			return &QuotaInfo{
				Provider:  "openai",
				Available: true,
				Error:     "admin API key required for usage data",
			}, nil
		}
		return nil, fmt.Errorf("openai usage: %w", err)
	}

	var resp struct {
		Data []struct {
			Results []struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("openai usage decode: %w", err)
	}

	totalUsed := 0
	for _, bucket := range resp.Data {
		for _, r := range bucket.Results {
			totalUsed += r.InputTokens + r.OutputTokens
		}
	}

	days := int(now.Sub(startTime).Hours() / 24)
	if days < 1 {
		days = 1
	}
	burnRate := totalUsed / days

	return &QuotaInfo{
		Provider:       "openai",
		Available:      true,
		UsedTokens:     totalUsed,
		BurnRatePerDay: burnRate,
	}, nil
}

// checkAnthropicQuota queries the Anthropic usage API for 7-day token consumption.
func checkAnthropicQuota(ctx context.Context, apiKey string) (*QuotaInfo, error) {
	now := time.Now()
	startTime := now.AddDate(0, 0, -7)

	query := url.Values{
		"starting_at":  {startTime.UTC().Format(time.RFC3339)},
		"ending_at":    {now.UTC().Format(time.RFC3339)},
		"bucket_width": {"1d"},
		"group_by":     {"model"},
		"limit":        {"100"},
	}

	u := "https://api.anthropic.com/v1/organizations/usage_report/messages?" + query.Encode()

	body, err := quotaGet(ctx, u, map[string]string{
		"x-api-key":         apiKey,
		"anthropic-version": "2023-06-01",
	})
	if err != nil {
		if isHTTPStatus(err, http.StatusForbidden) || isHTTPStatus(err, http.StatusUnauthorized) {
			return &QuotaInfo{
				Provider:  "anthropic",
				Available: true,
				Error:     "admin API key required for usage data",
			}, nil
		}
		return nil, fmt.Errorf("anthropic usage: %w", err)
	}

	var resp struct {
		Data []struct {
			Usage []struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("anthropic usage decode: %w", err)
	}

	totalUsed := 0
	for _, bucket := range resp.Data {
		for _, u := range bucket.Usage {
			totalUsed += u.InputTokens + u.OutputTokens
		}
	}

	days := int(now.Sub(startTime).Hours() / 24)
	if days < 1 {
		days = 1
	}
	burnRate := totalUsed / days

	return &QuotaInfo{
		Provider:       "anthropic",
		Available:      true,
		UsedTokens:     totalUsed,
		BurnRatePerDay: burnRate,
	}, nil
}

// checkDeepSeekQuota queries the DeepSeek balance API.
func checkDeepSeekQuota(ctx context.Context, apiKey string) (*QuotaInfo, error) {
	body, err := quotaGet(ctx, "https://api.deepseek.com/user/balance", map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("deepseek balance: %w", err)
	}

	var resp struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency       string `json:"currency"`
			TotalBalance   string `json:"total_balance"`
			GrantedBalance string `json:"granted_balance"`
			ToppedUp       string `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("deepseek balance decode: %w", err)
	}

	info := &QuotaInfo{
		Provider:  "deepseek",
		Available: resp.IsAvailable,
	}

	// prefer USD balance, fall back to first available
	for _, b := range resp.BalanceInfos {
		if b.Currency == "USD" {
			info.Balance = b.TotalBalance
			info.Currency = "USD"
			break
		}
		if info.Balance == "" {
			info.Balance = b.TotalBalance
			info.Currency = b.Currency
		}
	}

	if !resp.IsAvailable {
		info.Error = "insufficient balance"
	}

	return info, nil
}

// quotaGet performs a GET request with the given headers and returns the response body.
func quotaGet(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, quotaHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "tokencontrol")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &quotaHTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
		}
	}

	return body, nil
}

type quotaHTTPError struct {
	StatusCode int
	Body       string
}

func (e *quotaHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

func isHTTPStatus(err error, code int) bool {
	if he, ok := err.(*quotaHTTPError); ok {
		return he.StatusCode == code
	}
	return false
}

// RunnerToProvider maps a runner type to its API provider name.
// Returns empty string for providers with no quota endpoint.
func RunnerToProvider(runnerType string) string {
	switch runnerType {
	case "codex":
		return "openai"
	case "claude":
		return "anthropic"
	case "deepseek":
		return "deepseek"
	default:
		return ""
	}
}

// ProviderEnvVar returns the env var name for a provider's API key.
func ProviderEnvVar(provider string) string {
	switch provider {
	case "openai":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	default:
		return ""
	}
}

// CheckAllQuotas checks quotas for all providers that have API keys available.
func CheckAllQuotas(ctx context.Context, getenv func(string) string) []*QuotaInfo {
	providers := []string{"openai", "anthropic", "deepseek"}
	var results []*QuotaInfo

	for _, p := range providers {
		envVar := ProviderEnvVar(p)
		apiKey := getenv(envVar)
		if apiKey == "" {
			continue
		}

		info, err := CheckQuota(ctx, p, apiKey)
		if err != nil {
			slog.Debug("quota check failed", "provider", p, "error", err)
			results = append(results, &QuotaInfo{
				Provider:  p,
				Available: true,
				Error:     err.Error(),
			})
			continue
		}
		if info != nil {
			results = append(results, info)
		}
	}

	return results
}
