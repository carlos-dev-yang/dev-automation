package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	defaultAnthropicVersion = "2023-06-01"
	defaultAnthropicModel   = "claude-haiku-5-5"
	defaultAnthropicMaxTok  = 4096
)

// AnthropicAuthProvider abstracts how Anthropic API requests authenticate.
// Default implementation uses a static API key (x-api-key); later cloud deployments
// can plug in Workload Identity Federation (OIDC -> Bearer token) without changing adapter callers.
type AnthropicAuthProvider interface {
	ApplyAuth(ctx context.Context, req *http.Request) error
}

type StaticAnthropicAPIKey struct {
	APIKey string
}

func (p StaticAnthropicAPIKey) ApplyAuth(_ context.Context, req *http.Request) error {
	key := strings.TrimSpace(p.APIKey)
	if key == "" {
		return errors.New("ANTHROPIC_API_KEY is not set (add it to ~/.dev-automation/.env or export ANTHROPIC_API_KEY)")
	}
	req.Header.Set("x-api-key", key)
	return nil
}

// AnthropicAPIAdapter calls Anthropic Messages API directly over HTTPS (no CLI subprocess overhead),
// enabling Prompt Caching on the system/skills prompt block and exact token usage reporting.
type AnthropicAPIAdapter struct {
	Auth       AnthropicAuthProvider
	BaseURL    string
	APIVersion string
	MaxTokens  int
	HTTPClient *http.Client
}

func NewAnthropicAPIAdapter(apiKey, baseURL string) *AnthropicAPIAdapter {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultAnthropicBaseURL
	}
	return &AnthropicAPIAdapter{
		Auth:       StaticAnthropicAPIKey{APIKey: apiKey},
		BaseURL:    base,
		APIVersion: defaultAnthropicVersion,
		MaxTokens:  defaultAnthropicMaxTok,
		HTTPClient: &http.Client{Timeout: 2 * time.Minute},
	}
}

func (a *AnthropicAPIAdapter) Name() string { return "anthropic-api" }

func (a *AnthropicAPIAdapter) Run(ctx context.Context, req Request) (*Response, error) {
	start := time.Now()
	if req.Mode == ModeWorkspaceWrite {
		return nil, errors.New("anthropic-api adapter is text/JSON-only and does not support ModeWorkspaceWrite")
	}

	systemText, userText, err := buildAnthropicPrompts(req)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = defaultAnthropicModel
	}
	maxTokens := a.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTok
	}

	type cacheControl struct {
		Type string `json:"type"`
	}
	type textBlock struct {
		Type         string        `json:"type"`
		Text         string        `json:"text"`
		CacheControl *cacheControl `json:"cache_control,omitempty"`
	}
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	bodyPayload := struct {
		Model     string      `json:"model"`
		MaxTokens int         `json:"max_tokens"`
		System    []textBlock `json:"system,omitempty"`
		Messages  []message   `json:"messages"`
	}{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  []message{{Role: "user", Content: userText}},
	}
	if systemText != "" {
		bodyPayload.System = []textBlock{{
			Type:         "text",
			Text:         systemText,
			CacheControl: &cacheControl{Type: "ephemeral"},
		}}
	}

	reqBytes, err := json.Marshal(bodyPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	base := strings.TrimRight(strings.TrimSpace(a.BaseURL), "/")
	if base == "" {
		base = defaultAnthropicBaseURL
	}
	httpReq, err := http.NewRequestWithContext(runCtx, http.MethodPost, base+"/v1/messages", bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("build anthropic request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	ver := a.APIVersion
	if ver == "" {
		ver = defaultAnthropicVersion
	}
	httpReq.Header.Set("anthropic-version", ver)

	if a.Auth == nil {
		return nil, errors.New("anthropic-api adapter has no auth provider configured")
	}
	if err := a.Auth.ApplyAuth(runCtx, httpReq); err != nil {
		return nil, err
	}

	client := a.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	httpRes, err := client.Do(httpReq)
	dur := time.Since(start)
	if err != nil {
		timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
		return &Response{Adapter: a.Name(), ExitCode: -1, Duration: dur, TimedOut: timedOut}, fmt.Errorf("anthropic-api request failed: %w", err)
	}
	defer httpRes.Body.Close()

	rawBytes, err := io.ReadAll(httpRes.Body)
	if err != nil {
		return &Response{Adapter: a.Name(), ExitCode: -1, Duration: dur}, fmt.Errorf("read anthropic response: %w", err)
	}
	rawStr := string(rawBytes)

	if httpRes.StatusCode < 200 || httpRes.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(rawBytes, &apiErr) == nil && apiErr.Error.Message != "" {
			return &Response{Adapter: a.Name(), RawOutput: rawStr, ExitCode: httpRes.StatusCode, Duration: dur},
				fmt.Errorf("anthropic-api error (%d %s): %s", httpRes.StatusCode, apiErr.Error.Type, apiErr.Error.Message)
		}
		return &Response{Adapter: a.Name(), RawOutput: rawStr, ExitCode: httpRes.StatusCode, Duration: dur},
			fmt.Errorf("anthropic-api HTTP %d: %s", httpRes.StatusCode, strings.TrimSpace(rawStr))
	}

	var msgResp struct {
		ID      string `json:"id"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			OutputTokens             int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rawBytes, &msgResp); err != nil {
		return &Response{Adapter: a.Name(), RawOutput: rawStr, ExitCode: -1, Duration: dur}, fmt.Errorf("decode anthropic response: %w", err)
	}

	var texts []string
	for _, block := range msgResp.Content {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	textOut := strings.TrimSpace(strings.Join(texts, "\n"))

	resp := &Response{
		Adapter:    a.Name(),
		SessionID:  msgResp.ID,
		RawOutput:  rawStr,
		TextOutput: textOut,
		ExitCode:   0,
		Duration:   dur,
		Usage: &TokenUsage{
			InputTokens:              msgResp.Usage.InputTokens,
			CacheCreationInputTokens: msgResp.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     msgResp.Usage.CacheReadInputTokens,
			OutputTokens:             msgResp.Usage.OutputTokens,
		},
	}

	if req.JSONSchema != "" && textOut != "" {
		if extracted, err := ExtractJSONObject(textOut); err == nil {
			resp.Structured = extracted
		}
	}
	return resp, nil
}

func buildAnthropicPrompts(req Request) (string, string, error) {
	var sys strings.Builder
	if strings.TrimSpace(req.SystemPrompt) != "" {
		sys.WriteString(strings.TrimSpace(req.SystemPrompt))
	}
	if len(req.SkillFiles) > 0 {
		if sys.Len() > 0 {
			sys.WriteString("\n\n")
		}
		sys.WriteString("## Mandatory Skills (Injected by Harness — Follow Strictly)\n")
		for _, sf := range req.SkillFiles {
			data, err := os.ReadFile(sf)
			if err != nil {
				return "", "", fmt.Errorf("load mandatory skill %s: %w", sf, err)
			}
			sys.WriteString(fmt.Sprintf("\n### Skill: %s\n%s\n", filepath.Base(filepath.Dir(sf)), string(data)))
		}
	}
	if strings.TrimSpace(req.JSONSchema) != "" {
		if sys.Len() > 0 {
			sys.WriteString("\n\n")
		}
		sys.WriteString("## Mandatory Structured Output\nRespond with ONLY a single valid JSON object matching this JSON Schema (no markdown fences or commentary outside JSON):\n")
		sys.WriteString(strings.TrimSpace(req.JSONSchema))
	}
	return sys.String(), req.Prompt, nil
}
