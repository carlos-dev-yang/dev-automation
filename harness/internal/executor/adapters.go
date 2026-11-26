package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ClaudeAdapter invokes Claude Code CLI in headless (-p) mode.
type ClaudeAdapter struct {
	Binary string
}

func NewClaudeAdapter() *ClaudeAdapter {
	return &ClaudeAdapter{Binary: "claude"}
}

func (a *ClaudeAdapter) Name() string { return "claude" }

func (a *ClaudeAdapter) Run(ctx context.Context, req Request) (*Response, error) {
	prompt, err := BuildPromptWithSkills(req)
	if err != nil {
		return nil, err
	}

	args := []string{"-p", prompt, "--output-format", "json"}
	if req.Mode == ModeReadOnly {
		args = append(args, "--permission-mode", "plan", "--disallowedTools", "Edit,Write,NotebookEdit")
	} else {
		args = append(args, "--permission-mode", "acceptEdits")
	}
	if req.SessionID != "" {
		args = append(args, "-r", req.SessionID)
	}
	if req.JSONSchema != "" {
		args = append(args, "--json-schema", req.JSONSchema)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", req.MaxBudgetUSD))
	}
	for _, dir := range req.AdditionalDirs {
		args = append(args, "--add-dir", dir)
	}

	raw, exitCode, timedOut, dur, runErr := runProcessGroup(ctx, req.WorkDir, a.Binary, args, "", req.ExtraEnv, req.Timeout)
	resp := &Response{
		Adapter:   a.Name(),
		RawOutput: raw,
		ExitCode:  exitCode,
		TimedOut:  timedOut,
		Duration:  dur,
	}

	var envelope struct {
		SessionID        string          `json:"session_id"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		IsError          bool            `json:"is_error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &envelope); err == nil {
		resp.SessionID = envelope.SessionID
		resp.TextOutput = envelope.Result
		if len(envelope.StructuredOutput) > 0 && string(envelope.StructuredOutput) != "null" {
			resp.Structured = string(envelope.StructuredOutput)
		} else if req.JSONSchema != "" {
			if extracted, err := ExtractJSONObject(envelope.Result); err == nil {
				resp.Structured = extracted
			}
		}
		if envelope.IsError && runErr == nil {
			runErr = fmt.Errorf("claude returned error: %s", envelope.Result)
		}
	} else {
		resp.TextOutput = raw
		if req.JSONSchema != "" {
			if extracted, err := ExtractJSONObject(raw); err == nil {
				resp.Structured = extracted
			}
		}
	}

	return resp, runErr
}

// GeminiAdapter invokes Gemini CLI in headless (-p) mode.
type GeminiAdapter struct {
	Binary string
}

func NewGeminiAdapter() *GeminiAdapter {
	return &GeminiAdapter{Binary: "gemini"}
}

func (a *GeminiAdapter) Name() string { return "gemini" }

func (a *GeminiAdapter) Run(ctx context.Context, req Request) (*Response, error) {
	prompt, err := BuildPromptWithSkills(req)
	if err != nil {
		return nil, err
	}

	resp, runErr := a.runOnce(ctx, req, prompt, req.SessionID)
	// If resuming an expired/unsupported session failed, transparently retry once with a fresh session.
	if runErr != nil && req.SessionID != "" && !resp.TimedOut && isSessionResumeError(runErr, resp.RawOutput) {
		resp, runErr = a.runOnce(ctx, req, prompt, "")
	}
	return resp, runErr
}

func (a *GeminiAdapter) runOnce(ctx context.Context, req Request, prompt, sessionID string) (*Response, error) {
	// Pass prompt via stdin to avoid OS argv length limits when injecting SKILL.md + git diff.
	// Gemini CLI 0.22+ supports --approval-mode choices: default | auto_edit | yolo (default blocks write tools in headless mode).
	args := []string{"-p", "Follow the instructions provided on stdin.", "-o", "json"}
	if req.Mode == ModeReadOnly {
		args = append(args, "--approval-mode", "default")
	} else {
		args = append(args, "--approval-mode", "auto_edit")
	}
	if sessionID != "" {
		args = append(args, "-r", sessionID)
	}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	for _, dir := range req.AdditionalDirs {
		args = append(args, "--include-directories", dir)
	}

	raw, exitCode, timedOut, dur, runErr := runProcessGroup(ctx, req.WorkDir, a.Binary, args, prompt, req.ExtraEnv, req.Timeout)
	resp := &Response{
		Adapter:   a.Name(),
		RawOutput: raw,
		ExitCode:  exitCode,
		TimedOut:  timedOut,
		Duration:  dur,
	}

	// Gemini CLI may print non-JSON warning lines before the JSON object; extract outermost JSON envelope.
	envelopeStr := strings.TrimSpace(raw)
	if extracted, err := ExtractJSONObject(envelopeStr); err == nil {
		envelopeStr = extracted
	}

	var envelope struct {
		SessionID string `json:"session_id"`
		Response  string `json:"response"`
		Error     *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(envelopeStr), &envelope); err == nil && (envelope.Response != "" || envelope.SessionID != "" || envelope.Error != nil) {
		resp.SessionID = envelope.SessionID
		resp.TextOutput = envelope.Response
		if req.JSONSchema != "" && envelope.Response != "" {
			if extracted, err := ExtractJSONObject(envelope.Response); err == nil {
				resp.Structured = extracted
			}
		}
		if envelope.Error != nil {
			msg := strings.TrimSpace(envelope.Error.Message)
			if envelope.Error.Type != "" && msg != "" {
				runErr = fmt.Errorf("gemini %s: %s", envelope.Error.Type, msg)
			} else if msg != "" {
				runErr = fmt.Errorf("gemini error: %s", msg)
			} else {
				runErr = fmt.Errorf("gemini returned error (code=%d)", envelope.Error.Code)
			}
		}
	} else {
		resp.TextOutput = raw
		if req.JSONSchema != "" {
			if extracted, err := ExtractJSONObject(raw); err == nil {
				resp.Structured = extracted
			}
		}
	}

	return resp, runErr
}

func isSessionResumeError(err error, raw string) bool {
	combined := strings.ToLower(err.Error() + " " + raw)
	return strings.Contains(combined, "session") && (strings.Contains(combined, "not found") || strings.Contains(combined, "invalid") || strings.Contains(combined, "resume"))
}

// CodexAdapter invokes OpenAI Codex CLI (`codex exec`) backed by ChatGPT subscription auth.
// If AuthHome contains stored credentials (e.g. ~/.dev-automation/auth/codex/auth.json), it sets CODEX_HOME automatically;
// otherwise it falls back to the user's default Codex login (~/.codex).
type CodexAdapter struct {
	Binary   string
	AuthHome string
}

const macOSBundledCodexBinary = "/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"

// ResolveCodexBinary picks an explicit override first, then the newer ChatGPT desktop bundled Codex CLI (if installed),
// and falls back to "codex" on PATH.
func ResolveCodexBinary(override string) string {
	if s := strings.TrimSpace(override); s != "" {
		return s
	}
	if st, err := os.Stat(macOSBundledCodexBinary); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
		return macOSBundledCodexBinary
	}
	return "codex"
}

func NewCodexAdapter(authHome string) *CodexAdapter {
	return NewCodexAdapterWithBinary("", authHome)
}

func NewCodexAdapterWithBinary(binary, authHome string) *CodexAdapter {
	return &CodexAdapter{
		Binary:   ResolveCodexBinary(binary),
		AuthHome: authHome,
	}
}

func (a *CodexAdapter) Name() string { return "codex" }

func (a *CodexAdapter) Run(ctx context.Context, req Request) (*Response, error) {
	prompt, err := BuildPromptWithSkills(req)
	if err != nil {
		return nil, err
	}

	resp, runErr := a.runOnce(ctx, req, prompt, req.SessionID)
	if runErr != nil && req.SessionID != "" && !resp.TimedOut && isSessionResumeError(runErr, resp.RawOutput) {
		resp, runErr = a.runOnce(ctx, req, prompt, "")
	}
	return resp, runErr
}

func (a *CodexAdapter) runOnce(ctx context.Context, req Request, prompt, sessionID string) (*Response, error) {
	tmpDir, err := os.MkdirTemp("", "rd-codex-*")
	if err != nil {
		return nil, fmt.Errorf("create codex temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	lastMsgFile := filepath.Join(tmpDir, "last-message.txt")
	var schemaFile string
	if strings.TrimSpace(req.JSONSchema) != "" {
		schemaFile = filepath.Join(tmpDir, "schema.json")
		if err := os.WriteFile(schemaFile, []byte(req.JSONSchema), 0o600); err != nil {
			return nil, fmt.Errorf("write codex output schema: %w", err)
		}
	}

	sandbox := "read-only"
	if req.Mode == ModeWorkspaceWrite {
		sandbox = "workspace-write"
	}

	var args []string
	if sessionID != "" {
		args = append(args, "exec", "resume", sessionID)
	} else {
		args = append(args, "exec")
	}
	args = append(args, "--skip-git-repo-check", "--color", "never", "--json", "-s", sandbox, "-o", lastMsgFile)
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	if schemaFile != "" {
		args = append(args, "--output-schema", schemaFile)
	}
	for _, dir := range req.AdditionalDirs {
		args = append(args, "--add-dir", dir)
	}
	// Read prompt from stdin via '-' to avoid OS argv length limits.
	args = append(args, "-")

	extraEnv := append([]string{}, req.ExtraEnv...)
	if a.AuthHome != "" {
		if _, err := os.Stat(filepath.Join(a.AuthHome, "auth.json")); err == nil {
			extraEnv = append(extraEnv, "CODEX_HOME="+a.AuthHome)
		}
	}

	raw, exitCode, timedOut, dur, runErr := runProcessGroup(ctx, req.WorkDir, a.Binary, args, prompt, extraEnv, req.Timeout)
	resp := &Response{
		Adapter:   a.Name(),
		RawOutput: raw,
		ExitCode:  exitCode,
		TimedOut:  timedOut,
		Duration:  dur,
	}

	// Extract thread/session id and token usage from JSONL events if emitted.
	resp.SessionID = extractCodexSessionID(raw)
	resp.Usage = extractCodexUsage(raw)

	if data, err := os.ReadFile(lastMsgFile); err == nil && len(bytes.TrimSpace(data)) > 0 {
		resp.TextOutput = strings.TrimSpace(string(data))
	} else {
		resp.TextOutput = extractCodexTextFallback(raw)
	}

	if req.JSONSchema != "" && resp.TextOutput != "" {
		if extracted, err := ExtractJSONObject(resp.TextOutput); err == nil {
			resp.Structured = extracted
		}
	}

	return resp, runErr
}

func extractCodexSessionID(jsonl string) string {
	for _, line := range strings.Split(jsonl, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev struct {
			ThreadID  string `json:"thread_id"`
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err == nil {
			if ev.ThreadID != "" {
				return ev.ThreadID
			}
			if ev.SessionID != "" {
				return ev.SessionID
			}
		}
	}
	return ""
}

func extractCodexUsage(jsonl string) *TokenUsage {
	var usage *TokenUsage
	for _, line := range strings.Split(jsonl, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Usage *struct {
				InputTokens       int `json:"input_tokens"`
				CachedInputTokens int `json:"cached_input_tokens"`
				CacheWriteTokens  int `json:"cache_write_input_tokens"`
				OutputTokens      int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Usage != nil {
			usage = &TokenUsage{
				InputTokens:              ev.Usage.InputTokens,
				CacheReadInputTokens:     ev.Usage.CachedInputTokens,
				CacheCreationInputTokens: ev.Usage.CacheWriteTokens,
				OutputTokens:             ev.Usage.OutputTokens,
			}
		}
	}
	return usage
}

func extractCodexTextFallback(raw string) string {
	var last string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Item *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Item != nil && ev.Item.Text != "" {
			last = ev.Item.Text
		}
	}
	if last != "" {
		return strings.TrimSpace(last)
	}
	return strings.TrimSpace(raw)
}

// PendingProviderAdapter is a modular placeholder for AI providers whose API/subscription transport
// will be wired later (e.g. OpenAI / GPT-6 Luna). Swapping in a real implementation requires no caller changes.
type PendingProviderAdapter struct {
	ProviderName string
	Note         string
}

func NewPendingProviderAdapter(name, note string) *PendingProviderAdapter {
	return &PendingProviderAdapter{
		ProviderName: name,
		Note:         note,
	}
}

func (p *PendingProviderAdapter) Name() string { return p.ProviderName }

func (p *PendingProviderAdapter) Run(_ context.Context, req Request) (*Response, error) {
	model := req.Model
	if model == "" {
		model = "default"
	}
	note := p.Note
	if note == "" {
		note = "attach API or subscription client implementation in internal/executor"
	}
	return &Response{
		Adapter:  p.ProviderName,
		ExitCode: -1,
	}, fmt.Errorf("provider %q (model=%s) is not connected yet: %s", p.ProviderName, model, note)
}

// MockAdapter supports deterministic offline testing of the full state machine and CLI/Telegram gateways.
type MockAdapter struct {
	AdapterName string
	Handler     func(ctx context.Context, req Request) (*Response, error)

	mu    sync.Mutex
	Calls []Request
}

func NewMockAdapter(name string, handler func(ctx context.Context, req Request) (*Response, error)) *MockAdapter {
	return &MockAdapter{
		AdapterName: name,
		Handler:     handler,
	}
}

func (m *MockAdapter) Name() string { return m.AdapterName }

func (m *MockAdapter) Run(ctx context.Context, req Request) (*Response, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, req)
	m.mu.Unlock()
	if m.Handler == nil {
		return &Response{Adapter: m.AdapterName, TextOutput: "ok", ExitCode: 0}, nil
	}
	return m.Handler(ctx, req)
}

func (m *MockAdapter) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls)
}

func runProcessGroup(ctx context.Context, workDir, binary string, args []string, stdinText string, extraEnv []string, timeout time.Duration) (string, int, bool, time.Duration, error) {
	start := time.Now()
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = workDir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	if stdinText != "" {
		cmd.Stdin = strings.NewReader(stdinText)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return stderr.String(), -1, false, time.Since(start), fmt.Errorf("start %s: %w", binary, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var waitErr error
	timedOut := false
	select {
	case waitErr = <-done:
	case <-runCtx.Done():
		timedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded)
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		waitErr = <-done
	}

	dur := time.Since(start)
	if waitErr == nil && runCtx.Err() == nil {
		return stdout.String(), 0, false, dur, nil
	}

	exitCode := -1
	if ee, ok := waitErr.(*exec.ExitError); ok {
		exitCode = ee.ExitCode()
	}
	errMsg := strings.TrimSpace(stderr.String())
	if errMsg == "" {
		errMsg = strings.TrimSpace(stdout.String())
	}
	if timedOut {
		return stdout.String(), exitCode, true, dur, fmt.Errorf("%s timed out after %s: %s", binary, timeout, errMsg)
	}
	if waitErr != nil {
		return stdout.String(), exitCode, false, dur, fmt.Errorf("%s exited %d: %s", binary, exitCode, errMsg)
	}
	return stdout.String(), exitCode, false, dur, runCtx.Err()
}
