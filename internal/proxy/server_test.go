package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleResponses_NonStreaming(t *testing.T) {
	// mock upstream Chat Completions API
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("auth header: %s", r.Header.Get("Authorization"))
		}

		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "deepseek-chat" {
			t.Fatalf("model: %s", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("messages: %d", len(req.Messages))
		}

		resp := ChatCompletionResponse{
			ID: "chatcmpl-123", Model: "deepseek-chat",
			Choices: []Choice{{Message: ChatMessage{Role: "assistant", Content: "hi there"}}},
			Usage:   &Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	srv := New(Config{
		Listen: ":0",
		Targets: map[string]Target{
			"deepseek-chat": {BaseURL: upstream.URL, APIKey: "test-key"},
		},
	})
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	body := `{"model":"deepseek-chat","input":[{"type":"message","role":"user","content":"hello"}],"instructions":"be helpful","stream":false}`
	resp, err := http.Post("http://"+addr+"/v1/responses", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, string(b))
	}

	var result ResponsesAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Object != "response" {
		t.Fatalf("object: %s", result.Object)
	}
	if result.Status != "completed" {
		t.Fatalf("status: %s", result.Status)
	}
	if len(result.Output) != 1 {
		t.Fatalf("output: %d", len(result.Output))
	}
	if result.Output[0].Content[0].Text != "hi there" {
		t.Fatalf("text: %q", result.Output[0].Content[0].Text)
	}
	if result.Usage.InputTokens != 10 {
		t.Fatalf("usage: %+v", result.Usage)
	}
}

func TestHandleResponses_Streaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		stop := "stop"
		chunks := []ChatChunk{
			{ID: "c1", Model: "test", Choices: []ChunkChoice{{Delta: ChunkDelta{Role: "assistant", Content: "hello"}}}},
			{ID: "c2", Model: "test", Choices: []ChunkChoice{{Delta: ChunkDelta{Content: " world"}}}},
			{ID: "c3", Model: "test", Choices: []ChunkChoice{{Delta: ChunkDelta{}, FinishReason: &stop}}},
		}
		for _, ch := range chunks {
			b, _ := json.Marshal(ch)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	srv := New(Config{
		Listen:  ":0",
		Targets: map[string]Target{"test": {BaseURL: upstream.URL}},
	})
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	body := `{"model":"test","input":[{"type":"message","role":"user","content":"hi"}],"stream":true}`
	resp, err := http.Post("http://"+addr+"/v1/responses", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type: %s", resp.Header.Get("Content-Type"))
	}

	// read all SSE events
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	events := string(data)

	if !strings.Contains(events, "event: response.created") {
		t.Fatal("missing response.created event")
	}
	if !strings.Contains(events, "event: response.output_text.delta") {
		t.Fatal("missing text delta event")
	}
	if !strings.Contains(events, "event: response.completed") {
		t.Fatal("missing response.completed event")
	}
	if !strings.Contains(events, "hello world") {
		t.Fatal("missing full text in completed event")
	}
}

func TestHandleResponses_UnknownModel(t *testing.T) {
	srv := New(Config{
		Listen:  ":0",
		Targets: map[string]Target{},
	})
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	body := `{"model":"unknown","input":[{"type":"message","role":"user","content":"hi"}]}`
	resp, err := http.Post("http://"+addr+"/v1/responses", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}

func TestHandleResponses_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer upstream.Close()

	srv := New(Config{
		Listen:  ":0",
		Targets: map[string]Target{"test": {BaseURL: upstream.URL}},
	})
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	body := `{"model":"test","input":[{"type":"message","role":"user","content":"hi"}]}`
	resp, err := http.Post("http://"+addr+"/v1/responses", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want 429", resp.StatusCode)
	}
}

func TestHealth(t *testing.T) {
	srv := New(Config{Listen: ":0", Targets: map[string]Target{}})
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

func TestStartStop(t *testing.T) {
	srv := New(Config{Listen: ":0", Targets: map[string]Target{}})
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if addr == "" {
		t.Fatal("addr is empty")
	}

	// verify it's listening
	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	_ = resp.Body.Close()

	// stop and verify it's no longer listening
	if err := srv.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	_, err = http.Get("http://" + addr + "/health")
	if err == nil {
		t.Fatal("expected connection refused after stop")
	}
}
