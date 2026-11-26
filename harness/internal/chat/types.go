package chat

import (
	"github.com/carlos-dev-yang/dev-automation/harness/internal/orchestrator"
)

// ChatIntakeSchema forces the frontline ChatResponder (default: gpt-6-luna) to either answer trivial chat directly
// or hand off a clean, task-agnostic intent envelope to the central Orchestrator (default: claude-haiku-5-5).
const ChatIntakeSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["needs_orchestration", "normalized_intent", "project_hint", "direct_reply"],
  "properties": {
    "needs_orchestration": {"type": "boolean"},
    "normalized_intent": {"type": "string"},
    "project_hint": {"type": "string"},
    "direct_reply": {"type": "string"}
  }
}`

// ChatRequest is an incoming user utterance received by the frontline ChatResponder.
type ChatRequest struct {
	MessageID string
	Channel   string
	ChatID    string
	UserID    string
	Text      string
	SessionID string
	Metadata  map[string]string
}

// IntakeResult is the structured intake classification from ChatResponder before delegating to Orchestrator.
type IntakeResult struct {
	NeedsOrchestration bool   `json:"needs_orchestration"`
	NormalizedIntent   string `json:"normalized_intent"`
	ProjectHint        string `json:"project_hint,omitempty"`
	DirectReply        string `json:"direct_reply,omitempty"`
}

// ChatResponse is the user-ready chat message produced by ChatResponder, along with optional orchestration metadata.
type ChatResponse struct {
	Text          string                            `json:"text"`
	SessionID     string                            `json:"session_id,omitempty"`
	ChatModel     string                            `json:"chat_model"`
	Orchestrated  bool                              `json:"orchestrated"`
	Orchestration *orchestrator.OrchestrationResult `json:"orchestration,omitempty"`
}
