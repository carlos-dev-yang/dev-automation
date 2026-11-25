package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
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

	raw, exitCode, timedOut, dur, runErr := runProcessGroup(ctx, req.WorkDir, a.Binary, args, "", req.Timeout)
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

	// Pass prompt via stdin to avoid OS argv length limits when injecting SKILL.md + git diff.
	args := []string{"-p", "Follow the instructions provided on stdin.", "-o", "json"}
	if req.Mode == ModeReadOnly {
		args = append(args, "--approval-mode", "plan")
	} else {
		args = append(args, "--approval-mode", "auto_edit")
	}
	if req.SessionID != "" {
		args = append(args, "-r", req.SessionID)
	}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	for _, dir := range req.AdditionalDirs {
		args = append(args, "--include-directories", dir)
	}

	raw, exitCode, timedOut, dur, runErr := runProcessGroup(ctx, req.WorkDir, a.Binary, args, prompt, req.Timeout)
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
		Error     any    `json:"error"`
	}
	if err := json.Unmarshal([]byte(envelopeStr), &envelope); err == nil && (envelope.Response != "" || envelope.SessionID != "") {
		resp.SessionID = envelope.SessionID
		resp.TextOutput = envelope.Response
		if req.JSONSchema != "" {
			if extracted, err := ExtractJSONObject(envelope.Response); err == nil {
				resp.Structured = extracted
			}
		}
		if envelope.Error != nil && runErr == nil {
			runErr = fmt.Errorf("gemini returned error: %v", envelope.Error)
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

func runProcessGroup(ctx context.Context, workDir, binary string, args []string, stdinText string, timeout time.Duration) (string, int, bool, time.Duration, error) {
	start := time.Now()
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = workDir
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
