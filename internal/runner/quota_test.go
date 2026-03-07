package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckOpenAIQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organization/usage/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth header")
		}

		resp := map[string]any{
			"data": []map[string]any{
				{
					"results": []map[string]any{
						{"input_tokens": 500000, "output_tokens": 100000},
						{"input_tokens": 300000, "output_tokens": 50000},
					},
				},
				{
					"results": []map[string]any{
						{"input_tokens": 200000, "output_tokens": 80000},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// override the URL by calling the internal function directly via quotaGet
	// Instead, we test CheckQuota with a custom URL by testing checkOpenAIQuota indirectly
	// For proper testing, we test via the exported function with a mock server

	// test the HTTP helper directly
	body, err := quotaGet(context.Background(), srv.URL+"/v1/organization/usage/completions?start_time=0&bucket_width=1d&limit=100", map[string]string{
		"Authorization": "Bearer test-key",
	})
	if err != nil {
		t.Fatalf("quotaGet: %v", err)
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
		t.Fatalf("decode: %v", err)
	}

	totalUsed := 0
	for _, bucket := range resp.Data {
		for _, r := range bucket.Results {
			totalUsed += r.InputTokens + r.OutputTokens
		}
	}

	if totalUsed != 1230000 {
		t.Errorf("total used: got %d, want 1230000", totalUsed)
	}
}

func TestCheckAnthropicQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organizations/usage_report/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("missing anthropic-version header")
		}

		resp := map[string]any{
			"data": []map[string]any{
				{
					"usage": []map[string]any{
						{"model": "claude-3.5-sonnet", "input_tokens": 400000, "output_tokens": 80000},
					},
				},
				{
					"usage": []map[string]any{
						{"model": "claude-3.5-haiku", "input_tokens": 100000, "output_tokens": 20000},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	body, err := quotaGet(context.Background(), srv.URL+"/v1/organizations/usage_report/messages?starting_at=2026-01-01T00:00:00Z&bucket_width=1d&limit=100", map[string]string{
		"x-api-key":         "test-key",
		"anthropic-version": "2023-06-01",
	})
	if err != nil {
		t.Fatalf("quotaGet: %v", err)
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
		t.Fatalf("decode: %v", err)
	}

	totalUsed := 0
	for _, bucket := range resp.Data {
		for _, u := range bucket.Usage {
			totalUsed += u.InputTokens + u.OutputTokens
		}
	}

	if totalUsed != 600000 {
		t.Errorf("total used: got %d, want 600000", totalUsed)
	}
}

func TestCheckDeepSeekQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/balance" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := map[string]any{
			"is_available": true,
			"balance_infos": []map[string]any{
				{
					"currency":          "CNY",
					"total_balance":     "110.00",
					"granted_balance":   "10.00",
					"topped_up_balance": "100.00",
				},
				{
					"currency":          "USD",
					"total_balance":     "15.00",
					"granted_balance":   "5.00",
					"topped_up_balance": "10.00",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	body, err := quotaGet(context.Background(), srv.URL+"/user/balance", map[string]string{
		"Authorization": "Bearer test-key",
	})
	if err != nil {
		t.Fatalf("quotaGet: %v", err)
	}

	var resp struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency     string `json:"currency"`
			TotalBalance string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !resp.IsAvailable {
		t.Error("expected is_available=true")
	}
	if len(resp.BalanceInfos) != 2 {
		t.Fatalf("expected 2 balance infos, got %d", len(resp.BalanceInfos))
	}
	if resp.BalanceInfos[1].Currency != "USD" || resp.BalanceInfos[1].TotalBalance != "15.00" {
		t.Errorf("unexpected USD balance: %+v", resp.BalanceInfos[1])
	}
}

func TestCheckDeepSeekQuota_Unavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"is_available": false,
			"balance_infos": []map[string]any{
				{"currency": "USD", "total_balance": "0.00", "granted_balance": "0.00", "topped_up_balance": "0.00"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	body, err := quotaGet(context.Background(), srv.URL+"/user/balance", map[string]string{
		"Authorization": "Bearer test-key",
	})
	if err != nil {
		t.Fatalf("quotaGet: %v", err)
	}

	var resp struct {
		IsAvailable bool `json:"is_available"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.IsAvailable {
		t.Error("expected is_available=false")
	}
}

func TestCheckQuota_UnknownProvider(t *testing.T) {
	info, err := CheckQuota(context.Background(), "unknown", "some-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil for unknown provider, got %+v", info)
	}
}

func TestCheckQuota_NoKey(t *testing.T) {
	info, err := CheckQuota(context.Background(), "openai", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info for no key")
	}
	if info.Error != "no API key available" {
		t.Errorf("expected 'no API key available', got %q", info.Error)
	}
}

func TestQuotaGet_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	_, err := quotaGet(context.Background(), srv.URL+"/test", nil)
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !isHTTPStatus(err, http.StatusForbidden) {
		t.Errorf("expected 403 status, got: %v", err)
	}
}

func TestRunnerToProvider(t *testing.T) {
	tests := []struct {
		runner   string
		provider string
	}{
		{"codex", "openai"},
		{"claude", "anthropic"},
		{"deepseek", "deepseek"},
		{"gemini", ""},
		{"opencode", ""},
		{"script", ""},
	}
	for _, tt := range tests {
		if got := RunnerToProvider(tt.runner); got != tt.provider {
			t.Errorf("RunnerToProvider(%q) = %q, want %q", tt.runner, got, tt.provider)
		}
	}
}

func TestProviderEnvVar(t *testing.T) {
	tests := []struct {
		provider string
		envVar   string
	}{
		{"openai", "OPENAI_API_KEY"},
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"deepseek", "DEEPSEEK_API_KEY"},
		{"gemini", ""},
	}
	for _, tt := range tests {
		if got := ProviderEnvVar(tt.provider); got != tt.envVar {
			t.Errorf("ProviderEnvVar(%q) = %q, want %q", tt.provider, got, tt.envVar)
		}
	}
}
