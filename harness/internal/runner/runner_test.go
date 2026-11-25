package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
)

func TestRunShellPassFailAndTimeout(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	pass := RunShell(ctx, dir, "ok", "echo hello", 5*time.Second)
	if pass.Status != "PASS" || pass.ExitCode != 0 {
		t.Fatalf("expected PASS, got %+v", pass)
	}

	fail := RunShell(ctx, dir, "bad", "exit 7", 5*time.Second)
	if fail.Status != "FAIL" || fail.ExitCode != 7 {
		t.Fatalf("expected FAIL exit 7, got %+v", fail)
	}

	timedOut := RunShell(ctx, dir, "slow", "sleep 5", 150*time.Millisecond)
	if timedOut.Status != "FAIL" || !timedOut.TimedOut {
		t.Fatalf("expected timeout FAIL, got %+v", timedOut)
	}
}

func TestRunChecksWithOpenUnverified(t *testing.T) {
	dir := t.TempDir()
	pc := &config.ProjectConfig{
		ProjectID: "demo",
		Commands: map[string]string{
			"static_check": "echo lint-ok",
			"test":         "echo test-ok",
		},
	}
	rep := RunChecks(context.Background(), dir, pc, []string{"static_check", "test", "e2e"}, 5*time.Second)
	if !rep.AllPassed {
		t.Fatalf("expected AllPassed=true when configured checks pass and e2e is OPEN_UNVERIFIED: %+v", rep)
	}
	if len(rep.Results) != 3 || rep.Results[2].Status != "OPEN_UNVERIFIED" {
		t.Fatalf("expected e2e OPEN_UNVERIFIED, got %+v", rep.Results)
	}
}

func TestEnsureSkillLinksSiblingGuardAndConflict(t *testing.T) {
	parent := t.TempDir()
	central := filepath.Join(parent, "dev-automation")
	proj := filepath.Join(parent, "app-factory")
	if err := os.MkdirAll(filepath.Join(central, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	// Non-sibling project must be rejected (D-12)
	nested := filepath.Join(parent, "sub", "nested-app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSkillLinks(central, nested, true); !errors.Is(err, ErrLayoutViolation) {
		t.Fatalf("expected ErrLayoutViolation for non-sibling, got %v", err)
	}

	// Existing real dir causes ErrLinkConflict
	if err := os.MkdirAll(filepath.Join(proj, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSkillLinks(central, proj, false); !errors.Is(err, ErrLinkConflict) {
		t.Fatalf("expected ErrLinkConflict, got %v", err)
	}

	// Removing real dir allows clean symlink creation -> ../../dev-automation/skills
	if err := os.RemoveAll(filepath.Join(proj, ".agents", "skills")); err != nil {
		t.Fatal(err)
	}
	statuses, err := EnsureSkillLinks(central, proj, false)
	if err != nil {
		t.Fatalf("EnsureSkillLinks failed: %v", err)
	}
	for _, st := range statuses {
		if st.Status != "LINKED" || st.Target != filepath.Join("..", "..", "dev-automation", "skills") {
			t.Fatalf("unexpected link status: %+v", st)
		}
	}

	// Idempotent re-run reports OK
	statuses, err = EnsureSkillLinks(central, proj, false)
	if err != nil {
		t.Fatalf("re-run EnsureSkillLinks failed: %v", err)
	}
	for _, st := range statuses {
		if st.Status != "OK" {
			t.Fatalf("expected OK on re-run, got %+v", st)
		}
	}
}
