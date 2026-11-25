package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/executor"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/runner"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/store"
)

type IncomingMessage struct {
	Channel string // "local" | "telegram"
	ChatID  string
	UserID  string
	Text    string
}

type ActionButton struct {
	Label string
	Data  string
}

type OutgoingMessage struct {
	Channel    string
	ChatID     string
	Text       string
	Attachment string
	Actions    []ActionButton
}

type Notifier func(ctx context.Context, msg OutgoingMessage)

// Engine implements the platform-agnostic Chat Core and Stage 1-4 Workflow State Machine (D-09, D-10).
type Engine struct {
	cfg      *config.HarnessConfig
	store    *store.Store
	registry *executor.Registry
	notify   Notifier

	mu           sync.Mutex
	activeCancel map[string]context.CancelFunc

	// AsyncDispatch controls whether /approve launches the autonomous loop in a background goroutine (true in daemon mode)
	// or leaves execution to explicit ExecuteRun calls (useful in deterministic tests).
	AsyncDispatch bool
}

func NewEngine(cfg *config.HarnessConfig, st *store.Store, reg *executor.Registry, notify Notifier) *Engine {
	if notify == nil {
		notify = func(context.Context, OutgoingMessage) {}
	}
	return &Engine{
		cfg:           cfg,
		store:         st,
		registry:      reg,
		notify:        notify,
		activeCancel:  make(map[string]context.CancelFunc),
		AsyncDispatch: true,
	}
}

// RecoverOnStartup moves orphaned in-flight runs to WAITING_HUMAN safely (TC-09).
func (e *Engine) RecoverOnStartup(ctx context.Context) ([]store.Run, error) {
	recovered, err := e.store.RecoverOrphanedRuns(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range recovered {
		e.notifyProjectChannels(ctx, r.ProjectID, fmt.Sprintf("[Recovery] Run %s (%s) was interrupted by daemon restart and moved to WAITING_HUMAN.\nReason: %s", r.ID, r.Title, r.CurrentError), "", nil)
	}
	return recovered, nil
}

// HandleMessage routes chat commands and Stage 1 planning turns from any ChannelGateway.
func (e *Engine) HandleMessage(ctx context.Context, msg IncomingMessage) (OutgoingMessage, error) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: "Empty message. Send /help for commands."}, nil
	}

	if strings.HasPrefix(text, "/") {
		fields := strings.Fields(text)
		cmd := strings.ToLower(strings.Split(fields[0], "@")[0]) // strip @botname suffix from Telegram commands
		arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))

		switch cmd {
		case "/start", "/help":
			return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: helpText()}, nil
		case "/bind":
			return e.handleBind(ctx, msg, arg)
		case "/new":
			return e.handleNew(ctx, msg, arg)
		case "/gate0":
			return e.handleGate0(ctx, msg)
		case "/approve":
			return e.handleApprove(ctx, msg)
		case "/reject":
			return e.handleReject(ctx, msg, arg)
		case "/check":
			return e.handleCheck(ctx, msg, arg)
		case "/status":
			return e.handleStatus(ctx, msg)
		case "/logs":
			return e.handleLogs(ctx, msg)
		case "/retry":
			return e.handleRetry(ctx, msg)
		case "/abort":
			return e.handleAbort(ctx, msg)
		default:
			return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Unknown command %q. Send /help for available commands.", cmd)}, nil
		}
	}

	return e.handlePlanningChat(ctx, msg, text)
}

func helpText() string {
	return strings.Join([]string{
		"rd-harness commands (1 Chat = 1 Project):",
		"  /bind <project-name|path>  Bind this chat to a sibling project & check symlinks",
		"  /new <task title>          Start a new Run in Stage 1 (PLANNING)",
		"  <plain text>               Discuss blueprint with Stage 1 Planner (gemini)",
		"  /gate0                     Run Gate 0 blueprint check",
		"  /approve                   Approve Gate 0, Skill Correction, or Final Release",
		"  /reject <reason>           Reject current approval gate and return to planning",
		"  /check [cmd-key]           Run deterministic No-AI project CLI check",
		"  /status                    Show bound project & latest Run status",
		"  /logs                      Show artifact directory & recent stage attempts",
		"  /retry                     Resume a WAITING_HUMAN Run into Stage 2",
		"  /abort                     Cancel in-flight Run and kill active subprocesses",
	}, "\n")
}

func (e *Engine) handleBind(ctx context.Context, msg IncomingMessage, arg string) (OutgoingMessage, error) {
	if arg == "" {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: "Usage: /bind <sibling-project-name-or-path>"}, nil
	}

	projPath := arg
	if !filepath.IsAbs(projPath) && e.cfg.CentralRepoDir != "" {
		projPath = filepath.Join(filepath.Dir(e.cfg.CentralRepoDir), arg)
	}
	absPath, err := filepath.Abs(projPath)
	if err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Invalid path: %v", err)}, nil
	}
	if st, err := os.Stat(absPath); err != nil || !st.IsDir() {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Project directory not found: %s", absPath)}, nil
	}

	pc, hasConfig, err := config.LoadProjectConfig(absPath)
	if err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Failed to load rd-project.yaml: %v", err)}, nil
	}

	linkNote := "symlink check skipped (central_repo_dir not set)"
	if e.cfg.CentralRepoDir != "" {
		statuses, linkErr := runner.EnsureSkillLinks(e.cfg.CentralRepoDir, absPath, true)
		var lines []string
		for _, s := range statuses {
			lines = append(lines, fmt.Sprintf("- %s: %s (%s)", filepath.Base(filepath.Dir(s.LinkPath))+"/skills", s.Status, s.Message))
		}
		if linkErr != nil {
			linkNote = fmt.Sprintf("WARN (%v)\n%s", linkErr, strings.Join(lines, "\n"))
		} else {
			linkNote = "OK\n" + strings.Join(lines, "\n")
		}
	}

	configPath := ""
	if hasConfig {
		configPath = filepath.Join(absPath, "rd-project.yaml")
	}
	proj, err := e.store.UpsertProject(ctx, store.Project{
		ID:          uuid.NewString(),
		Name:        pc.ProjectID,
		RootPath:    absPath,
		RepoLockKey: pc.RepoLockKey,
		ConfigPath:  configPath,
	})
	if err != nil {
		return OutgoingMessage{}, err
	}
	if err := e.store.BindChat(ctx, msg.Channel, msg.ChatID, proj.ID); err != nil {
		return OutgoingMessage{}, err
	}

	cfgStatus := "found"
	if !hasConfig {
		cfgStatus = "not found (using default status command)"
	}
	reply := fmt.Sprintf("Bound chat to project %q\n- Path: %s\n- Repo Lock Key: %s\n- rd-project.yaml: %s\n- Skills Symlink: %s",
		proj.Name, proj.RootPath, proj.RepoLockKey, cfgStatus, linkNote)
	return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: reply}, nil
}

func (e *Engine) requireBoundProject(ctx context.Context, msg IncomingMessage) (*store.Project, *OutgoingMessage, error) {
	proj, err := e.store.GetBoundProject(ctx, msg.Channel, msg.ChatID)
	if errors.Is(err, store.ErrNotFound) {
		out := &OutgoingMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Text:    "This chat is not bound to a project yet. Run `/bind <project-name>` first.",
		}
		return nil, out, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return proj, nil, nil
}

func (e *Engine) handleNew(ctx context.Context, msg IncomingMessage, title string) (OutgoingMessage, error) {
	proj, errReply, err := e.requireBoundProject(ctx, msg)
	if err != nil || errReply != nil {
		if errReply != nil {
			return *errReply, nil
		}
		return OutgoingMessage{}, err
	}
	if title == "" {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: "Usage: /new <task title>"}, nil
	}

	runID := fmt.Sprintf("run-%s-%s", time.Now().UTC().Format("20060102-150405"), uuid.NewString()[:6])
	run, err := e.store.CreateRun(ctx, store.Run{
		ID:        runID,
		ProjectID: proj.ID,
		Title:     title,
		State:     store.StatePlanning,
	})
	if err != nil {
		return OutgoingMessage{}, err
	}
	_ = e.store.AppendEvent(ctx, run.ID, msg.UserID, "run_created", map[string]string{"title": title})

	blueprintPath := filepath.Join(e.cfg.RunDir(run.ID), "01-blueprint.md")
	header := fmt.Sprintf("# Blueprint: %s\nProject: %s\nRun ID: %s\n\n", title, proj.Name, run.ID)
	if err := runner.WriteAtomicBytes(blueprintPath, []byte(header)); err != nil {
		return OutgoingMessage{}, err
	}

	return OutgoingMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Text:    fmt.Sprintf("Started Run `%s` for **%s** (`PLANNING`).\nSend requirements in plain text to plan with `%s`, then run `/gate0` when ready.", run.ID, proj.Name, e.cfg.Roles.Planner),
	}, nil
}

func (e *Engine) handlePlanningChat(ctx context.Context, msg IncomingMessage, text string) (OutgoingMessage, error) {
	proj, errReply, err := e.requireBoundProject(ctx, msg)
	if err != nil || errReply != nil {
		if errReply != nil {
			return *errReply, nil
		}
		return OutgoingMessage{}, err
	}

	run, err := e.store.GetLatestRunForProject(ctx, proj.ID)
	if errors.Is(err, store.ErrNotFound) {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: "No active Run. Start one with `/new <task title>`."}, nil
	}
	if err != nil {
		return OutgoingMessage{}, err
	}
	if run.State != store.StatePlanning {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Run `%s` is currently in `%s`. Use /status, /approve, /reject, /retry, or /new.", run.ID, run.State)}, nil
	}

	execAdapter, err := e.registry.Get(e.cfg.Roles.Planner)
	if err != nil {
		return OutgoingMessage{}, err
	}

	skillFiles := e.resolveSkills("design-system-arch", "design-feature-spec", "design-data-model")
	req := executor.Request{
		WorkDir:      proj.RootPath,
		SystemPrompt: fmt.Sprintf("You are the Stage 1 Planner for project %q (Run %s: %s). Discuss requirements concisely, surface Open Decisions clearly, and do NOT modify files.", proj.Name, run.ID, run.Title),
		Prompt:       text,
		Mode:         executor.ModeReadOnly,
		SessionID:    run.PlannerSessionID,
		SkillFiles:   skillFiles,
		Timeout:      e.cfg.StepTimeout(),
		Model:        e.modelForAdapter(execAdapter.Name()),
	}

	attemptID := uuid.NewString()
	started := time.Now().UTC()
	resp, runErr := execAdapter.Run(ctx, req)
	finished := time.Now().UTC()

	status := "OK"
	exitCode := 0
	replyText := ""
	if resp != nil {
		exitCode = resp.ExitCode
		replyText = strings.TrimSpace(resp.TextOutput)
		if resp.SessionID != "" {
			_ = e.store.UpdatePlannerSession(ctx, run.ID, resp.SessionID)
		}
	}
	if runErr != nil {
		status = "ERROR"
		if replyText == "" {
			replyText = runErr.Error()
		}
	}

	blueprintPath := filepath.Join(e.cfg.RunDir(run.ID), "01-blueprint.md")
	appendPlanningTurn(blueprintPath, text, replyText)

	_ = e.store.RecordStageAttempt(ctx, store.StageAttempt{
		ID:           attemptID,
		RunID:        run.ID,
		Stage:        "stage-1-plan",
		Role:         "planner",
		Adapter:      execAdapter.Name(),
		SessionID:    sessionIDOf(resp),
		Status:       status,
		ExitCode:     exitCode,
		ArtifactPath: blueprintPath,
		StartedAt:    started,
		FinishedAt:   &finished,
	})

	if runErr != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Planner error: %v", runErr)}, nil
	}
	return OutgoingMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Text:    replyText,
		Actions: []ActionButton{{Label: "Run Gate 0", Data: "/gate0"}},
	}, nil
}

func (e *Engine) handleGate0(ctx context.Context, msg IncomingMessage) (OutgoingMessage, error) {
	proj, errReply, err := e.requireBoundProject(ctx, msg)
	if err != nil || errReply != nil {
		if errReply != nil {
			return *errReply, nil
		}
		return OutgoingMessage{}, err
	}
	run, err := e.store.GetLatestRunForProject(ctx, proj.ID)
	if err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: "No active Run. Start one with `/new <task title>`."}, nil
	}

	if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StatePlanning}, store.StateGate0Checking, "", msg.UserID, "gate0_started", nil); err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Cannot run Gate 0 from state `%s`.", run.State)}, nil
	}

	execAdapter, err := e.registry.Get(e.cfg.Roles.Planner)
	if err != nil {
		_ = e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateGate0Checking}, store.StatePlanning, err.Error(), "system", "gate0_error", nil)
		return OutgoingMessage{}, err
	}

	blueprintPath := filepath.Join(e.cfg.RunDir(run.ID), "01-blueprint.md")
	blueprintBytes, _ := os.ReadFile(blueprintPath)

	req := executor.Request{
		WorkDir:      proj.RootPath,
		SystemPrompt: "Execute Gate 0 blueprint check strictly. Set result=PASS ONLY if open_decisions is empty and roadmap + acceptance_tcs are concrete.",
		Prompt:       fmt.Sprintf("Run ID: %s\nTitle: %s\n\nAccumulated Blueprint:\n%s", run.ID, run.Title, string(blueprintBytes)),
		Mode:         executor.ModeReadOnly,
		SkillFiles:   e.resolveSkills("gate-blueprint-check"),
		JSONSchema:   Gate0Schema,
		Timeout:      e.cfg.StepTimeout(),
		Model:        e.modelForAdapter(execAdapter.Name()),
	}

	attemptID := uuid.NewString()
	started := time.Now().UTC()
	var verdict Gate0Verdict
	resp, rawJSON, runErr := e.runStructuredWithRetry(ctx, execAdapter, req, &verdict)
	finished := time.Now().UTC()

	gate0Path := filepath.Join(e.cfg.RunDir(run.ID), "02-gate0.json")
	if rawJSON != "" {
		_ = runner.WriteAtomicBytes(gate0Path, []byte(rawJSON))
	}

	_ = e.store.RecordStageAttempt(ctx, store.StageAttempt{
		ID:           attemptID,
		RunID:        run.ID,
		Stage:        "stage-1-gate0",
		Role:         "planner",
		Adapter:      execAdapter.Name(),
		SessionID:    sessionIDOf(resp),
		Status:       statusFromErr(runErr),
		ExitCode:     exitCodeOf(resp),
		VerdictJSON:  rawJSON,
		ArtifactPath: gate0Path,
		StartedAt:    started,
		FinishedAt:   &finished,
	})

	if runErr != nil {
		_ = e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateGate0Checking}, store.StatePlanning, runErr.Error(), "system", "gate0_failed", nil)
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Gate 0 failed to produce valid verdict: %v\nReturned to `PLANNING`.", runErr)}, nil
	}

	// Hard rule: Open Decisions > 0 forces BLOCKED even if model claimed PASS.
	if len(verdict.OpenDecisions) > 0 {
		verdict.Result = "BLOCKED"
	}

	if verdict.Result == "PASS" {
		if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateGate0Checking}, store.StateWaitingGate0Approval, "", "system", "gate0_passed", verdict); err != nil {
			return OutgoingMessage{}, err
		}
		summary := fmt.Sprintf("[Gate 0: PASS — READY TO BUILD]\nRun: `%s`\nSummary: %s\nSlices: %d | TCs: %d\n\nSend `/approve` to launch Stage 2 autonomous implementation.",
			run.ID, verdict.Summary, len(verdict.Roadmap), len(verdict.AcceptanceTCs))
		return OutgoingMessage{
			Channel:    msg.Channel,
			ChatID:     msg.ChatID,
			Text:       summary,
			Attachment: gate0Path,
			Actions: []ActionButton{
				{Label: "Approve & Build", Data: "/approve"},
				{Label: "Reject", Data: "/reject needs revision"},
			},
		}, nil
	}

	_ = e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateGate0Checking}, store.StatePlanning, "Gate 0 BLOCKED", "system", "gate0_blocked", verdict)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[Gate 0: BLOCKED]\nSummary: %s\n", verdict.Summary))
	if len(verdict.OpenDecisions) > 0 {
		b.WriteString("\nOpen Decisions:\n")
		for i, od := range verdict.OpenDecisions {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, od))
		}
	}
	b.WriteString("\nReply to resolve decisions, then run `/gate0` again.")
	return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: b.String(), Attachment: gate0Path}, nil
}

func (e *Engine) handleApprove(ctx context.Context, msg IncomingMessage) (OutgoingMessage, error) {
	proj, errReply, err := e.requireBoundProject(ctx, msg)
	if err != nil || errReply != nil {
		if errReply != nil {
			return *errReply, nil
		}
		return OutgoingMessage{}, err
	}
	run, err := e.store.GetLatestRunForProject(ctx, proj.ID)
	if err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: "No active Run to approve."}, nil
	}

	switch run.State {
	case store.StateWaitingGate0Approval:
		if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateWaitingGate0Approval}, store.StateQueued, "", msg.UserID, "gate0_approved", nil); err != nil {
			return OutgoingMessage{}, err
		}
		e.dispatchRun(run.ID)
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Gate 0 approved for `%s`. Queued for autonomous Stage 2 → Stage 3 execution.", run.ID)}, nil

	case store.StateWaitingSkillApproval:
		// Validate central skills before unlocking Stage 2 code correction (AGENTS.md rule 4).
		if e.cfg.CentralRepoDir != "" {
			valScript := filepath.Join(e.cfg.CentralRepoDir, "scripts", "validate-skills.mjs")
			if _, err := os.Stat(valScript); err == nil {
				res := runner.RunShell(ctx, e.cfg.CentralRepoDir, "validate-skills", "node scripts/validate-skills.mjs", 30*time.Second)
				if res.Status != "PASS" {
					return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Cannot approve skill correction: `validate-skills.mjs` failed:\n%s\n%s", res.Stdout, res.Stderr)}, nil
				}
			}
		}
		if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateWaitingSkillApproval}, store.StateQueued, "", msg.UserID, "skill_correction_approved", nil); err != nil {
			return OutgoingMessage{}, err
		}
		e.dispatchRun(run.ID)
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Skill correction approved for `%s`. Resuming Stage 2 code correction with updated skills.", run.ID)}, nil

	case store.StateReadyForRelease:
		if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateReadyForRelease}, store.StateCompleted, "", msg.UserID, "release_approved", nil); err != nil {
			return OutgoingMessage{}, err
		}
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Run `%s` marked `COMPLETED`.", run.ID)}, nil

	default:
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Run `%s` is in state `%s` — nothing awaiting approval.", run.ID, run.State)}, nil
	}
}

func (e *Engine) handleReject(ctx context.Context, msg IncomingMessage, reason string) (OutgoingMessage, error) {
	proj, errReply, err := e.requireBoundProject(ctx, msg)
	if err != nil || errReply != nil {
		if errReply != nil {
			return *errReply, nil
		}
		return OutgoingMessage{}, err
	}
	run, err := e.store.GetLatestRunForProject(ctx, proj.ID)
	if err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: "No active Run to reject."}, nil
	}
	if reason == "" {
		reason = "rejected by operator"
	}

	switch run.State {
	case store.StateWaitingGate0Approval:
		if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateWaitingGate0Approval}, store.StatePlanning, reason, msg.UserID, "gate0_rejected", map[string]string{"reason": reason}); err != nil {
			return OutgoingMessage{}, err
		}
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Gate 0 rejected. Run `%s` returned to `PLANNING` (%s).", run.ID, reason)}, nil

	case store.StateWaitingSkillApproval, store.StateReadyForRelease:
		if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{run.State}, store.StateWaitingHuman, reason, msg.UserID, "approval_rejected", map[string]string{"reason": reason}); err != nil {
			return OutgoingMessage{}, err
		}
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Run `%s` moved to `WAITING_HUMAN` (%s).", run.ID, reason)}, nil

	default:
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Run `%s` is in state `%s` — cannot reject.", run.ID, run.State)}, nil
	}
}

func (e *Engine) handleCheck(ctx context.Context, msg IncomingMessage, key string) (OutgoingMessage, error) {
	proj, errReply, err := e.requireBoundProject(ctx, msg)
	if err != nil || errReply != nil {
		if errReply != nil {
			return *errReply, nil
		}
		return OutgoingMessage{}, err
	}
	pc, _, err := config.LoadProjectConfig(proj.RootPath)
	if err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Failed to load rd-project.yaml: %v", err)}, nil
	}

	var keys []string
	if key != "" {
		keys = []string{key}
	} else {
		keys = []string{"status", "static_check", "test", "build_check"}
	}
	rep := runner.RunChecks(ctx, proj.RootPath, pc, keys, e.cfg.StepTimeout())
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[No-AI Project Check: %s] AllPassed=%v\n", proj.Name, rep.AllPassed))
	for _, r := range rep.Results {
		b.WriteString(fmt.Sprintf("- %s: %s (exit=%d, %dms)\n", r.Key, r.Status, r.ExitCode, r.DurationMs))
		if r.Status == "FAIL" && strings.TrimSpace(r.Stderr+r.Stdout) != "" {
			snippet := strings.TrimSpace(r.Stderr)
			if snippet == "" {
				snippet = strings.TrimSpace(r.Stdout)
			}
			if len(snippet) > 400 {
				snippet = snippet[len(snippet)-400:]
			}
			b.WriteString("  " + snippet + "\n")
		}
	}
	return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: b.String()}, nil
}

func (e *Engine) handleStatus(ctx context.Context, msg IncomingMessage) (OutgoingMessage, error) {
	proj, errReply, err := e.requireBoundProject(ctx, msg)
	if err != nil || errReply != nil {
		if errReply != nil {
			return *errReply, nil
		}
		return OutgoingMessage{}, err
	}
	run, err := e.store.GetLatestRunForProject(ctx, proj.ID)
	if errors.Is(err, store.ErrNotFound) {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Project `%s` bound (%s). No Runs yet. Use `/new <title>`.", proj.Name, proj.RootPath)}, nil
	}
	if err != nil {
		return OutgoingMessage{}, err
	}
	text := fmt.Sprintf("Project: `%s`\nRun: `%s` — %s\nState: `%s` (retries=%d/%d)", proj.Name, run.ID, run.Title, run.State, run.RetryCount, e.cfg.MaxRetries)
	if run.CurrentError != "" {
		text += "\nNote: " + run.CurrentError
	}
	return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: text}, nil
}

func (e *Engine) handleLogs(ctx context.Context, msg IncomingMessage) (OutgoingMessage, error) {
	proj, errReply, err := e.requireBoundProject(ctx, msg)
	if err != nil || errReply != nil {
		if errReply != nil {
			return *errReply, nil
		}
		return OutgoingMessage{}, err
	}
	run, err := e.store.GetLatestRunForProject(ctx, proj.ID)
	if err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: "No active Run."}, nil
	}
	attempts, err := e.store.ListStageAttempts(ctx, run.ID)
	if err != nil {
		return OutgoingMessage{}, err
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Run `%s` artifacts: %s\n", run.ID, e.cfg.RunDir(run.ID)))
	if len(attempts) == 0 {
		b.WriteString("No stage attempts recorded yet.")
	}
	for _, a := range attempts {
		b.WriteString(fmt.Sprintf("- %s [%s/%s]: %s (exit=%d)\n", a.Stage, a.Role, a.Adapter, a.Status, a.ExitCode))
	}
	return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: b.String()}, nil
}

func (e *Engine) handleRetry(ctx context.Context, msg IncomingMessage) (OutgoingMessage, error) {
	proj, errReply, err := e.requireBoundProject(ctx, msg)
	if err != nil || errReply != nil {
		if errReply != nil {
			return *errReply, nil
		}
		return OutgoingMessage{}, err
	}
	run, err := e.store.GetLatestRunForProject(ctx, proj.ID)
	if err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: "No active Run."}, nil
	}
	if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateWaitingHuman}, store.StateQueued, "", msg.UserID, "human_retry", nil); err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Cannot /retry from state `%s`.", run.State)}, nil
	}
	e.dispatchRun(run.ID)
	return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Run `%s` re-queued for Stage 2 execution.", run.ID)}, nil
}

func (e *Engine) handleAbort(ctx context.Context, msg IncomingMessage) (OutgoingMessage, error) {
	proj, errReply, err := e.requireBoundProject(ctx, msg)
	if err != nil || errReply != nil {
		if errReply != nil {
			return *errReply, nil
		}
		return OutgoingMessage{}, err
	}
	run, err := e.store.GetLatestRunForProject(ctx, proj.ID)
	if err != nil {
		return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: "No active Run to abort."}, nil
	}

	e.mu.Lock()
	cancel, ok := e.activeCancel[run.ID]
	e.mu.Unlock()
	if ok {
		cancel()
	}

	if err := e.store.TransitionRunState(ctx, run.ID, nil, store.StateCancelled, "aborted by user", msg.UserID, "user_aborted", nil); err != nil {
		return OutgoingMessage{}, err
	}
	return OutgoingMessage{Channel: msg.Channel, ChatID: msg.ChatID, Text: fmt.Sprintf("Run `%s` cancelled and active subprocesses terminated.", run.ID)}, nil
}

func (e *Engine) dispatchRun(runID string) {
	if !e.AsyncDispatch {
		return
	}
	go func() {
		_ = e.ExecuteRun(context.Background(), runID)
	}()
}

// ExecuteRun runs an approved QUEUED Run through Stage 2 -> Stage 3 Parallel Fan-out/Fan-in -> Stage 4.
// Returns immediately without starting if another Run already holds the same repo_lock_key (TC-07).
func (e *Engine) ExecuteRun(parentCtx context.Context, runID string) error {
	run, err := e.store.GetRun(parentCtx, runID)
	if err != nil {
		return err
	}
	if run.State != store.StateQueued {
		return fmt.Errorf("%w: ExecuteRun requires QUEUED state (Gate 0 approval), got %s", store.ErrInvalidTransition, run.State)
	}

	proj, err := e.store.GetProjectByID(parentCtx, run.ProjectID)
	if err != nil {
		return err
	}

	// Enforce per-repo sequential execution (D-09, TC-07).
	locked, err := e.store.HasActiveRunForRepoLock(parentCtx, proj.RepoLockKey, run.ID)
	if err != nil {
		return err
	}
	if locked {
		e.notifyProjectChannels(parentCtx, proj.ID, fmt.Sprintf("Run `%s` is `QUEUED` waiting on repo lock `%s`.", run.ID, proj.RepoLockKey), "", nil)
		return nil
	}

	ctx, cancel := context.WithCancel(parentCtx)
	e.mu.Lock()
	e.activeCancel[run.ID] = cancel
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		delete(e.activeCancel, run.ID)
		e.mu.Unlock()
		e.drainRepoLockQueue(context.Background(), proj.RepoLockKey)
	}()

	if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateQueued}, store.StateImplementing, "", "system", "stage2_started", nil); err != nil {
		return err
	}

	for {
		run, err = e.store.GetRun(ctx, runID)
		if err != nil {
			return err
		}

		// Stage 2: Autonomous Implementation
		implVerdict, implPath, err := e.runStage2Implement(ctx, proj, run)
		if err != nil {
			e.moveToWaitingHuman(ctx, proj.ID, run.ID, store.StateImplementing, fmt.Sprintf("Stage 2 execution error: %v", err), implPath)
			return err
		}
		if implVerdict.Status == "ABORTED" {
			e.moveToWaitingHuman(ctx, proj.ID, run.ID, store.StateImplementing, fmt.Sprintf("Stage 2 Stop Policy triggered: %s", implVerdict.StopReason), implPath)
			return nil
		}

		// Stage 3: No-AI Evidence + Parallel Golden Set A & B -> Fan-in
		if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateImplementing}, store.StateEvaluatingParallel, "", "system", "stage3_parallel_started", nil); err != nil {
			return err
		}
		e.notifyProjectChannels(ctx, proj.ID, fmt.Sprintf("[Stage 3] Run `%s`: running No-AI checks + parallel Golden Set A (`%s`) & Golden Set B (`%s` Read-Only).", run.ID, e.cfg.Roles.GoldenA, e.cfg.Roles.GoldenB), "", nil)

		verdictA, verdictB, gateErr := e.runStage3ParallelGates(ctx, proj, run, implVerdict)
		if gateErr != nil {
			e.moveToWaitingHuman(ctx, proj.ID, run.ID, store.StateEvaluatingParallel, fmt.Sprintf("Stage 3 gate error: %v", gateErr), "")
			return gateErr
		}

		// Fan-in Decision (D-05, D-06): A == PASS AND B.CRITICAL == 0
		aPassed := verdictA.Result == "PASS"
		bPassed := verdictB.CriticalCount == 0 && verdictB.Result != "FAIL"
		if aPassed && bPassed {
			if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateEvaluatingParallel}, store.StateReadyForRelease, "", "system", "stage3_passed", map[string]any{"a": verdictA, "b": verdictB}); err != nil {
				return err
			}
			msgText := fmt.Sprintf("[Stage 3: PASS — READY FOR RELEASE]\nRun: `%s`\n- Golden Set A: PASS (%s)\n- Golden Set B: %s (CRITICAL=%d, MEDIUM=%d, LOW=%d)\n\nSend `/approve` to mark COMPLETED.",
				run.ID, verdictA.Summary, verdictB.Result, verdictB.CriticalCount, verdictB.MediumCount, verdictB.LowCount)
			e.notifyProjectChannels(ctx, proj.ID, msgText, "", []ActionButton{
				{Label: "Complete Release", Data: "/approve"},
				{Label: "Hold / Reject", Data: "/reject hold"},
			})
			return nil
		}

		// Stage 4: Unified Remediation (Fan-in completed)
		if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateEvaluatingParallel}, store.StateRemediating, "", "system", "stage4_remediation_started", nil); err != nil {
			return err
		}

		retryCount, err := e.store.IncrementRetryCount(ctx, run.ID)
		if err != nil {
			return err
		}
		if retryCount > e.cfg.MaxRetries {
			e.moveToWaitingHuman(ctx, proj.ID, run.ID, store.StateRemediating, fmt.Sprintf("Circuit Breaker: exceeded max remediation retries (%d). A=%s, B.CRITICAL=%d", e.cfg.MaxRetries, verdictA.Result, verdictB.CriticalCount), "")
			return nil
		}

		remVerdict, remPath, err := e.runStage4Remediation(ctx, proj, run, verdictA, verdictB)
		if err != nil {
			e.moveToWaitingHuman(ctx, proj.ID, run.ID, store.StateRemediating, fmt.Sprintf("Stage 4 remediation error: %v", err), remPath)
			return err
		}

		// D-07 / TC-04: Type S skill defect forces SKILL CORRECTION FIRST + Human Approval Gate!
		if remVerdict.Sequence == "SKILL_CORRECTION_FIRST" {
			skillErr := e.runStage4SkillEvolution(ctx, proj, run, remVerdict)
			if skillErr != nil {
				e.moveToWaitingHuman(ctx, proj.ID, run.ID, store.StateRemediating, fmt.Sprintf("Stage 4 skill evolution blocked: %v", skillErr), remPath)
				return skillErr
			}
			if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateRemediating}, store.StateWaitingSkillApproval, "", "system", "waiting_skill_approval", remVerdict); err != nil {
				return err
			}
			e.notifyProjectChannels(ctx, proj.ID,
				fmt.Sprintf("[Stage 4: SKILL CORRECTION FIRST — Approval Required]\nRun: `%s`\nSummary: %s\nSkill Plan: %s\n\nReview changes in `dev-automation/skills/` and send `/approve` to resume Stage 2 code correction.", run.ID, remVerdict.Summary, remVerdict.SkillCorrectionPlan),
				remPath,
				[]ActionButton{
					{Label: "Approve Skill Fix & Resume", Data: "/approve"},
					{Label: "Reject", Data: "/reject revise skill fix"},
				},
			)
			return nil
		}

		// Type C only: loop straight back into Stage 2 with unified directive.
		e.notifyProjectChannels(ctx, proj.ID, fmt.Sprintf("[Stage 4: CODE CORRECTION ONLY] Retry %d/%d for `%s`: %s", retryCount, e.cfg.MaxRetries, run.ID, remVerdict.Summary), remPath, nil)
		if err := e.store.TransitionRunState(ctx, run.ID, []store.RunState{store.StateRemediating}, store.StateImplementing, "", "system", "stage4_loop_to_stage2", remVerdict); err != nil {
			return err
		}
	}
}

func (e *Engine) runStage2Implement(ctx context.Context, proj *store.Project, run *store.Run) (*ImplVerdict, string, error) {
	execAdapter, err := e.registry.Get(e.cfg.Roles.Implementer)
	if err != nil {
		return nil, "", err
	}

	runDir := e.cfg.RunDir(run.ID)
	blueprint, _ := os.ReadFile(filepath.Join(runDir, "01-blueprint.md"))
	gate0, _ := os.ReadFile(filepath.Join(runDir, "02-gate0.json"))
	remediation, _ := os.ReadFile(filepath.Join(runDir, fmt.Sprintf("07-remediation-%d.json", run.RetryCount)))

	var prompt strings.Builder
	prompt.WriteString(fmt.Sprintf("Project: %s\nRun ID: %s (%s)\nRetry Attempt: %d\n\n", proj.Name, run.ID, run.Title, run.RetryCount))
	prompt.WriteString("=== Approved Gate 0 Blueprint ===\n")
	prompt.WriteString(string(gate0))
	prompt.WriteString("\n\n=== Planning Notes ===\n")
	prompt.WriteString(string(blueprint))
	if len(remediation) > 0 {
		prompt.WriteString("\n\n=== Mandatory Remediation Directive (Fix Root Causes — No Reward Hacking) ===\n")
		prompt.WriteString(string(remediation))
	}

	req := executor.Request{
		WorkDir:      proj.RootPath,
		SystemPrompt: "Execute Stage 2 autonomous implementation strictly following impl-autonomous-slice. Do NOT self-certify Golden Set A or B.",
		Prompt:       prompt.String(),
		Mode:         executor.ModeWorkspaceWrite,
		SkillFiles:   e.resolveSkills("impl-autonomous-slice"),
		JSONSchema:   ImplSchema,
		Timeout:      e.cfg.StepTimeout(),
		Model:        e.modelForAdapter(execAdapter.Name()),
		MaxBudgetUSD: e.cfg.Models.ClaudeMaxBudgetUSD,
	}

	attemptID := uuid.NewString()
	started := time.Now().UTC()
	var verdict ImplVerdict
	resp, rawJSON, err := e.runStructuredWithRetry(ctx, execAdapter, req, &verdict)
	finished := time.Now().UTC()

	artPath := filepath.Join(runDir, fmt.Sprintf("03-impl-attempt-%d.json", run.RetryCount))
	if rawJSON != "" {
		_ = runner.WriteAtomicBytes(artPath, []byte(rawJSON))
	}

	_ = e.store.RecordStageAttempt(ctx, store.StageAttempt{
		ID:           attemptID,
		RunID:        run.ID,
		Stage:        "stage-2-impl",
		Role:         "implementer",
		Adapter:      execAdapter.Name(),
		SessionID:    sessionIDOf(resp),
		Status:       statusFromErr(err),
		ExitCode:     exitCodeOf(resp),
		VerdictJSON:  rawJSON,
		ArtifactPath: artPath,
		StartedAt:    started,
		FinishedAt:   &finished,
	})
	return &verdict, artPath, err
}

func (e *Engine) runStage3ParallelGates(ctx context.Context, proj *store.Project, run *store.Run, impl *ImplVerdict) (*GoldenAVerdict, *GoldenBVerdict, error) {
	adapterA, err := e.registry.Get(e.cfg.Roles.GoldenA)
	if err != nil {
		return nil, nil, err
	}
	adapterB, err := e.registry.Get(e.cfg.Roles.GoldenB)
	if err != nil {
		return nil, nil, err
	}

	runDir := e.cfg.RunDir(run.ID)
	pc, _, err := config.LoadProjectConfig(proj.RootPath)
	if err != nil {
		return nil, nil, err
	}

	// Step 3.0: Deterministic No-AI Project CLI Evidence BEFORE parallel read-only AI evaluations.
	cliReport := runner.RunChecks(ctx, proj.RootPath, pc, []string{"static_check", "test", "build_check", "e2e"}, e.cfg.StepTimeout())
	cliPath := filepath.Join(runDir, fmt.Sprintf("04-cli-evidence-%d.json", run.RetryCount))
	_ = runner.WriteAtomicJSON(cliPath, cliReport)
	cliJSON, _ := json.MarshalIndent(cliReport, "", "  ")

	// Capture git status & diff snapshot AFTER No-AI checks so ReadOnly guard verifies neither AI mutates the repo (TC-05).
	beforeStatus, gitErr := runner.GitStatusPorcelain(ctx, proj.RootPath)
	diffSummary := runner.GitDiffSummary(ctx, proj.RootPath, 32000)
	gate0Bytes, _ := os.ReadFile(filepath.Join(runDir, "02-gate0.json"))
	implJSON, _ := json.MarshalIndent(impl, "", "  ")

	var verdictA GoldenAVerdict
	var verdictB GoldenBVerdict

	// Step 3.1: Parallel Fan-out via errgroup — BOTH must complete before Fan-in (D-05, TC-02).
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		reqA := executor.Request{
			WorkDir:      proj.RootPath,
			SystemPrompt: "You are independent Evaluator AI #2 (Golden Set A). Separate process from Implementer. Judge strictly 100% Hard Pass/Fail based on deterministic CLI evidence and Gate 0 TCs. Do NOT edit files.",
			Prompt:       fmt.Sprintf("Gate 0 TCs:\n%s\n\nNo-AI Deterministic CLI Evidence:\n%s\n\nGit Diff:\n%s", string(gate0Bytes), string(cliJSON), diffSummary),
			Mode:         executor.ModeReadOnly,
			SessionID:    "", // Always fresh isolated session
			SkillFiles:   e.resolveSkills("eval-release-pr-gate"),
			JSONSchema:   GoldenASchema,
			Timeout:      e.cfg.StepTimeout(),
			Model:        e.modelForAdapter(adapterA.Name()),
		}
		attemptID := uuid.NewString()
		started := time.Now().UTC()
		resp, rawJSON, err := e.runStructuredWithRetry(gCtx, adapterA, reqA, &verdictA)
		finished := time.Now().UTC()

		artPath := filepath.Join(runDir, fmt.Sprintf("05-golden-a-%d.json", run.RetryCount))
		if rawJSON != "" {
			_ = runner.WriteAtomicBytes(artPath, []byte(rawJSON))
		}

		// Hard gate: if deterministic No-AI CLI checks failed, force Golden Set A FAIL regardless of LLM text.
		if err == nil && !cliReport.AllPassed {
			verdictA.Result = "FAIL"
			verdictA.Failures = append(verdictA.Failures, "Deterministic No-AI CLI check suite failed (see 04-cli-evidence)")
			if updated, mErr := json.MarshalIndent(verdictA, "", "  "); mErr == nil {
				rawJSON = string(updated)
				_ = runner.WriteAtomicBytes(artPath, updated)
			}
		}

		_ = e.store.RecordStageAttempt(ctx, store.StageAttempt{
			ID:           attemptID,
			RunID:        run.ID,
			Stage:        "stage-3-golden-a",
			Role:         "reviewer-a",
			Adapter:      adapterA.Name(),
			SessionID:    sessionIDOf(resp),
			Status:       statusFromErr(err),
			ExitCode:     exitCodeOf(resp),
			VerdictJSON:  rawJSON,
			ArtifactPath: artPath,
			StartedAt:    started,
			FinishedAt:   &finished,
		})
		return err
	})

	g.Go(func() error {
		reqB := executor.Request{
			WorkDir:      proj.RootPath,
			SystemPrompt: "You are independent Senior Reviewer AI #3 (Golden Set B, Read-Only). Judge strictly by severity (CRITICAL >= 1 => FAIL; MEDIUM/LOW => Advice). Detect Reward Hacking and skill violations. NEVER edit files.",
			Prompt:       fmt.Sprintf("Gate 0 Blueprint:\n%s\n\nImplementer Compliance Report:\n%s\n\nGit Diff:\n%s", string(gate0Bytes), string(implJSON), diffSummary),
			Mode:         executor.ModeReadOnly,
			SessionID:    "", // Always fresh isolated session
			SkillFiles:   e.resolveSkills("eval-senior-quality-gate"),
			JSONSchema:   GoldenBSchema,
			Timeout:      e.cfg.StepTimeout(),
			Model:        e.modelForAdapter(adapterB.Name()),
		}
		attemptID := uuid.NewString()
		started := time.Now().UTC()
		resp, rawJSON, err := e.runStructuredWithRetry(gCtx, adapterB, reqB, &verdictB)
		finished := time.Now().UTC()

		artPath := filepath.Join(runDir, fmt.Sprintf("06-golden-b-%d.json", run.RetryCount))
		if rawJSON != "" {
			_ = runner.WriteAtomicBytes(artPath, []byte(rawJSON))
		}

		// Hard rule (D-06): CRITICAL >= 1 always forces FAIL.
		if err == nil && verdictB.CriticalCount >= 1 {
			verdictB.Result = "FAIL"
		}

		_ = e.store.RecordStageAttempt(ctx, store.StageAttempt{
			ID:           attemptID,
			RunID:        run.ID,
			Stage:        "stage-3-golden-b",
			Role:         "reviewer-b",
			Adapter:      adapterB.Name(),
			SessionID:    sessionIDOf(resp),
			Status:       statusFromErr(err),
			ExitCode:     exitCodeOf(resp),
			VerdictJSON:  rawJSON,
			ArtifactPath: artPath,
			StartedAt:    started,
			FinishedAt:   &finished,
		})
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	// TC-05: Dual Read-Only verification — ensure git working tree was NOT mutated during Stage 3.
	if gitErr == nil {
		if afterStatus, afterErr := runner.GitStatusPorcelain(ctx, proj.RootPath); afterErr == nil {
			if roErr := executor.VerifyReadOnlyUnchanged(beforeStatus, afterStatus); roErr != nil {
				return &verdictA, &verdictB, roErr
			}
		}
	}

	return &verdictA, &verdictB, nil
}

func (e *Engine) runStage4Remediation(ctx context.Context, proj *store.Project, run *store.Run, a *GoldenAVerdict, b *GoldenBVerdict) (*RemediationVerdict, string, error) {
	execAdapter, err := e.registry.Get(e.cfg.Roles.Remediation)
	if err != nil {
		return nil, "", err
	}

	aJSON, _ := json.MarshalIndent(a, "", "  ")
	bJSON, _ := json.MarshalIndent(b, "", "  ")
	diffSummary := runner.GitDiffSummary(ctx, proj.RootPath, 24000)

	req := executor.Request{
		WorkDir:      proj.RootPath,
		SystemPrompt: "You are the Stage 4 Remediation Director. Both Golden Set A and B have completed (Fan-in). Classify root causes as Type S (skill/blueprint defect -> SKILL_CORRECTION_FIRST) or Type C (code mistake -> CODE_CORRECTION_ONLY) and issue a single anti-hacking directive.",
		Prompt:       fmt.Sprintf("Golden Set A Verdict:\n%s\n\nGolden Set B Verdict:\n%s\n\nGit Diff:\n%s", string(aJSON), string(bJSON), diffSummary),
		Mode:         executor.ModeReadOnly,
		SkillFiles:   e.resolveSkills("ops-remediation-director"),
		JSONSchema:   RemediationSchema,
		Timeout:      e.cfg.StepTimeout(),
		Model:        e.modelForAdapter(execAdapter.Name()),
	}

	attemptID := uuid.NewString()
	started := time.Now().UTC()
	var verdict RemediationVerdict
	resp, rawJSON, err := e.runStructuredWithRetry(ctx, execAdapter, req, &verdict)
	finished := time.Now().UTC()

	artPath := filepath.Join(e.cfg.RunDir(run.ID), fmt.Sprintf("07-remediation-%d.json", run.RetryCount))
	if rawJSON != "" {
		_ = runner.WriteAtomicBytes(artPath, []byte(rawJSON))
	}

	_ = e.store.RecordStageAttempt(ctx, store.StageAttempt{
		ID:           attemptID,
		RunID:        run.ID,
		Stage:        "stage-4-remediation",
		Role:         "director",
		Adapter:      execAdapter.Name(),
		SessionID:    sessionIDOf(resp),
		Status:       statusFromErr(err),
		ExitCode:     exitCodeOf(resp),
		VerdictJSON:  rawJSON,
		ArtifactPath: artPath,
		StartedAt:    started,
		FinishedAt:   &finished,
	})
	return &verdict, artPath, err
}

func (e *Engine) runStage4SkillEvolution(ctx context.Context, proj *store.Project, run *store.Run, rem *RemediationVerdict) error {
	if e.cfg.CentralRepoDir == "" {
		return errors.New("central_repo_dir not configured; cannot execute ops-skill-evolution")
	}
	execAdapter, err := e.registry.Get(e.cfg.Roles.SkillCurator)
	if err != nil {
		return err
	}

	beforePorcelain, gitErr := runner.GitStatusPorcelain(ctx, e.cfg.CentralRepoDir)
	beforePaths := runner.GitChangedPaths(beforePorcelain)

	req := executor.Request{
		WorkDir:      e.cfg.CentralRepoDir,
		SystemPrompt: "Execute Stage 4 ops-skill-evolution. You MAY ONLY modify files under `skills/`. Modifying `harness/`, `scripts/`, or `AGENTS.md` is strictly forbidden.",
		Prompt:       fmt.Sprintf("Target Project: %s\nRun ID: %s\nSkill Correction Plan:\n%s\n\nRoot Causes:\n%s", proj.Name, run.ID, rem.SkillCorrectionPlan, strings.Join(rem.RootCauses, "\n- ")),
		Mode:         executor.ModeWorkspaceWrite,
		SkillFiles:   e.resolveSkills("ops-skill-evolution"),
		JSONSchema:   SkillEvolutionSchema,
		Timeout:      e.cfg.StepTimeout(),
		Model:        e.modelForAdapter(execAdapter.Name()),
	}

	attemptID := uuid.NewString()
	started := time.Now().UTC()
	var verdict SkillEvolutionVerdict
	resp, rawJSON, err := e.runStructuredWithRetry(ctx, execAdapter, req, &verdict)
	finished := time.Now().UTC()

	artPath := filepath.Join(e.cfg.RunDir(run.ID), fmt.Sprintf("08-skill-evolution-%d.json", run.RetryCount))
	if rawJSON != "" {
		_ = runner.WriteAtomicBytes(artPath, []byte(rawJSON))
	}

	// TC-06: Meta-Hacking Guard — verify ONLY skills/ was mutated in central repo!
	if err == nil && gitErr == nil {
		if afterPorcelain, afterErr := runner.GitStatusPorcelain(ctx, e.cfg.CentralRepoDir); afterErr == nil {
			afterPaths := runner.GitChangedPaths(afterPorcelain)
			if boundErr := executor.VerifyAllowedWritePaths(beforePaths, afterPaths, []string{"skills/"}); boundErr != nil {
				err = boundErr
			}
		}
	}

	_ = e.store.RecordStageAttempt(ctx, store.StageAttempt{
		ID:           attemptID,
		RunID:        run.ID,
		Stage:        "stage-4-skill-evolution",
		Role:         "skill-curator",
		Adapter:      execAdapter.Name(),
		SessionID:    sessionIDOf(resp),
		Status:       statusFromErr(err),
		ExitCode:     exitCodeOf(resp),
		VerdictJSON:  rawJSON,
		ArtifactPath: artPath,
		StartedAt:    started,
		FinishedAt:   &finished,
	})
	return err
}

// runStructuredWithRetry parses the JSON verdict and performs 1 format-correction retry on malformed output.
func (e *Engine) runStructuredWithRetry(ctx context.Context, execAdapter executor.AgentExecutor, req executor.Request, out any) (*executor.Response, string, error) {
	var lastResp *executor.Response
	var lastErr error

	for attempt := 0; attempt < 2; attempt++ {
		curReq := req
		if attempt == 1 {
			curReq.Prompt = req.Prompt + "\n\nPREVIOUS RESPONSE WAS NOT VALID JSON. Output ONLY a single valid JSON object strictly matching the schema."
		}
		resp, err := execAdapter.Run(ctx, curReq)
		lastResp = resp
		if err != nil {
			return resp, "", err
		}

		candidate := resp.Structured
		if candidate == "" {
			candidate, err = executor.ExtractJSONObject(resp.TextOutput)
			if err != nil {
				lastErr = fmt.Errorf("extract structured JSON: %w", err)
				continue
			}
		}
		if err := json.Unmarshal([]byte(candidate), out); err != nil {
			lastErr = fmt.Errorf("unmarshal structured verdict: %w", err)
			continue
		}
		return resp, candidate, nil
	}
	return lastResp, "", lastErr
}

func (e *Engine) moveToWaitingHuman(ctx context.Context, projectID, runID string, from store.RunState, reason, attachment string) {
	_ = e.store.TransitionRunState(ctx, runID, []store.RunState{from}, store.StateWaitingHuman, reason, "system", "waiting_human", map[string]string{"reason": reason})
	e.notifyProjectChannels(ctx, projectID, fmt.Sprintf("[WAITING HUMAN] Run `%s`\n%s\n\nSend `/retry` or `/abort`.", runID, reason), attachment, []ActionButton{
		{Label: "Retry Stage 2", Data: "/retry"},
		{Label: "Abort Run", Data: "/abort"},
	})
}

func (e *Engine) notifyProjectChannels(ctx context.Context, projectID, text, attachment string, actions []ActionButton) {
	for _, ch := range []string{"telegram", "local"} {
		b, err := e.store.GetChatBindingByProject(ctx, ch, projectID)
		if err != nil {
			continue
		}
		e.notify(ctx, OutgoingMessage{
			Channel:    b.Channel,
			ChatID:     b.ChatID,
			Text:       text,
			Attachment: attachment,
			Actions:    actions,
		})
	}
}

func (e *Engine) drainRepoLockQueue(ctx context.Context, repoLockKey string) {
	next, err := e.store.NextQueuedRunForRepoLock(ctx, repoLockKey)
	if err != nil || next == nil {
		return
	}
	e.dispatchRun(next.ID)
}

func (e *Engine) resolveSkills(names ...string) []string {
	skillsDir := e.cfg.SkillsDir()
	if skillsDir == "" {
		return nil
	}
	var out []string
	for _, n := range names {
		p := filepath.Join(skillsDir, n, "SKILL.md")
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func (e *Engine) modelForAdapter(name string) string {
	switch name {
	case "claude":
		return e.cfg.Models.ClaudeModel
	case "gemini":
		return e.cfg.Models.GeminiModel
	default:
		return ""
	}
}

func appendPlanningTurn(path, userText, replyText string) {
	existing, _ := os.ReadFile(path)
	var b strings.Builder
	b.Write(existing)
	b.WriteString(fmt.Sprintf("## User\n%s\n\n## Planner\n%s\n\n", userText, replyText))
	_ = runner.WriteAtomicBytes(path, []byte(b.String()))
}

func sessionIDOf(resp *executor.Response) string {
	if resp == nil {
		return ""
	}
	return resp.SessionID
}

func exitCodeOf(resp *executor.Response) int {
	if resp == nil {
		return -1
	}
	return resp.ExitCode
}

func statusFromErr(err error) string {
	if err != nil {
		return "ERROR"
	}
	return "OK"
}
