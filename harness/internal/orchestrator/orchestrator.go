package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/executor"
)

// TaskDispatcher bridges Orchestrator task decisions to internal execution subsystems
// (e.g. Stage 1-4 Engine, No-AI Runner, or specialized sub-agents).
type TaskDispatcher interface {
	Dispatch(ctx context.Context, req HandoffRequest, node *NodeInfo, task TaskItem) (DispatchOutcome, error)
}

// PlanOnlyDispatcher is the default lightweight dispatcher that queues prioritized tasks
// without executing mutations until worker execution hooks are attached.
type PlanOnlyDispatcher struct{}

func (PlanOnlyDispatcher) Dispatch(_ context.Context, _ HandoffRequest, node *NodeInfo, task TaskItem) (DispatchOutcome, error) {
	target := task.TargetNode
	if target == "" && node != nil {
		target = node.Name
	}
	if target == "" {
		target = "unassigned"
	}
	return DispatchOutcome{
		TaskID:  task.ID,
		Status:  "QUEUED",
		Summary: fmt.Sprintf("Queued task %q (order=%d, priority=P%d, node=%s)", task.Title, task.Order, task.Priority, target),
	}, nil
}

// Service is the central orchestration brain (default model: claude-haiku).
// It is decoupled from user-facing chat formatting and owns context/node/skill discovery,
// task ordering, priority assignment, remaining-work calculation, and internal dispatching.
type Service struct {
	cfg        *config.HarnessConfig
	execReg    *executor.Registry
	ctxReg     ContextRegistry
	dispatcher TaskDispatcher

	mu       sync.RWMutex
	backlogs map[string]BacklogSnapshot
}

func NewService(cfg *config.HarnessConfig, execReg *executor.Registry, ctxReg ContextRegistry, dispatcher TaskDispatcher) *Service {
	if dispatcher == nil {
		dispatcher = PlanOnlyDispatcher{}
	}
	return &Service{
		cfg:        cfg,
		execReg:    execReg,
		ctxReg:     ctxReg,
		dispatcher: dispatcher,
		backlogs:   make(map[string]BacklogSnapshot),
	}
}

// ModelBinding returns the configured (adapter, model) pair for central orchestration.
func (s *Service) ModelBinding() config.RoleModel {
	return s.cfg.Conversation.Orchestrator
}

// SnapshotContext gathers discoverable nodes, skills, and current task backlog for a chat scope.
func (s *Service) SnapshotContext(ctx context.Context, channel, chatID string) (ContextSnapshot, error) {
	var bound *NodeInfo
	var nodes []NodeInfo
	var skills []SkillSummary

	if s.ctxReg != nil {
		bound, _ = s.ctxReg.ResolveBoundNode(ctx, channel, chatID)
		if n, err := s.ctxReg.ListNodes(ctx); err == nil {
			nodes = n
		}
		if sk, err := s.ctxReg.ListSkills(ctx); err == nil {
			skills = sk
		}
	}

	scopeKey := scopeKeyFor(channel, chatID, bound)
	backlog := s.GetBacklog(scopeKey)
	return ContextSnapshot{
		BoundNode: bound,
		Nodes:     nodes,
		Skills:    skills,
		Backlog:   backlog,
	}, nil
}

// GetBacklog returns the latest prioritized task backlog and remaining count for scopeKey.
func (s *Service) GetBacklog(scopeKey string) BacklogSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if snap, ok := s.backlogs[scopeKey]; ok {
		return snap
	}
	return BacklogSnapshot{ScopeKey: scopeKey}
}

// HandleHandoff receives a normalized requirement from ChatResponder, consults the Orchestrator AI
// (default: claude-haiku), updates prioritized/ordered backlog + remaining work, and dispatches next work.
func (s *Service) HandleHandoff(ctx context.Context, req HandoffRequest) (OrchestrationResult, error) {
	binding := s.ModelBinding()
	execAdapter, err := s.execReg.Get(binding.Adapter)
	if err != nil {
		return OrchestrationResult{}, fmt.Errorf("orchestrator adapter %q unavailable: %w", binding.Adapter, err)
	}

	snap, err := s.SnapshotContext(ctx, req.Channel, req.ChatID)
	if err != nil {
		return OrchestrationResult{}, err
	}

	workDir := s.cfg.CentralRepoDir
	if snap.BoundNode != nil && snap.BoundNode.RootPath != "" {
		workDir = snap.BoundNode.RootPath
	}
	if workDir == "" {
		workDir = s.cfg.RuntimeDir
	}

	ctxPayload, _ := json.MarshalIndent(snap, "", "  ")
	handoffPayload, _ := json.MarshalIndent(req, "", "  ")

	execReq := executor.Request{
		WorkDir: workDir,
		SystemPrompt: strings.Join([]string{
			"You are the Central Orchestrator inside rd-harness.",
			"You do NOT talk directly to the end user; a separate ChatResponder translates your structured result into chat.",
			"Your responsibilities:",
			"1. Inspect the registered project/context nodes and central skill catalog.",
			"2. Decompose the incoming request into sequential tasks with clear execution order (order=1..N) and priority (priority=1 highest .. 5 lowest).",
			"3. Calculate remaining_count accurately (count of tasks whose status is not DONE) and select next_task_id.",
			"4. Choose action=DISPATCH_TASKS when tasks can be queued/dispatched, ASK_USER when critical input is missing, or REPORT_STATUS for status queries.",
			"Do NOT modify files.",
		}, "\n"),
		Prompt:     fmt.Sprintf("=== Handoff From ChatResponder ===\n%s\n\n=== Registry & Current Backlog Context ===\n%s", string(handoffPayload), string(ctxPayload)),
		Mode:       executor.ModeReadOnly,
		JSONSchema: OrchestratorDecisionSchema,
		Timeout:    s.cfg.StepTimeout(),
		Model:      binding.Model,
	}

	resp, err := execAdapter.Run(ctx, execReq)
	if err != nil {
		return OrchestrationResult{}, fmt.Errorf("orchestrator (%s/%s) failed: %w", binding.Adapter, binding.Model, err)
	}

	rawJSON := resp.Structured
	if rawJSON == "" {
		rawJSON, err = executor.ExtractJSONObject(resp.TextOutput)
		if err != nil {
			return OrchestrationResult{}, fmt.Errorf("orchestrator returned invalid structured decision: %w", err)
		}
	}

	var decision OrchestrationDecision
	if err := json.Unmarshal([]byte(rawJSON), &decision); err != nil {
		return OrchestrationResult{}, fmt.Errorf("unmarshal orchestrator decision: %w", err)
	}

	// Deterministic normalization: sort by (order, priority) and recompute exact remaining work count.
	scopeKey := scopeKeyFor(req.Channel, req.ChatID, snap.BoundNode)
	updatedBacklog := computeBacklog(scopeKey, decision.Tasks, snap.Backlog.Tasks)
	decision.Tasks = updatedBacklog.Tasks
	decision.RemainingCount = updatedBacklog.RemainingCount
	if updatedBacklog.NextTask != nil && decision.NextTaskID == "" {
		decision.NextTaskID = updatedBacklog.NextTask.ID
	}

	s.mu.Lock()
	s.backlogs[scopeKey] = updatedBacklog
	s.mu.Unlock()

	var dispatches []DispatchOutcome
	if decision.Action == "DISPATCH_TASKS" && updatedBacklog.NextTask != nil && s.dispatcher != nil {
		targetNode := findNode(snap.Nodes, decision.SelectedNode, snap.BoundNode)
		if outcome, dErr := s.dispatcher.Dispatch(ctx, req, targetNode, *updatedBacklog.NextTask); dErr == nil {
			dispatches = append(dispatches, outcome)
		} else {
			dispatches = append(dispatches, DispatchOutcome{
				TaskID:  updatedBacklog.NextTask.ID,
				Status:  "FAILED",
				Summary: dErr.Error(),
			})
		}
	}

	return OrchestrationResult{
		Decision:        decision,
		Backlog:         updatedBacklog,
		Dispatches:      dispatches,
		ModelUsed:       fmt.Sprintf("%s/%s", binding.Adapter, binding.Model),
		AvailableNodes:  len(snap.Nodes),
		AvailableSkills: len(snap.Skills),
	}, nil
}

func scopeKeyFor(channel, chatID string, bound *NodeInfo) string {
	if bound != nil && bound.Name != "" {
		return "project:" + bound.Name
	}
	return fmt.Sprintf("chat:%s:%s", channel, chatID)
}

func computeBacklog(scopeKey string, incoming, previous []TaskItem) BacklogSnapshot {
	tasks := incoming
	if len(tasks) == 0 {
		tasks = previous
	}
	normalized := make([]TaskItem, len(tasks))
	copy(normalized, tasks)

	sort.SliceStable(normalized, func(i, j int) bool {
		if normalized[i].Order != normalized[j].Order {
			return normalized[i].Order < normalized[j].Order
		}
		return normalized[i].Priority < normalized[j].Priority
	})

	completed := 0
	remaining := 0
	var nextTask *TaskItem
	for i := range normalized {
		if normalized[i].Status == "" {
			normalized[i].Status = "PENDING"
		}
		if normalized[i].Priority <= 0 {
			normalized[i].Priority = 3
		}
		if normalized[i].Order <= 0 {
			normalized[i].Order = i + 1
		}
		if normalized[i].Status == "DONE" {
			completed++
		} else {
			remaining++
			if nextTask == nil && (normalized[i].Status == "PENDING" || normalized[i].Status == "IN_PROGRESS") {
				tCopy := normalized[i]
				nextTask = &tCopy
			}
		}
	}

	return BacklogSnapshot{
		ScopeKey:       scopeKey,
		Tasks:          normalized,
		TotalCount:     len(normalized),
		CompletedCount: completed,
		RemainingCount: remaining,
		NextTask:       nextTask,
		UpdatedAt:      time.Now().UTC(),
	}
}

func findNode(nodes []NodeInfo, selectedName string, bound *NodeInfo) *NodeInfo {
	if selectedName != "" {
		for i := range nodes {
			if nodes[i].Name == selectedName || nodes[i].ID == selectedName {
				return &nodes[i]
			}
		}
	}
	return bound
}
