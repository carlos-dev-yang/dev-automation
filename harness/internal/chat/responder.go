package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/executor"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/orchestrator"
)

// OrchestratorClient is the narrow boundary between frontline chat and central orchestration.
type OrchestratorClient interface {
	HandleHandoff(ctx context.Context, req orchestrator.HandoffRequest) (orchestrator.OrchestrationResult, error)
}

// Responder is the user-facing conversational module (default model: gpt-6-luna).
// It stays agnostic to internal task execution details and focuses strictly on:
//  1. Receiving user messages and handing structured requirements to the central Orchestrator.
//  2. Translating Orchestrator results, remaining-work summaries, and follow-up questions back into natural chat replies.
type Responder struct {
	cfg          *config.HarnessConfig
	execReg      *executor.Registry
	orchestrator OrchestratorClient
}

func NewResponder(cfg *config.HarnessConfig, execReg *executor.Registry, orch OrchestratorClient) *Responder {
	return &Responder{
		cfg:          cfg,
		execReg:      execReg,
		orchestrator: orch,
	}
}

// ModelBinding returns the configured (adapter, model) pair for frontline user conversation.
func (r *Responder) ModelBinding() config.RoleModel {
	return r.cfg.Conversation.ChatResponder
}

// HandleChat executes the two-tier flow:
// User -> ChatResponder (intake) -> Central Orchestrator (routing/priority/remaining work) -> ChatResponder (chat presentation).
func (r *Responder) HandleChat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	binding := r.ModelBinding()
	modelLabel := fmt.Sprintf("%s/%s", binding.Adapter, binding.Model)

	execAdapter, err := r.execReg.Get(binding.Adapter)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("chat responder adapter %q unavailable: %w", binding.Adapter, err)
	}

	workDir := r.cfg.RuntimeDir
	if r.cfg.CentralRepoDir != "" {
		workDir = r.cfg.CentralRepoDir
	}

	// Step 1: Frontline intake (classify & normalize user intent without performing orchestration itself)
	intakeReq := executor.Request{
		WorkDir: workDir,
		SystemPrompt: strings.Join([]string{
			"You are the Frontline ChatResponder for rd-harness.",
			"You are NOT the task orchestrator and must NOT plan internal workflows or invent execution states.",
			"Your job in this step is ONLY to:",
			"1. Decide if the user's message requires internal orchestration (needs_orchestration=true for any work request, project/skill/task inquiry, status check, or decision).",
			"2. Summarize the user's requirement cleanly in normalized_intent and extract project_hint if mentioned.",
			"3. Only set needs_orchestration=false with direct_reply for simple greetings or chitchat.",
			"Do NOT modify files.",
		}, "\n"),
		Prompt:     fmt.Sprintf("Channel: %s\nChatID: %s\nUserID: %s\nUser Message:\n%s", req.Channel, req.ChatID, req.UserID, req.Text),
		Mode:       executor.ModeReadOnly,
		SessionID:  req.SessionID,
		JSONSchema: ChatIntakeSchema,
		Timeout:    r.cfg.StepTimeout(),
		Model:      binding.Model,
	}

	intakeResp, err := execAdapter.Run(ctx, intakeReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("chat responder intake (%s) failed: %w", modelLabel, err)
	}

	intake, parseErr := parseIntake(intakeResp)
	if parseErr != nil {
		// Graceful fallback: forward raw user text to Orchestrator if intake model returned plain text.
		intake = IntakeResult{
			NeedsOrchestration: true,
			NormalizedIntent:   strings.TrimSpace(req.Text),
		}
	}

	if !intake.NeedsOrchestration && strings.TrimSpace(intake.DirectReply) != "" {
		return ChatResponse{
			Text:         strings.TrimSpace(intake.DirectReply),
			SessionID:    intakeResp.SessionID,
			ChatModel:    modelLabel,
			Orchestrated: false,
		}, nil
	}

	if r.orchestrator == nil {
		return ChatResponse{
			Text:         strings.TrimSpace(intake.DirectReply),
			SessionID:    intakeResp.SessionID,
			ChatModel:    modelLabel,
			Orchestrated: false,
		}, nil
	}

	// Step 2: Hand off normalized intent to Central Orchestrator (default: claude-haiku)
	handoff := orchestrator.HandoffRequest{
		MessageID:        req.MessageID,
		Channel:          req.Channel,
		ChatID:           req.ChatID,
		UserID:           req.UserID,
		RawText:          req.Text,
		NormalizedIntent: intake.NormalizedIntent,
		ProjectHint:      intake.ProjectHint,
		Metadata:         req.Metadata,
	}
	orchRes, err := r.orchestrator.HandleHandoff(ctx, handoff)
	if err != nil {
		return ChatResponse{}, err
	}

	// Step 3: Translate Orchestrator structured result back into a natural chat reply via ChatResponder.
	sessionID := intakeResp.SessionID
	if sessionID == "" {
		sessionID = req.SessionID
	}
	presented, newSession, err := r.presentResult(ctx, execAdapter, binding.Model, workDir, sessionID, req.Text, orchRes)
	if err != nil {
		// Fallback to deterministic concise summary if presentation call fails, so orchestration progress is never lost.
		presented = fallbackSummary(orchRes)
	}
	if newSession != "" {
		sessionID = newSession
	}

	return ChatResponse{
		Text:          presented,
		SessionID:     sessionID,
		ChatModel:     modelLabel,
		Orchestrated:  true,
		Orchestration: &orchRes,
	}, nil
}

// RelayResult converts an asynchronous internal OrchestrationResult into a user-facing chat update.
func (r *Responder) RelayResult(ctx context.Context, orchRes orchestrator.OrchestrationResult) (ChatResponse, error) {
	binding := r.ModelBinding()
	modelLabel := fmt.Sprintf("%s/%s", binding.Adapter, binding.Model)

	execAdapter, err := r.execReg.Get(binding.Adapter)
	if err != nil {
		return ChatResponse{
			Text:          fallbackSummary(orchRes),
			ChatModel:     modelLabel,
			Orchestrated:  true,
			Orchestration: &orchRes,
		}, nil
	}

	workDir := r.cfg.RuntimeDir
	if r.cfg.CentralRepoDir != "" {
		workDir = r.cfg.CentralRepoDir
	}

	text, sess, err := r.presentResult(ctx, execAdapter, binding.Model, workDir, "", "(async background update)", orchRes)
	if err != nil {
		text = fallbackSummary(orchRes)
	}
	return ChatResponse{
		Text:          text,
		SessionID:     sess,
		ChatModel:     modelLabel,
		Orchestrated:  true,
		Orchestration: &orchRes,
	}, nil
}

func (r *Responder) presentResult(ctx context.Context, execAdapter executor.AgentExecutor, model, workDir, sessionID, userText string, res orchestrator.OrchestrationResult) (string, string, error) {
	payload, _ := json.MarshalIndent(res, "", "  ")
	presReq := executor.Request{
		WorkDir: workDir,
		SystemPrompt: strings.Join([]string{
			"You are the Frontline ChatResponder presenting an internal Orchestrator result to the user.",
			"Translate the structured orchestration data into a concise, clear chat reply in the user's language.",
			"Always reflect the exact task order, remaining task count, and any question_for_user without inventing unverified facts.",
			"Do NOT modify files.",
		}, "\n"),
		Prompt:    fmt.Sprintf("Original User Message:\n%s\n\nCentral Orchestrator Result (JSON):\n%s", userText, string(payload)),
		Mode:      executor.ModeReadOnly,
		SessionID: sessionID,
		Timeout:   r.cfg.StepTimeout(),
		Model:     model,
	}

	resp, err := execAdapter.Run(ctx, presReq)
	if err != nil {
		return "", sessionID, err
	}
	text := strings.TrimSpace(resp.TextOutput)
	if text == "" {
		text = fallbackSummary(res)
	}
	return text, resp.SessionID, nil
}

func parseIntake(resp *executor.Response) (IntakeResult, error) {
	if resp == nil {
		return IntakeResult{}, fmt.Errorf("empty intake response")
	}
	raw := resp.Structured
	if raw == "" {
		var err error
		raw, err = executor.ExtractJSONObject(resp.TextOutput)
		if err != nil {
			return IntakeResult{}, err
		}
	}
	var out IntakeResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return IntakeResult{}, err
	}
	return out, nil
}

func fallbackSummary(res orchestrator.OrchestrationResult) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%s] %s\n", res.Decision.Action, res.Decision.Summary))
	b.WriteString(fmt.Sprintf("- Remaining tasks: %d (done %d / total %d)\n", res.Backlog.RemainingCount, res.Backlog.CompletedCount, res.Backlog.TotalCount))
	if res.Backlog.NextTask != nil {
		b.WriteString(fmt.Sprintf("- Next task: #%d [P%d] %s\n", res.Backlog.NextTask.Order, res.Backlog.NextTask.Priority, res.Backlog.NextTask.Title))
	}
	if res.Decision.QuestionForUser != "" {
		b.WriteString("\nQuestion: " + res.Decision.QuestionForUser)
	}
	return strings.TrimSpace(b.String())
}
