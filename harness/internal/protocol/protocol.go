package protocol

import (
	"context"
	"time"
)

const Version = "v1"

// IngressMessage is the channel-agnostic external payload sent into the harness.
// Any external module (Telegram, HTTP/curl, Slack, Web UI, Local CLI) speaks this schema.
type IngressMessage struct {
	MessageID string            `json:"message_id,omitempty"`
	Channel   string            `json:"channel"`
	ChatID    string            `json:"chat_id"`
	UserID    string            `json:"user_id"`
	Text      string            `json:"text"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	SentAt    time.Time         `json:"sent_at,omitempty"`
}

// Action describes an interactive quick-reply action (rendered as inline buttons in Telegram, choices in CLI/Web).
type Action struct {
	Label string `json:"label"`
	Data  string `json:"data"`
}

// EgressMessage is the channel-agnostic response or async event emitted by the harness.
type EgressMessage struct {
	ProtocolVersion string    `json:"protocol_version"`
	MessageID       string    `json:"message_id"`
	ReplyTo         string    `json:"reply_to,omitempty"`
	Channel         string    `json:"channel"`
	ChatID          string    `json:"chat_id"`
	Text            string    `json:"text"`
	Attachment      string    `json:"attachment,omitempty"`
	Actions         []Action  `json:"actions,omitempty"`
	Async           bool      `json:"async,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
}

// HealthStatus reports harness readiness and configured AI adapters over the protocol.
type HealthStatus struct {
	Status          string    `json:"status"`
	ProtocolVersion string    `json:"protocol_version"`
	ChatResponder   string    `json:"chat_responder,omitempty"`
	Orchestrator    string    `json:"orchestrator,omitempty"`
	PlannerAdapter  string    `json:"planner_adapter"`
	Adapters        []string  `json:"adapters"`
	Timestamp       time.Time `json:"timestamp"`
}

// Transport is the unified boundary between external channel modules (Telegram, Local CLI, Slack, etc.)
// and the harness Chat Core. Implementations include in-process Broker and network HTTPClient.
type Transport interface {
	Send(ctx context.Context, msg IngressMessage) (EgressMessage, error)
	Subscribe(ctx context.Context, channel string) (<-chan EgressMessage, error)
	Health(ctx context.Context) (HealthStatus, error)
}
