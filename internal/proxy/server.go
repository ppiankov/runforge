package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Target describes an upstream Chat Completions API endpoint.
type Target struct {
	BaseURL string // e.g. "https://api.deepseek.com"
	APIKey  string // resolved API key (not "env:...")
}

// Config holds proxy server configuration.
type Config struct {
	Listen  string            // ":4000"
	Targets map[string]Target // model name → target
}

// Server is the Responses API → Chat Completions translation proxy.
type Server struct {
	cfg    Config
	srv    *http.Server
	client *http.Client
	mu     sync.Mutex
	addr   string
}

// New creates a new proxy server.
func New(cfg Config) *Server {
	return &Server{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

// Start begins listening. Returns the actual address.
func (s *Server) Start() (string, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/responses", s.handleResponses)
	mux.HandleFunc("/health", s.handleHealth)

	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return "", fmt.Errorf("proxy listen %s: %w", s.cfg.Listen, err)
	}

	s.mu.Lock()
	s.addr = ln.Addr().String()
	s.srv = &http.Server{Handler: mux}
	s.mu.Unlock()

	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("proxy server error", "error", err)
		}
	}()

	slog.Info("proxy started", "addr", s.addr, "targets", len(s.cfg.Targets))
	return s.addr, nil
}

// Stop gracefully shuts down the proxy.
func (s *Server) Stop() error {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// Addr returns the listening address after Start.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	var req ResponsesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	target, ok := s.resolveTarget(req.Model)
	if !ok {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("no target configured for model %q", req.Model))
		return
	}

	chatReq, err := TranslateRequest(&req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "translate request: "+err.Error())
		return
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "marshal request: "+err.Error())
		return
	}

	upstreamURL := strings.TrimRight(target.BaseURL, "/") + "/v1/chat/completions"
	upReq, err := http.NewRequestWithContext(r.Context(), "POST", upstreamURL, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create upstream request: "+err.Error())
		return
	}
	upReq.Header.Set("Content-Type", "application/json")

	// target API key takes precedence; fallback to pass-through
	if target.APIKey != "" {
		upReq.Header.Set("Authorization", "Bearer "+target.APIKey)
	} else if auth := r.Header.Get("Authorization"); auth != "" {
		upReq.Header.Set("Authorization", auth)
	}

	slog.Debug("proxy forwarding", "model", req.Model, "upstream", upstreamURL, "stream", req.Stream)

	upResp, err := s.client.Do(upReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream error: "+err.Error())
		return
	}
	defer func() { _ = upResp.Body.Close() }()

	// pass through upstream errors
	if upResp.StatusCode >= 400 {
		w.Header().Set("Content-Type", upResp.Header.Get("Content-Type"))
		w.WriteHeader(upResp.StatusCode)
		_, _ = io.Copy(w, upResp.Body)
		return
	}

	if req.Stream {
		s.handleStreamingResponse(w, upResp)
	} else {
		s.handleNonStreamingResponse(w, upResp)
	}
}

func (s *Server) handleNonStreamingResponse(w http.ResponseWriter, upResp *http.Response) {
	var chatResp ChatCompletionResponse
	if err := json.NewDecoder(upResp.Body).Decode(&chatResp); err != nil {
		writeError(w, http.StatusBadGateway, "decode upstream response: "+err.Error())
		return
	}

	resp := TranslateResponse(&chatResp)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStreamingResponse(w http.ResponseWriter, upResp *http.Response) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	translator := NewStreamTranslator()
	scanner := bufio.NewScanner(upResp.Body)
	// 256KB buffer for large chunks
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var chunk ChatChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			slog.Debug("proxy: skip unparseable chunk", "error", err)
			continue
		}

		events, done := translator.TranslateChunk(&chunk)
		for _, ev := range events {
			_, _ = fmt.Fprint(w, ev.Format())
		}
		flusher.Flush()

		if done {
			break
		}
	}
}

func (s *Server) resolveTarget(model string) (Target, bool) {
	if t, ok := s.cfg.Targets[model]; ok {
		return t, true
	}
	if t, ok := s.cfg.Targets["default"]; ok {
		return t, true
	}
	return Target{}, false
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	resp := map[string]any{"error": map[string]any{"message": msg, "code": code}}
	_ = json.NewEncoder(w).Encode(resp)
}
