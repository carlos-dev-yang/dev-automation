package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/executor"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/orchestrator"
)

func TestResponderSeparatesChatModelAndOrchestratorModel(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultHarnessConfig()
	cfg.RuntimeDir = filepath.Join(root, "rt")
	cfg.CentralRepoDir = filepath.Join(root, "dev-automation")

	var chatModels []string
	var orchModels []string

	codexMock := executor.NewMockAdapter("codex", func(_ context.Context, req executor.Request) (*executor.Response, error) {
		chatModels = append(chatModels, req.Model)
		if strings.Contains(req.SystemPrompt, "presenting an internal Orchestrator result") {
			return &executor.Response{
				Adapter:    "codex",
				SessionID:  "luna-sess-2",
				TextOutput: "작업 2건을 정리했고 남은 작업은 2건입니다. 1번 작업부터 진행할까요?",
			}, nil
		}
		if strings.Contains(req.Prompt, "안녕") {
			return &executor.Response{
				Adapter:    "codex",
				SessionID:  "luna-sess-1",
				Structured: `{"needs_orchestration":false,"normalized_intent":"greeting","direct_reply":"안녕하세요! 무엇을 도와드릴까요?"}`,
			}, nil
		}
		return &executor.Response{
			Adapter:    "codex",
			SessionID:  "luna-sess-2",
			Structured: `{"needs_orchestration":true,"normalized_intent":"Prioritize and run pending app tasks","project_hint":"app-factory"}`,
		}, nil
	})

	anthropicMock := executor.NewMockAdapter("anthropic-api", func(_ context.Context, req executor.Request) (*executor.Response, error) {
		orchModels = append(orchModels, req.Model)
		return &executor.Response{
			Adapter: "anthropic-api",
			Structured: `{
				"action": "DISPATCH_TASKS",
				"summary": "Ordered 2 tasks for app-factory",
				"selected_node": "app-factory",
				"selected_skills": ["design-feature-spec"],
				"tasks": [
					{"id": "T-1", "title": "Task A", "priority": 1, "order": 1, "status": "PENDING"},
					{"id": "T-2", "title": "Task B", "priority": 2, "order": 2, "status": "PENDING"}
				],
				"remaining_count": 2,
				"next_task_id": "T-1"
			}`,
		}, nil
	})

	reg := executor.NewRegistry(codexMock, anthropicMock)
	orch := orchestrator.NewService(cfg, reg, nil, nil)
	responder := NewResponder(cfg, reg, orch)

	ctx := context.Background()

	// 1. Trivial greeting stays on ChatResponder (gpt-6-luna) without waking Orchestrator.
	greetResp, err := responder.HandleChat(ctx, ChatRequest{
		Channel: "telegram",
		ChatID:  "-100",
		UserID:  "u1",
		Text:    "안녕",
	})
	if err != nil {
		t.Fatalf("greeting HandleChat failed: %v", err)
	}
	if greetResp.Orchestrated || greetResp.Text != "안녕하세요! 무엇을 도와드릴까요?" {
		t.Fatalf("unexpected direct greeting reply: %+v", greetResp)
	}
	if len(orchModels) != 0 {
		t.Fatalf("expected orchestrator not invoked for greeting, got %v", orchModels)
	}

	// 2. Actionable request routes: ChatResponder(gpt-6-luna) -> Orchestrator(claude-haiku) -> ChatResponder(gpt-6-luna).
	workResp, err := responder.HandleChat(ctx, ChatRequest{
		Channel: "telegram",
		ChatID:  "-100",
		UserID:  "u1",
		Text:    "app-factory 남은 작업 정리해서 우선순위대로 진행해줘",
	})
	if err != nil {
		t.Fatalf("work HandleChat failed: %v", err)
	}
	if !workResp.Orchestrated || workResp.Orchestration == nil {
		t.Fatalf("expected orchestrated response, got %+v", workResp)
	}
	if workResp.Orchestration.Backlog.RemainingCount != 2 {
		t.Fatalf("expected remaining_count=2, got %d", workResp.Orchestration.Backlog.RemainingCount)
	}
	if len(orchModels) != 1 || orchModels[0] != "claude-haiku-5-5" {
		t.Fatalf("expected orchestrator invoked once with claude-haiku-5-5, got %v", orchModels)
	}
	if len(chatModels) != 3 { // 1 greeting + 1 intake + 1 presentation
		t.Fatalf("expected 3 chat model calls, got %d (%v)", len(chatModels), chatModels)
	}
	for _, m := range chatModels {
		if m != "gpt-6-luna" {
			t.Fatalf("expected chat responder model gpt-6-luna, got %q", m)
		}
	}
}
