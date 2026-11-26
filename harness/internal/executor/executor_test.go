package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyReadOnlyUnchanged(t *testing.T) {
	if err := VerifyReadOnlyUnchanged(" M a.go\n", " M a.go\n"); err != nil {
		t.Fatalf("expected unchanged to pass: %v", err)
	}
	if err := VerifyReadOnlyUnchanged(" M a.go\n", " M a.go\n?? sneak.go\n"); !errors.Is(err, ErrReadOnlyViolation) {
		t.Fatalf("expected ErrReadOnlyViolation, got %v", err)
	}
}

func TestVerifyAllowedWritePaths(t *testing.T) {
	before := []string{"README.md"}
	afterOK := []string{"README.md", "skills/new-skill/SKILL.md"}
	if err := VerifyAllowedWritePaths(before, afterOK, []string{"skills/"}); err != nil {
		t.Fatalf("expected skills/ change to pass: %v", err)
	}

	afterBad := []string{"README.md", "skills/new-skill/SKILL.md", "harness/internal/core/engine.go"}
	if err := VerifyAllowedWritePaths(before, afterBad, []string{"skills/"}); !errors.Is(err, ErrWriteBoundaryViolation) {
		t.Fatalf("expected ErrWriteBoundaryViolation for harness/ edit, got %v", err)
	}
}

func TestExtractJSONObject(t *testing.T) {
	cases := []string{
		`{"result":"PASS","critical_count":0}`,
		"Here is the review:\n```json\n{\"result\":\"FAIL\",\"critical_count\":1}\n```\nDone.",
		`Warning: something logged first {"result":"PASS","notes":"brace } inside string"}`,
	}
	for i, tc := range cases {
		got, err := ExtractJSONObject(tc)
		if err != nil {
			t.Fatalf("case %d failed: %v", i, err)
		}
		if !strings.Contains(got, `"result"`) {
			t.Fatalf("case %d unexpected json: %s", i, got)
		}
	}
}

func TestBuildPromptWithSkills(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "gate-blueprint-check")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("# Gate 0 Rule\nMust verify open decisions."), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt, err := BuildPromptWithSkills(Request{
		Prompt:       "Review blueprint",
		SystemPrompt: "Act as architect",
		SkillFiles:   []string{skillPath},
		JSONSchema:   `{"type":"object"}`,
	})
	if err != nil {
		t.Fatalf("BuildPromptWithSkills failed: %v", err)
	}
	for _, want := range []string{"Gate 0 Rule", "Review blueprint", "Mandatory Structured Output"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q", want)
		}
	}
}

func TestGeminiAdapterFlagsEnvErrorAndSessionFallback(t *testing.T) {
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args.log")
	stubPath := filepath.Join(dir, "fake-gemini.sh")

	// Stub mimics real Gemini CLI 0.22.5 JSON envelopes:
	// - rejects --approval-mode plan
	// - fails if GOOGLE_CLOUD_PROJECT is missing
	// - fails once on stale -r session so fallback retry is verified
	script := `#!/bin/sh
echo "$@" >> "` + argsLog + `"
for arg in "$@"; do
  if [ "$arg" = "plan" ]; then
    echo 'Invalid values: approval-mode plan' >&2
    exit 1
  fi
done
if [ -z "$GOOGLE_CLOUD_PROJECT" ]; then
  echo '{"session_id":"s-err","error":{"type":"ProjectIdRequiredError","message":"set GOOGLE_CLOUD_PROJECT","code":41}}'
  exit 41
fi
case "$*" in
  *"-r stale-sess"*)
    echo '{"session_id":"stale-sess","error":{"type":"SessionNotFound","message":"session not found","code":1}}'
    exit 1
    ;;
esac
echo '{"session_id":"real-sess-99","response":"PONG from stub"}'
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := &GeminiAdapter{Binary: stubPath}
	ctx := context.Background()

	// 1. Missing GOOGLE_CLOUD_PROJECT returns structured ProjectIdRequiredError message.
	_, err := adapter.Run(ctx, Request{
		WorkDir: dir,
		Prompt:  "ping",
		Mode:    ModeReadOnly,
		Timeout: 5 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "ProjectIdRequiredError") {
		t.Fatalf("expected ProjectIdRequiredError, got %v", err)
	}

	// 2. With GOOGLE_CLOUD_PROJECT and stale session, retries cleanly without -r and succeeds using --approval-mode default.
	resp, err := adapter.Run(ctx, Request{
		WorkDir:   dir,
		Prompt:    "ping",
		Mode:      ModeReadOnly,
		SessionID: "stale-sess",
		ExtraEnv:  []string{"GOOGLE_CLOUD_PROJECT=my-sub-project"},
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected fallback retry to succeed, got error: %v", err)
	}
	if resp.TextOutput != "PONG from stub" || resp.SessionID != "real-sess-99" {
		t.Fatalf("unexpected gemini response: %+v", resp)
	}

	logged, _ := os.ReadFile(argsLog)
	if !strings.Contains(string(logged), "--approval-mode default") {
		t.Fatalf("expected --approval-mode default in args, got:\n%s", string(logged))
	}
}

func TestCodexAdapterFlagsAuthHomeAndPendingProvider(t *testing.T) {
	dir := t.TempDir()
	authHome := filepath.Join(dir, "auth-codex")
	_ = os.MkdirAll(authHome, 0o700)
	_ = os.WriteFile(filepath.Join(authHome, "auth.json"), []byte(`{"tokens":{}}`), 0o600)

	argsLog := filepath.Join(dir, "codex-args.log")
	envLog := filepath.Join(dir, "codex-env.log")
	stubPath := filepath.Join(dir, "fake-codex.sh")

	script := `#!/bin/sh
echo "$@" > "` + argsLog + `"
echo "$CODEX_HOME" > "` + envLog + `"
OUT=""
PREV=""
for arg in "$@"; do
  if [ "$PREV" = "-o" ]; then
    OUT="$arg"
  fi
  PREV="$arg"
done
echo '{"type":"thread.started","thread_id":"thr-luna-1"}'
if [ -n "$OUT" ]; then
  printf '%s' '{"needs_orchestration":true,"normalized_intent":"test"}' > "$OUT"
fi
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := &CodexAdapter{Binary: stubPath, AuthHome: authHome}
	resp, err := adapter.Run(context.Background(), Request{
		WorkDir:    dir,
		Prompt:     "hello",
		Mode:       ModeReadOnly,
		Model:      "gpt-6-luna",
		JSONSchema: `{"type":"object"}`,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("CodexAdapter.Run failed: %v", err)
	}
	if resp.SessionID != "thr-luna-1" {
		t.Fatalf("expected session/thread id thr-luna-1, got %q", resp.SessionID)
	}
	if !strings.Contains(resp.Structured, `"needs_orchestration":true`) {
		t.Fatalf("expected structured JSON extracted, got %q", resp.Structured)
	}
	loggedArgs, _ := os.ReadFile(argsLog)
	if !strings.Contains(string(loggedArgs), "-m gpt-6-luna") || !strings.Contains(string(loggedArgs), "-s read-only") || !strings.Contains(string(loggedArgs), "--output-schema") {
		t.Fatalf("unexpected codex args: %s", string(loggedArgs))
	}
	loggedEnv, _ := os.ReadFile(envLog)
	if strings.TrimSpace(string(loggedEnv)) != authHome {
		t.Fatalf("expected CODEX_HOME=%q, got %q", authHome, strings.TrimSpace(string(loggedEnv)))
	}

	// PendingProviderAdapter returns clear not-connected error
	pending := NewPendingProviderAdapter("openai", "connect API later")
	if _, err := pending.Run(context.Background(), Request{Model: "gpt-6-luna"}); err == nil || !strings.Contains(err.Error(), "not connected yet") {
		t.Fatalf("expected not connected error from PendingProviderAdapter, got %v", err)
	}
}
