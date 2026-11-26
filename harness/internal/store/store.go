package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	_ "modernc.org/sqlite"
)

type RunState string

const (
	StatePlanning             RunState = "PLANNING"
	StateGate0Checking        RunState = "GATE0_CHECKING"
	StateWaitingGate0Approval RunState = "WAITING_GATE0_APPROVAL"
	StateQueued               RunState = "QUEUED"
	StateImplementing         RunState = "IMPLEMENTING"
	StateEvaluatingParallel   RunState = "EVALUATING_PARALLEL"
	StateRemediating          RunState = "REMEDIATING"
	StateWaitingSkillApproval RunState = "WAITING_SKILL_APPROVAL"
	StateWaitingHuman         RunState = "WAITING_HUMAN"
	StateReadyForRelease      RunState = "READY_FOR_RELEASE"
	StateCompleted            RunState = "COMPLETED"
	StateCancelled            RunState = "CANCELLED"
)

var ErrNotFound = errors.New("not found")
var ErrInvalidTransition = errors.New("invalid state transition")

type Project struct {
	ID          string
	Name        string
	RootPath    string
	RepoLockKey string
	ConfigPath  string
	CreatedAt   time.Time
}

type ChatBinding struct {
	Channel   string
	ChatID    string
	ProjectID string
	CreatedAt time.Time
}

type Run struct {
	ID               string
	ProjectID        string
	Title            string
	State            RunState
	RetryCount       int
	PlannerSessionID string
	CurrentError     string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type StageAttempt struct {
	ID           string
	RunID        string
	Stage        string
	Role         string
	Adapter      string
	SessionID    string
	Status       string
	ExitCode     int
	VerdictJSON  string
	ArtifactPath string
	StartedAt    time.Time
	FinishedAt   *time.Time
}

type RunEvent struct {
	ID          int64
	RunID       string
	Actor       string
	EventType   string
	PayloadJSON string
	CreatedAt   time.Time
}

type Store struct {
	db *sql.DB
}

// Open opens or initializes the SQLite WAL database at dbPath.
func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	// Single connection serializes writers cleanly under WAL without SQLITE_BUSY errors.
	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("exec pragma %q: %w", p, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	root_path TEXT NOT NULL,
	repo_lock_key TEXT NOT NULL,
	config_path TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_bindings (
	channel TEXT NOT NULL,
	chat_id TEXT NOT NULL,
	project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	PRIMARY KEY (channel, chat_id),
	UNIQUE (channel, project_id)
);

CREATE TABLE IF NOT EXISTS runs (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	state TEXT NOT NULL,
	retry_count INTEGER NOT NULL DEFAULT 0,
	planner_session_id TEXT NOT NULL DEFAULT '',
	current_error TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_runs_project_updated ON runs(project_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_state ON runs(state);

CREATE TABLE IF NOT EXISTS stage_attempts (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
	stage TEXT NOT NULL,
	role TEXT NOT NULL,
	adapter TEXT NOT NULL,
	session_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	exit_code INTEGER NOT NULL DEFAULT 0,
	verdict_json TEXT NOT NULL DEFAULT '',
	artifact_path TEXT NOT NULL DEFAULT '',
	started_at TEXT NOT NULL,
	finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_stage_attempts_run ON stage_attempts(run_id, started_at);

CREATE TABLE IF NOT EXISTS run_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
	actor TEXT NOT NULL,
	event_type TEXT NOT NULL,
	payload_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_run_events_run ON run_events(run_id, id);

CREATE TABLE IF NOT EXISTS chat_sessions (
	channel TEXT NOT NULL,
	chat_id TEXT NOT NULL,
	adapter TEXT NOT NULL,
	session_id TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL,
	PRIMARY KEY (channel, chat_id)
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate sqlite schema: %w", err)
	}
	return nil
}

func nowStr() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func (s *Store) UpsertProject(ctx context.Context, p Project) (*Project, error) {
	now := nowStr()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO projects(id, name, root_path, repo_lock_key, config_path, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			root_path = excluded.root_path,
			repo_lock_key = excluded.repo_lock_key,
			config_path = excluded.config_path
	`, p.ID, p.Name, p.RootPath, p.RepoLockKey, p.ConfigPath, now)
	if err != nil {
		return nil, fmt.Errorf("upsert project %q: %w", p.Name, err)
	}
	return s.GetProjectByName(ctx, p.Name)
}

func (s *Store) GetProjectByName(ctx context.Context, name string) (*Project, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, root_path, repo_lock_key, config_path, created_at FROM projects WHERE name = ?`, name)
	return scanProject(row)
}

func (s *Store) GetProjectByID(ctx context.Context, id string) (*Project, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, root_path, repo_lock_key, config_path, created_at FROM projects WHERE id = ?`, id)
	return scanProject(row)
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, root_path, repo_lock_key, config_path, created_at FROM projects ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		var p Project
		var created string
		if err := rows.Scan(&p.ID, &p.Name, &p.RootPath, &p.RepoLockKey, &p.ConfigPath, &created); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTime(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanProject(row *sql.Row) (*Project, error) {
	var p Project
	var created string
	if err := row.Scan(&p.ID, &p.Name, &p.RootPath, &p.RepoLockKey, &p.ConfigPath, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.CreatedAt = parseTime(created)
	return &p, nil
}

// BindChat links a (channel, chat_id) exclusively to a single project (1 Chat = 1 Project).
func (s *Store) BindChat(ctx context.Context, channel, chatID, projectID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM chat_bindings WHERE channel = ? AND (chat_id = ? OR project_id = ?)`, channel, chatID, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO chat_bindings(channel, chat_id, project_id, created_at) VALUES (?, ?, ?, ?)`, channel, chatID, projectID, nowStr()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetBoundProject(ctx context.Context, channel, chatID string) (*Project, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.root_path, p.repo_lock_key, p.config_path, p.created_at
		FROM chat_bindings b
		JOIN projects p ON p.id = b.project_id
		WHERE b.channel = ? AND b.chat_id = ?
	`, channel, chatID)
	return scanProject(row)
}

func (s *Store) GetChatBindingByProject(ctx context.Context, channel, projectID string) (*ChatBinding, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT channel, chat_id, project_id, created_at
		FROM chat_bindings
		WHERE channel = ? AND project_id = ?
	`, channel, projectID)
	var b ChatBinding
	var created string
	if err := row.Scan(&b.Channel, &b.ChatID, &b.ProjectID, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	b.CreatedAt = parseTime(created)
	return &b, nil
}

func (s *Store) ListChatBindingsByProject(ctx context.Context, projectID string) ([]ChatBinding, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT channel, chat_id, project_id, created_at
		FROM chat_bindings
		WHERE project_id = ?
		ORDER BY created_at ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChatBinding
	for rows.Next() {
		var b ChatBinding
		var created string
		if err := rows.Scan(&b.Channel, &b.ChatID, &b.ProjectID, &created); err != nil {
			return nil, err
		}
		b.CreatedAt = parseTime(created)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetChatSession(ctx context.Context, channel, chatID string) (adapter, sessionID string, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT adapter, session_id FROM chat_sessions WHERE channel = ? AND chat_id = ?`, channel, chatID)
	if err := row.Scan(&adapter, &sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	return adapter, sessionID, nil
}

func (s *Store) UpsertChatSession(ctx context.Context, channel, chatID, adapter, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chat_sessions(channel, chat_id, adapter, session_id, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(channel, chat_id) DO UPDATE SET
			adapter = excluded.adapter,
			session_id = excluded.session_id,
			updated_at = excluded.updated_at
	`, channel, chatID, adapter, sessionID, nowStr())
	return err
}

func (s *Store) DeleteChatSession(ctx context.Context, channel, chatID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chat_sessions WHERE channel = ? AND chat_id = ?`, channel, chatID)
	return err
}

func (s *Store) CreateRun(ctx context.Context, r Run) (*Run, error) {
	now := nowStr()
	if r.State == "" {
		r.State = StatePlanning
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO runs(id, project_id, title, state, retry_count, planner_session_id, current_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.ProjectID, r.Title, string(r.State), r.RetryCount, r.PlannerSessionID, r.CurrentError, now, now)
	if err != nil {
		return nil, fmt.Errorf("create run %s: %w", r.ID, err)
	}
	return s.GetRun(ctx, r.ID)
}

func (s *Store) GetRun(ctx context.Context, runID string) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, title, state, retry_count, planner_session_id, current_error, created_at, updated_at
		FROM runs WHERE id = ?
	`, runID)
	return scanRun(row)
}

func (s *Store) GetLatestRunForProject(ctx context.Context, projectID string) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, title, state, retry_count, planner_session_id, current_error, created_at, updated_at
		FROM runs WHERE project_id = ?
		ORDER BY updated_at DESC LIMIT 1
	`, projectID)
	return scanRun(row)
}

func scanRun(row *sql.Row) (*Run, error) {
	var r Run
	var state, created, updated string
	if err := row.Scan(&r.ID, &r.ProjectID, &r.Title, &state, &r.RetryCount, &r.PlannerSessionID, &r.CurrentError, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.State = RunState(state)
	r.CreatedAt = parseTime(created)
	r.UpdatedAt = parseTime(updated)
	return &r, nil
}

// TransitionRunState atomically validates allowed source states, updates the run, and appends an audit event.
func (s *Store) TransitionRunState(ctx context.Context, runID string, allowedFrom []RunState, to RunState, errMsg, actor, eventType string, payload any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var curState string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM runs WHERE id = ?`, runID).Scan(&curState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if len(allowedFrom) > 0 && !slices.Contains(allowedFrom, RunState(curState)) {
		return fmt.Errorf("%w: run %s cannot transition %s -> %s", ErrInvalidTransition, runID, curState, to)
	}

	now := nowStr()
	if _, err := tx.ExecContext(ctx, `UPDATE runs SET state = ?, current_error = ?, updated_at = ? WHERE id = ?`, string(to), errMsg, now, runID); err != nil {
		return err
	}

	payloadJSON := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			payloadJSON = string(b)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO run_events(run_id, actor, event_type, payload_json, created_at) VALUES (?, ?, ?, ?, ?)`, runID, actor, eventType, payloadJSON, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdatePlannerSession(ctx context.Context, runID, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET planner_session_id = ?, updated_at = ? WHERE id = ?`, sessionID, nowStr(), runID)
	return err
}

func (s *Store) IncrementRetryCount(ctx context.Context, runID string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `UPDATE runs SET retry_count = retry_count + 1, updated_at = ? WHERE id = ?`, nowStr(), runID); err != nil {
		return 0, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT retry_count FROM runs WHERE id = ?`, runID).Scan(&count); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) AppendEvent(ctx context.Context, runID, actor, eventType string, payload any) error {
	payloadJSON := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			payloadJSON = string(b)
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO run_events(run_id, actor, event_type, payload_json, created_at) VALUES (?, ?, ?, ?, ?)`, runID, actor, eventType, payloadJSON, nowStr())
	return err
}

func (s *Store) ListEvents(ctx context.Context, runID string) ([]RunEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, actor, event_type, payload_json, created_at FROM run_events WHERE run_id = ? ORDER BY id ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []RunEvent
	for rows.Next() {
		var e RunEvent
		var created string
		if err := rows.Scan(&e.ID, &e.RunID, &e.Actor, &e.EventType, &e.PayloadJSON, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(created)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) RecordStageAttempt(ctx context.Context, a StageAttempt) error {
	var finished sql.NullString
	if a.FinishedAt != nil {
		finished = sql.NullString{String: a.FinishedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO stage_attempts(id, run_id, stage, role, adapter, session_id, status, exit_code, verdict_json, artifact_path, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			session_id = excluded.session_id,
			status = excluded.status,
			exit_code = excluded.exit_code,
			verdict_json = excluded.verdict_json,
			artifact_path = excluded.artifact_path,
			finished_at = excluded.finished_at
	`, a.ID, a.RunID, a.Stage, a.Role, a.Adapter, a.SessionID, a.Status, a.ExitCode, a.VerdictJSON, a.ArtifactPath, a.StartedAt.UTC().Format(time.RFC3339Nano), finished)
	return err
}

func (s *Store) ListStageAttempts(ctx context.Context, runID string) ([]StageAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, stage, role, adapter, session_id, status, exit_code, verdict_json, artifact_path, started_at, finished_at
		FROM stage_attempts WHERE run_id = ? ORDER BY started_at ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StageAttempt
	for rows.Next() {
		var a StageAttempt
		var started string
		var finished sql.NullString
		if err := rows.Scan(&a.ID, &a.RunID, &a.Stage, &a.Role, &a.Adapter, &a.SessionID, &a.Status, &a.ExitCode, &a.VerdictJSON, &a.ArtifactPath, &started, &finished); err != nil {
			return nil, err
		}
		a.StartedAt = parseTime(started)
		if finished.Valid {
			t := parseTime(finished.String)
			a.FinishedAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// HasActiveRunForRepoLock returns true if another run holding the same repo_lock_key is actively mutating/evaluating the repo.
func (s *Store) HasActiveRunForRepoLock(ctx context.Context, repoLockKey, excludeRunID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM runs r
		JOIN projects p ON p.id = r.project_id
		WHERE p.repo_lock_key = ?
		  AND r.id != ?
		  AND r.state IN (?, ?, ?)
	`, repoLockKey, excludeRunID, string(StateImplementing), string(StateEvaluatingParallel), string(StateRemediating)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// NextQueuedRunForRepoLock returns the oldest QUEUED run waiting on repoLockKey.
func (s *Store) NextQueuedRunForRepoLock(ctx context.Context, repoLockKey string) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT r.id, r.project_id, r.title, r.state, r.retry_count, r.planner_session_id, r.current_error, r.created_at, r.updated_at
		FROM runs r
		JOIN projects p ON p.id = r.project_id
		WHERE p.repo_lock_key = ? AND r.state = ?
		ORDER BY r.updated_at ASC
		LIMIT 1
	`, repoLockKey, string(StateQueued))
	return scanRun(row)
}

// RecoverOrphanedRuns moves any in-flight runs left behind by a daemon crash into WAITING_HUMAN safely.
func (s *Store) RecoverOrphanedRuns(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, title, state, retry_count, planner_session_id, current_error, created_at, updated_at
		FROM runs
		WHERE state IN (?, ?, ?, ?)
	`, string(StateGate0Checking), string(StateImplementing), string(StateEvaluatingParallel), string(StateRemediating))
	if err != nil {
		return nil, err
	}
	var orphaned []Run
	for rows.Next() {
		var r Run
		var state, created, updated string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Title, &state, &r.RetryCount, &r.PlannerSessionID, &r.CurrentError, &created, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		r.State = RunState(state)
		r.CreatedAt = parseTime(created)
		r.UpdatedAt = parseTime(updated)
		orphaned = append(orphaned, r)
	}
	rows.Close()

	recovered := make([]Run, 0, len(orphaned))
	for _, r := range orphaned {
		msg := fmt.Sprintf("daemon restarted while run was in state %s; awaiting human review (/retry or /abort)", r.State)
		if err := s.TransitionRunState(ctx, r.ID, []RunState{r.State}, StateWaitingHuman, msg, "system", "daemon_crash_recovery", map[string]string{"previous_state": string(r.State)}); err != nil {
			return recovered, err
		}
		r.State = StateWaitingHuman
		r.CurrentError = msg
		recovered = append(recovered, r)
	}
	return recovered, nil
}
