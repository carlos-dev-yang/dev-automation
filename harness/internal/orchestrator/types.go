package orchestrator

import "time"

// OrchestratorDecisionSchema enforces structured output from the central orchestrator model (default: claude-haiku).
const OrchestratorDecisionSchema = `{
  "type": "object",
  "required": ["action", "summary", "selected_node", "selected_skills", "tasks", "remaining_count"],
  "properties": {
    "action": {"type": "string", "enum": ["DISPATCH_TASKS", "ASK_USER", "REPORT_STATUS", "NOOP"]},
    "summary": {"type": "string"},
    "selected_node": {"type": "string"},
    "selected_skills": {"type": "array", "items": {"type": "string"}},
    "tasks": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "title", "priority", "order", "status"],
        "properties": {
          "id": {"type": "string"},
          "title": {"type": "string"},
          "priority": {"type": "integer", "minimum": 1, "maximum": 5},
          "order": {"type": "integer", "minimum": 1},
          "status": {"type": "string", "enum": ["PENDING", "IN_PROGRESS", "BLOCKED", "DONE"]},
          "target_node": {"type": "string"},
          "skills": {"type": "array", "items": {"type": "string"}},
          "depends_on": {"type": "array", "items": {"type": "string"}}
        }
      }
    },
    "remaining_count": {"type": "integer", "minimum": 0},
    "next_task_id": {"type": "string"},
    "question_for_user": {"type": "string"}
  }
}`

// HandoffRequest is the normalized payload passed from the frontline ChatResponder into the central Orchestrator.
type HandoffRequest struct {
	MessageID        string            `json:"message_id,omitempty"`
	Channel          string            `json:"channel"`
	ChatID           string            `json:"chat_id"`
	UserID           string            `json:"user_id"`
	RawText          string            `json:"raw_text"`
	NormalizedIntent string            `json:"normalized_intent"`
	ProjectHint      string            `json:"project_hint,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// TaskItem represents an ordered, prioritized unit of work managed by the central Orchestrator.
type TaskItem struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Priority   int      `json:"priority"` // 1 (highest) .. 5 (lowest)
	Order      int      `json:"order"`    // Sequential execution order
	Status     string   `json:"status"`   // PENDING | IN_PROGRESS | BLOCKED | DONE
	TargetNode string   `json:"target_node,omitempty"`
	Skills     []string `json:"skills,omitempty"`
	DependsOn  []string `json:"depends_on,omitempty"`
}

// BacklogSnapshot captures the current ordered task queue and remaining work calculation for a chat/project scope.
type BacklogSnapshot struct {
	ScopeKey       string     `json:"scope_key"`
	Tasks          []TaskItem `json:"tasks"`
	TotalCount     int        `json:"total_count"`
	CompletedCount int        `json:"completed_count"`
	RemainingCount int        `json:"remaining_count"`
	NextTask       *TaskItem  `json:"next_task,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// OrchestrationDecision is the structured plan returned by the central Orchestrator model.
type OrchestrationDecision struct {
	Action          string     `json:"action"` // DISPATCH_TASKS | ASK_USER | REPORT_STATUS | NOOP
	Summary         string     `json:"summary"`
	SelectedNode    string     `json:"selected_node"`
	SelectedSkills  []string   `json:"selected_skills"`
	Tasks           []TaskItem `json:"tasks"`
	RemainingCount  int        `json:"remaining_count"`
	NextTaskID      string     `json:"next_task_id,omitempty"`
	QuestionForUser string     `json:"question_for_user,omitempty"`
}

// DispatchOutcome records what internal worker/executor produced when a task was dispatched.
type DispatchOutcome struct {
	TaskID  string `json:"task_id"`
	Status  string `json:"status"` // QUEUED | EXECUTED | SKIPPED | FAILED
	Summary string `json:"summary"`
}

// OrchestrationResult is returned to the frontline ChatResponder so it can present a user-friendly chat reply.
type OrchestrationResult struct {
	Decision        OrchestrationDecision `json:"decision"`
	Backlog         BacklogSnapshot       `json:"backlog"`
	Dispatches      []DispatchOutcome     `json:"dispatches,omitempty"`
	ModelUsed       string                `json:"model_used"`
	AvailableNodes  int                   `json:"available_nodes"`
	AvailableSkills int                   `json:"available_skills"`
}

// NodeInfo describes a registered project/context node discoverable by the central Orchestrator.
type NodeInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	RootPath     string   `json:"root_path"`
	RepoLockKey  string   `json:"repo_lock_key,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// SkillSummary is lightweight metadata scanned from skills/*/SKILL.md frontmatter.
type SkillSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Status      string   `json:"status,omitempty"`
	Stages      []string `json:"stages,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	Path        string   `json:"path"`
}

// ContextSnapshot is the context gathered from the node & skill registry before consulting the Orchestrator model.
type ContextSnapshot struct {
	BoundNode *NodeInfo       `json:"bound_node,omitempty"`
	Nodes     []NodeInfo      `json:"nodes"`
	Skills    []SkillSummary  `json:"skills"`
	Backlog   BacklogSnapshot `json:"backlog"`
}
