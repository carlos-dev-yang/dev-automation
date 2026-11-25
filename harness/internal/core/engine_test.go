package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/executor"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/store"
)

func initTempGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func setupTestEnv(t *testing.T, claudeHandler, geminiHandler func(context.Context, executor.Request) (*executor.Response, error)) (*Engine, *store.Store, *config.HarnessConfig, string, string) {
	t.Helper()
	root := t.TempDir()
	centralDir := filepath.Join(root, "dev-automation")
	projDir := filepath.Join(root, "sample-proj")
	runtimeDir := filepath.Join(root, "runtime")

	initTempGitRepo(t, centralDir)
	if err := os.MkdirAll(filepath.Join(centralDir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	initTempGitRepo(t, projDir)

	projYAML := `project_id: sample-proj
repo_lock_key: sample-proj
commands:
  status: "git status -sb"
  static_check: "echo ok"
  test: "echo ok"
  build_check: "echo ok"
`
	if err := os.WriteFile(filepath.Join(projDir, "rd-project.yaml"), []byte(projYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, projDir, "add", "rd-project.yaml")
	runGit(t, projDir, "commit", "-m", "add rd-project.yaml")

	cfg := config.DefaultHarnessConfig()
	cfg.RuntimeDir = runtimeDir
	cfg.CentralRepoDir = centralDir
	cfg.MaxRetries = 2
	cfg.StepTimeoutSeconds = 30

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	reg := executor.NewRegistry(
		executor.NewMockAdapter("claude", claudeHandler),
		executor.NewMockAdapter("gemini", geminiHandler),
	)
	eng := NewEngine(cfg, st, reg, nil)
	eng.AsyncDispatch = false
	return eng, st, cfg, centralDir, projDir
}

// TC-01, TC-03, TC-10: Full Stage 1 -> Gate 0 -> Approve -> Stage 2 -> Parallel Stage 3 (PASS WITH ADVICE) -> Release
func TestFullPipelineHappyPathAndMediumAdvicePasses(t *testing.T) {
	ctx := context.Background()

	claude := func(_ context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "Stage 2") {
			return &executor.Response{
				Adapter:    "claude",
				Structured: `{"status":"READY_FOR_STAGE_3","summary":"Implemented slice 1","changed_files":["a.go"],"compliance_evidence":["impl-autonomous-slice -> a.go#L1-L10"]}`,
			}, nil
		}
		// Golden Set A (separate fresh session)
		if req.SessionID != "" {
			t.Errorf("Golden Set A must run in a fresh isolated session, got %q", req.SessionID)
		}
		return &executor.Response{
			Adapter:    "claude",
			Structured: `{"result":"PASS","summary":"All checks green","failures":[],"unverified_scope":["e2e"]}`,
		}, nil
	}

	gemini := func(_ context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "Stage 1 Planner") {
			return &executor.Response{Adapter: "gemini", SessionID: "sess-plan-1", TextOutput: "Drafted blueprint."}, nil
		}
		if strings.Contains(req.SystemPrompt, "Gate 0") {
			return &executor.Response{
				Adapter:    "gemini",
				Structured: `{"result":"PASS","summary":"Ready","open_decisions":[],"roadmap":["Slice 1"],"acceptance_tcs":["TC-1"]}`,
			}, nil
		}
		// Golden Set B: CRITICAL=0, MEDIUM=2 -> must PASS WITH ADVICE (TC-03)
		return &executor.Response{
			Adapter:    "gemini",
			Structured: `{"result":"PASS_WITH_ADVICE","critical_count":0,"medium_count":2,"low_count":1,"findings":["[MEDIUM] minor cleanup"],"skill_compliance_notes":["good"]}`,
		}, nil
	}

	eng, st, _, _, _ := setupTestEnv(t, claude, gemini)
	msg := func(txt string) IncomingMessage { return IncomingMessage{Channel: "local", ChatID: "c1", UserID: "u1", Text: txt} }

	if _, err := eng.HandleMessage(ctx, msg("/bind sample-proj")); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.HandleMessage(ctx, msg("/new Add health endpoint")); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.HandleMessage(ctx, msg("Need /health returning 200 OK")); err != nil {
		t.Fatal(err)
	}

	// TC-01: ExecuteRun before Gate 0 approval must fail
	proj, _ := st.GetBoundProject(ctx, "local", "c1")
	run, _ := st.GetLatestRunForProject(ctx, proj.ID)
	if err := eng.ExecuteRun(ctx, run.ID); !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition before Gate 0 approval, got %v", err)
	}

	if _, err := eng.HandleMessage(ctx, msg("/gate0")); err != nil {
		t.Fatal(err)
	}
	run, _ = st.GetLatestRunForProject(ctx, proj.ID)
	if run.State != store.StateWaitingGate0Approval {
		t.Fatalf("expected WAITING_GATE0_APPROVAL, got %s", run.State)
	}

	if _, err := eng.HandleMessage(ctx, msg("/approve")); err != nil {
		t.Fatal(err)
	}
	if err := eng.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("ExecuteRun failed: %v", err)
	}
	run, _ = st.GetRun(ctx, run.ID)
	if run.State != store.StateReadyForRelease {
		t.Fatalf("expected READY_FOR_RELEASE with CRITICAL=0/MEDIUM=2, got %s", run.State)
	}

	if _, err := eng.HandleMessage(ctx, msg("/approve")); err != nil {
		t.Fatal(err)
	}
	run, _ = st.GetRun(ctx, run.ID)
	if run.State != store.StateCompleted {
		t.Fatalf("expected COMPLETED, got %s", run.State)
	}
}

// TC-02 & TC-04: Parallel Fan-in Barrier + CRITICAL>=1 Failure -> Type S SKILL CORRECTION FIRST Lock
func TestParallelFanInAndSkillCorrectionFirstLock(t *testing.T) {
	ctx := context.Background()
	var bFinished atomic.Bool
	var remSawBFinished atomic.Bool

	claude := func(_ context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "Stage 2") {
			return &executor.Response{
				Adapter:    "claude",
				Structured: `{"status":"READY_FOR_STAGE_3","summary":"Done","changed_files":["x.go"],"compliance_evidence":["ok"]}`,
			}, nil
		}
		// Golden Set A fails immediately, BEFORE Golden Set B finishes
		return &executor.Response{
			Adapter:    "claude",
			Structured: `{"result":"FAIL","summary":"Unit test failed","failures":["TC-1 failed"],"unverified_scope":[]}`,
		}, nil
	}

	gemini := func(_ context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "Gate 0") {
			return &executor.Response{
				Adapter:    "gemini",
				Structured: `{"result":"PASS","summary":"OK","open_decisions":[],"roadmap":["S1"],"acceptance_tcs":["TC-1"]}`,
			}, nil
		}
		if strings.Contains(req.SystemPrompt, "Golden Set B") {
			time.Sleep(120 * time.Millisecond) // slower than A to verify Fan-in waits for B
			bFinished.Store(true)
			return &executor.Response{
				Adapter:    "gemini",
				Structured: `{"result":"FAIL","critical_count":1,"medium_count":0,"low_count":0,"findings":["[CRITICAL] missing skill rule"],"skill_compliance_notes":["gap"]}`,
			}, nil
		}
		if strings.Contains(req.SystemPrompt, "Remediation Director") {
			remSawBFinished.Store(bFinished.Load())
			return &executor.Response{
				Adapter:    "gemini",
				Structured: `{"sequence":"SKILL_CORRECTION_FIRST","summary":"Type S gap","root_causes":["RC-1 Type S"],"skill_correction_plan":"Add rule to skills/demo/SKILL.md","code_directive":"Fix after skill update","anti_hacking_constraints":["No hardcoding"]}`,
			}, nil
		}
		// ops-skill-evolution modifies ONLY skills/
		skillPath := filepath.Join(req.WorkDir, "skills", "draft-fix", "SKILL.md")
		_ = os.MkdirAll(filepath.Dir(skillPath), 0o755)
		_ = os.WriteFile(skillPath, []byte("---\nname: draft-fix\ndescription: \"[DRAFT] fix\"\nmetadata:\n  status: draft\n---\n"), 0o644)
		return &executor.Response{
			Adapter:    "gemini",
			Structured: `{"action":"NEW_DRAFT","summary":"Added draft skill","target_skills":["draft-fix"]}`,
		}, nil
	}

	eng, st, _, _, _ := setupTestEnv(t, claude, gemini)
	msg := func(txt string) IncomingMessage { return IncomingMessage{Channel: "local", ChatID: "c1", UserID: "u1", Text: txt} }
	_, _ = eng.HandleMessage(ctx, msg("/bind sample-proj"))
	_, _ = eng.HandleMessage(ctx, msg("/new Feature"))
	_, _ = eng.HandleMessage(ctx, msg("/gate0"))
	_, _ = eng.HandleMessage(ctx, msg("/approve"))

	proj, _ := st.GetBoundProject(ctx, "local", "c1")
	run, _ := st.GetLatestRunForProject(ctx, proj.ID)

	if err := eng.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("ExecuteRun failed: %v", err)
	}
	if !remSawBFinished.Load() {
		t.Fatalf("TC-02 violation: Stage 4 Remediation ran before Golden Set B finished Fan-in!")
	}
	run, _ = st.GetRun(ctx, run.ID)
	if run.State != store.StateWaitingSkillApproval {
		t.Fatalf("TC-04 violation: expected WAITING_SKILL_APPROVAL, got %s", run.State)
	}
}

// TC-05: Golden Set B mutating a file in target repo must be caught by Read-Only dual guard.
func TestReadOnlyGuardCatchesMutationInStage3(t *testing.T) {
	ctx := context.Background()

	claude := func(_ context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "Stage 2") {
			return &executor.Response{Adapter: "claude", Structured: `{"status":"READY_FOR_STAGE_3","summary":"ok","changed_files":[],"compliance_evidence":[]}`}, nil
		}
		return &executor.Response{Adapter: "claude", Structured: `{"result":"PASS","summary":"ok","failures":[],"unverified_scope":[]}`}, nil
	}
	gemini := func(_ context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "Gate 0") {
			return &executor.Response{Adapter: "gemini", Structured: `{"result":"PASS","summary":"ok","open_decisions":[],"roadmap":["S1"],"acceptance_tcs":["TC-1"]}`}, nil
		}
		// Misbehaving Golden Set B mutates a file during Read-Only review!
		_ = os.WriteFile(filepath.Join(req.WorkDir, "sneaky-edit.txt"), []byte("hack"), 0o644)
		return &executor.Response{Adapter: "gemini", Structured: `{"result":"PASS","critical_count":0,"medium_count":0,"low_count":0,"findings":[],"skill_compliance_notes":[]}`}, nil
	}

	eng, st, _, _, _ := setupTestEnv(t, claude, gemini)
	msg := func(txt string) IncomingMessage { return IncomingMessage{Channel: "local", ChatID: "c1", UserID: "u1", Text: txt} }
	_, _ = eng.HandleMessage(ctx, msg("/bind sample-proj"))
	_, _ = eng.HandleMessage(ctx, msg("/new Feature"))
	_, _ = eng.HandleMessage(ctx, msg("/gate0"))
	_, _ = eng.HandleMessage(ctx, msg("/approve"))
	proj, _ := st.GetBoundProject(ctx, "local", "c1")
	run, _ := st.GetLatestRunForProject(ctx, proj.ID)

	err := eng.ExecuteRun(ctx, run.ID)
	if !errors.Is(err, executor.ErrReadOnlyViolation) {
		t.Fatalf("TC-05 violation: expected ErrReadOnlyViolation, got %v", err)
	}
	run, _ = st.GetRun(ctx, run.ID)
	if run.State != store.StateWaitingHuman {
		t.Fatalf("expected WAITING_HUMAN after ReadOnly violation, got %s", run.State)
	}
}

// TC-06: Stage 4 ops-skill-evolution attempting to modify harness/ must be blocked (Meta-Hacking Guard).
func TestSkillEvolutionMetaHackingBlocked(t *testing.T) {
	ctx := context.Background()

	claude := func(_ context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "Stage 2") {
			return &executor.Response{Adapter: "claude", Structured: `{"status":"READY_FOR_STAGE_3","summary":"ok","changed_files":[],"compliance_evidence":[]}`}, nil
		}
		return &executor.Response{Adapter: "claude", Structured: `{"result":"FAIL","summary":"fail","failures":["err"],"unverified_scope":[]}`}, nil
	}
	gemini := func(_ context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "Gate 0") {
			return &executor.Response{Adapter: "gemini", Structured: `{"result":"PASS","summary":"ok","open_decisions":[],"roadmap":["S1"],"acceptance_tcs":["TC-1"]}`}, nil
		}
		if strings.Contains(req.SystemPrompt, "Golden Set B") {
			return &executor.Response{Adapter: "gemini", Structured: `{"result":"FAIL","critical_count":1,"medium_count":0,"low_count":0,"findings":["crit"],"skill_compliance_notes":[]}`}, nil
		}
		if strings.Contains(req.SystemPrompt, "Remediation Director") {
			return &executor.Response{Adapter: "gemini", Structured: `{"sequence":"SKILL_CORRECTION_FIRST","summary":"Type S","root_causes":["RC-1"],"skill_correction_plan":"fix","code_directive":"fix","anti_hacking_constraints":[]}`}, nil
		}
		// Maliciously modifies harness/ instead of skills/!
		badFile := filepath.Join(req.WorkDir, "harness", "tamper.go")
		_ = os.MkdirAll(filepath.Dir(badFile), 0o755)
		_ = os.WriteFile(badFile, []byte("package evil"), 0o644)
		return &executor.Response{Adapter: "gemini", Structured: `{"action":"CLEAN_REWRITE","summary":"tampered","target_skills":["x"]}`}, nil
	}

	eng, st, _, _, _ := setupTestEnv(t, claude, gemini)
	msg := func(txt string) IncomingMessage { return IncomingMessage{Channel: "local", ChatID: "c1", UserID: "u1", Text: txt} }
	_, _ = eng.HandleMessage(ctx, msg("/bind sample-proj"))
	_, _ = eng.HandleMessage(ctx, msg("/new Feature"))
	_, _ = eng.HandleMessage(ctx, msg("/gate0"))
	_, _ = eng.HandleMessage(ctx, msg("/approve"))
	proj, _ := st.GetBoundProject(ctx, "local", "c1")
	run, _ := st.GetLatestRunForProject(ctx, proj.ID)

	err := eng.ExecuteRun(ctx, run.ID)
	if !errors.Is(err, executor.ErrWriteBoundaryViolation) {
		t.Fatalf("TC-06 violation: expected ErrWriteBoundaryViolation, got %v", err)
	}
	run, _ = st.GetRun(ctx, run.ID)
	if run.State != store.StateWaitingHuman {
		t.Fatalf("expected WAITING_HUMAN after Meta-Hacking block, got %s", run.State)
	}
}
