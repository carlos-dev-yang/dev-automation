package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestStoreStateTransitionsAndLocking(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	p, err := s.UpsertProject(ctx, Project{
		ID:          "proj-1",
		Name:        "app-factory",
		RootPath:    "/tmp/app-factory",
		RepoLockKey: "app-factory",
	})
	if err != nil {
		t.Fatalf("UpsertProject failed: %v", err)
	}

	if err := s.BindChat(ctx, "telegram", "chat-100", p.ID); err != nil {
		t.Fatalf("BindChat failed: %v", err)
	}
	bound, err := s.GetBoundProject(ctx, "telegram", "chat-100")
	if err != nil || bound.ID != p.ID {
		t.Fatalf("GetBoundProject mismatch: %+v, err=%v", bound, err)
	}

	r1, err := s.CreateRun(ctx, Run{ID: "run-1", ProjectID: p.ID, Title: "Feature A"})
	if err != nil || r1.State != StatePlanning {
		t.Fatalf("CreateRun failed: %+v, err=%v", r1, err)
	}

	// TC-01: Illegal direct transition PLANNING -> IMPLEMENTING must fail
	if err := s.TransitionRunState(ctx, r1.ID, []RunState{StateQueued}, StateImplementing, "", "system", "start", nil); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}

	// Valid transition chain
	if err := s.TransitionRunState(ctx, r1.ID, []RunState{StatePlanning}, StateImplementing, "", "system", "test_active", nil); err != nil {
		t.Fatalf("transition failed: %v", err)
	}

	// TC-07: Second run on same repo_lock_key sees active lock
	r2, err := s.CreateRun(ctx, Run{ID: "run-2", ProjectID: p.ID, Title: "Feature B", State: StateQueued})
	if err != nil {
		t.Fatalf("CreateRun r2 failed: %v", err)
	}
	locked, err := s.HasActiveRunForRepoLock(ctx, p.RepoLockKey, r2.ID)
	if err != nil || !locked {
		t.Fatalf("expected active repo lock, got locked=%v err=%v", locked, err)
	}
	next, err := s.NextQueuedRunForRepoLock(ctx, p.RepoLockKey)
	if err != nil || next.ID != r2.ID {
		t.Fatalf("expected queued run-2, got %+v err=%v", next, err)
	}

	// TC-09: Crash recovery moves active in-flight run to WAITING_HUMAN
	recovered, err := s.RecoverOrphanedRuns(ctx)
	if err != nil || len(recovered) != 1 || recovered[0].ID != r1.ID || recovered[0].State != StateWaitingHuman {
		t.Fatalf("expected 1 recovered run-1 in WAITING_HUMAN, got %+v err=%v", recovered, err)
	}
}
