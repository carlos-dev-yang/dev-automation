package protocol

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MessageHandler is implemented by core.Engine (or a test double) to process normalized IngressMessages.
type MessageHandler interface {
	HandleProtocolMessage(ctx context.Context, msg IngressMessage) (EgressMessage, error)
	ProtocolHealth(ctx context.Context) HealthStatus
}

type subscriber struct {
	id      uint64
	channel string
	ch      chan EgressMessage
}

// Broker is the in-process protocol hub that adapts MessageHandler to Transport
// and fans out async EgressMessages to all subscribed external modules (Telegram, SSE, Local CLI).
type Broker struct {
	handler MessageHandler

	mu     sync.RWMutex
	nextID uint64
	subs   map[uint64]*subscriber
}

func NewBroker(handler MessageHandler) *Broker {
	return &Broker{
		handler: handler,
		subs:    make(map[uint64]*subscriber),
	}
}

// Send normalizes and forwards an external IngressMessage into Chat Core.
func (b *Broker) Send(ctx context.Context, msg IngressMessage) (EgressMessage, error) {
	if b.handler == nil {
		return EgressMessage{}, errors.New("protocol broker has no message handler")
	}
	msg.Channel = strings.TrimSpace(msg.Channel)
	msg.ChatID = strings.TrimSpace(msg.ChatID)
	msg.UserID = strings.TrimSpace(msg.UserID)
	if msg.Channel == "" {
		return EgressMessage{}, errors.New("protocol: channel is required")
	}
	if msg.ChatID == "" {
		return EgressMessage{}, errors.New("protocol: chat_id is required")
	}
	if msg.UserID == "" {
		msg.UserID = msg.Channel + ":anonymous"
	}
	if msg.MessageID == "" {
		msg.MessageID = uuid.NewString()
	}
	if msg.SentAt.IsZero() {
		msg.SentAt = time.Now().UTC()
	}

	reply, err := b.handler.HandleProtocolMessage(ctx, msg)
	if err != nil {
		return EgressMessage{}, err
	}
	if reply.ProtocolVersion == "" {
		reply.ProtocolVersion = Version
	}
	if reply.MessageID == "" {
		reply.MessageID = uuid.NewString()
	}
	if reply.ReplyTo == "" {
		reply.ReplyTo = msg.MessageID
	}
	if reply.Channel == "" {
		reply.Channel = msg.Channel
	}
	if reply.ChatID == "" {
		reply.ChatID = msg.ChatID
	}
	if reply.Timestamp.IsZero() {
		reply.Timestamp = time.Now().UTC()
	}
	return reply, nil
}

// Publish broadcasts an async EgressMessage from Chat Core to matching subscribers.
func (b *Broker) Publish(msg EgressMessage) {
	if msg.ProtocolVersion == "" {
		msg.ProtocolVersion = Version
	}
	if msg.MessageID == "" {
		msg.MessageID = uuid.NewString()
	}
	msg.Async = true
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		if s.channel != "" && s.channel != "*" && s.channel != msg.Channel {
			continue
		}
		select {
		case s.ch <- msg:
		default:
			// Non-blocking drop if a slow subscriber overflows buffer.
		}
	}
}

// Subscribe returns a channel receiving async EgressMessages for the given channel ("" or "*" receives all).
func (b *Broker) Subscribe(ctx context.Context, channel string) (<-chan EgressMessage, error) {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	sub := &subscriber{
		id:      id,
		channel: strings.TrimSpace(channel),
		ch:      make(chan EgressMessage, 64),
	}
	b.subs[id] = sub
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subs, id)
		close(sub.ch)
		b.mu.Unlock()
	}()
	return sub.ch, nil
}

func (b *Broker) Health(ctx context.Context) (HealthStatus, error) {
	if b.handler == nil {
		return HealthStatus{
			Status:          "degraded",
			ProtocolVersion: Version,
			Timestamp:       time.Now().UTC(),
		}, nil
	}
	st := b.handler.ProtocolHealth(ctx)
	if st.ProtocolVersion == "" {
		st.ProtocolVersion = Version
	}
	if st.Timestamp.IsZero() {
		st.Timestamp = time.Now().UTC()
	}
	return st, nil
}
