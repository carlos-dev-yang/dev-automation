package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicAPIAdapterRequestCachingUsageAndErrors(t *testing.T) {
	var seenHeaders http.Header
	var seenBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		if r.Header.Get("x-api-key") == "bad-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_01ABC",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "{\"action\":\"REPORT_STATUS\",\"summary\":\"ok\"}"}],
			"usage": {
				"input_tokens": 1200,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens": 900,
				"output_tokens": 85
			}
		}`))
	}))
	defer srv.Close()

	ctx := context.Background()

	// 1. Missing API key returns helpful error pointing to ~/.dev-automation/.env.
	emptyAdapter := NewAnthropicAPIAdapter("", srv.URL)
	if _, err := emptyAdapter.Run(ctx, Request{Prompt: "hi", Mode: ModeReadOnly}); err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("expected missing ANTHROPIC_API_KEY error, got %v", err)
	}

	// 2. Successful Messages API call with Prompt Caching and structured JSON output.
	adapter := NewAnthropicAPIAdapter("sk-ant-test-key", srv.URL)
	resp, err := adapter.Run(ctx, Request{
		SystemPrompt: "You are Central Orchestrator.",
		Prompt:       "Prioritize tasks",
		Mode:         ModeReadOnly,
		Model:        "claude-haiku-5-5",
		JSONSchema:   `{"type":"object"}`,
	})
	if err != nil {
		t.Fatalf("AnthropicAPIAdapter.Run failed: %v", err)
	}
	if seenHeaders.Get("x-api-key") != "sk-ant-test-key" || seenHeaders.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("unexpected headers: %+v", seenHeaders)
	}
	if seenBody["model"] != "claude-haiku-5-5" {
		t.Fatalf("expected model claude-haiku-5-5, got %v", seenBody["model"])
	}
	sysBlocks, ok := seenBody["system"].([]any)
	if !ok || len(sysBlocks) != 1 {
		t.Fatalf("expected 1 cached system block, got %#v", seenBody["system"])
	}
	blockMap := sysBlocks[0].(map[string]any)
	cc, _ := blockMap["cache_control"].(map[string]any)
	if cc["type"] != "ephemeral" {
		t.Fatalf("expected ephemeral cache_control on system block, got %#v", blockMap)
	}
	if resp.Structured != `{"action":"REPORT_STATUS","summary":"ok"}` {
		t.Fatalf("unexpected structured output: %q", resp.Structured)
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 1200 || resp.Usage.CacheReadInputTokens != 900 || resp.Usage.OutputTokens != 85 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}

	// 3. Unauthorized API response surfaces structured Anthropic error message.
	badAdapter := NewAnthropicAPIAdapter("bad-key", srv.URL)
	if _, err := badAdapter.Run(ctx, Request{Prompt: "hi", Mode: ModeReadOnly}); err == nil || !strings.Contains(err.Error(), "authentication_error") {
		t.Fatalf("expected authentication_error, got %v", err)
	}

	// 4. ModeWorkspaceWrite is rejected explicitly.
	if _, err := adapter.Run(ctx, Request{Prompt: "edit file", Mode: ModeWorkspaceWrite}); err == nil {
		t.Fatalf("expected ModeWorkspaceWrite to be rejected by anthropic-api adapter")
	}
}
