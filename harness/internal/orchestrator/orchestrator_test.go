package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/executor"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/store"
)

func TestOrchestratorDiscoversContextCalculatesRemainingWorkAndDispatches(t *testing.T) {
	root := t.TempDir()
	centralDir := filepath.Join(root, "dev-automation")
	skillDir := filepath.Join(centralDir, "skills", "design-feature-spec")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: design-feature-spec
description: "[Stage 1] Decompose feature specs"
metadata:
  status: official
  stages: [stage-1]
  scopes: [core]
---
# Body
`), 0o644)

	cfg := config.DefaultHarnessConfig()
	cfg.RuntimeDir = filepath.Join(root, "rt")
	cfg.CentralRepoDir = centralDir

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	proj, err := st.UpsertProject(ctx, store.Project{
		ID:          "p-1",
		Name:        "app-factory",
		RootPath:    filepath.Join(root, "app-factory"),
		RepoLockKey: "app-factory",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.BindChat(ctx, "telegram", "-100", proj.ID); err != nil {
		t.Fatal(err)
	}

	var capturedReq executor.Request
	anthropicMock := executor.NewMockAdapter("anthropic-api", func(_ context.Context, req executor.Request) (*executor.Response, error) {
		capturedReq = req
		// Deliberately return tasks unsorted and wrong remaining_count to verify deterministic normalization.
		return &executor.Response{
			Adapter: "anthropic-api",
			Structured: `{
				"action": "DISPATCH_TASKS",
				"summary": "Split auth work into 3 ordered steps",
				"selected_node": "app-factory",
				"selected_skills": ["design-feature-spec"],
				"tasks": [
					{"id": "T-3", "title": "Write tests", "priority": 2, "order": 3, "status": "PENDING"},
					{"id": "T-1", "title": "Inspect schema", "priority": 1, "order": 1, "status": "DONE"},
					{"id": "T-2", "title": "Implement API", "priority": 1, "order": 2, "status": "PENDING"}
				],
				"remaining_count": 99
			}`,
		}, nil
	})

	ctxReg := NewLocalContextRegistry(st, cfg.SkillsDir())
	svc := NewService(cfg, executor.NewRegistry(anthropicMock), ctxReg, nil)

	res, err := svc.HandleHandoff(ctx, HandoffRequest{
		Channel:          "telegram",
		ChatID:           "-100",
		UserID:           "tg:1",
		RawText:          "로그인 기능 구현 계획 짜줘",
		NormalizedIntent: "Plan and prioritize login feature implementation",
	})
	if err != nil {
		t.Fatalf("HandleHandoff failed: %v", err)
	}

	if capturedReq.Model != "claude-haiku-5-5" {
		t.Fatalf("expected orchestrator model claude-haiku-5-5, got %q", capturedReq.Model)
	}
	if !strings.Contains(capturedReq.Prompt, "design-feature-spec") || !strings.Contains(capturedReq.Prompt, "app-factory") {
		t.Fatalf("expected prompt to include discovered skill and node context, got:\n%s", capturedReq.Prompt)
	}
	if res.AvailableNodes != 1 || res.AvailableSkills != 1 {
		t.Fatalf("expected 1 node and 1 skill discovered, got nodes=%d skills=%d", res.AvailableNodes, res.AvailableSkills)
	}
	if res.Backlog.TotalCount != 3 || res.Backlog.CompletedCount != 1 || res.Backlog.RemainingCount != 2 {
		t.Fatalf("unexpected backlog counts: %+v", res.Backlog)
	}
	if res.Backlog.NextTask == nil || res.Backlog.NextTask.ID != "T-2" {
		t.Fatalf("expected next task T-2, got %+v", res.Backlog.NextTask)
	}
	if len(res.Dispatches) != 1 || res.Dispatches[0].TaskID != "T-2" || res.Dispatches[0].Status != "QUEUED" {
		t.Fatalf("expected T-2 dispatched as QUEUED, got %+v", res.Dispatches)
	}
}
