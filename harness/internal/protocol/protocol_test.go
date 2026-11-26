package protocol_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/core"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/executor"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/protocol"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/store"
)

func TestHTTPProtocolRoundTripGeminiChatAndSSE(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultHarnessConfig()
	cfg.RuntimeDir = filepath.Join(root, "rt")
	cfg.CentralRepoDir = filepath.Join(root, "dev-automation")
	cfg.Models.GeminiCloudProject = "demo-gcp-proj"

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var mu sync.Mutex
	var seenReqs []executor.Request
	geminiMock := executor.NewMockAdapter("gemini", func(_ context.Context, req executor.Request) (*executor.Response, error) {
		mu.Lock()
		seenReqs = append(seenReqs, req)
		mu.Unlock()
		return &executor.Response{
			Adapter:    "gemini",
			SessionID:  "gem-sess-42",
			TextOutput: "Gemini says: " + req.Prompt,
		}, nil
	})
	reg := executor.NewRegistry(geminiMock)

	eng := core.NewEngine(cfg, st, reg, nil)
	broker := protocol.NewBroker(eng)
	eng.SetNotifier(func(_ context.Context, msg core.OutgoingMessage) {
		broker.Publish(core.ToProtocolEgress(msg, ""))
	})

	srv := protocol.NewServer(broker, "secret-token-123", nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Auth guard check: request without valid Bearer token must be rejected (401).
	unauthClient := protocol.NewHTTPClient(ts.URL, "wrong-token")
	if _, err := unauthClient.Health(ctx); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 unauthorized, got %v", err)
	}
	// Also verify raw HTTP 401 status
	res, err := http.Get(ts.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", res.StatusCode)
	}

	// 2. Authorized Health check over HTTP protocol.
	client := protocol.NewHTTPClient(ts.URL, "secret-token-123")
	health, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if health.Status != "ok" || health.ProtocolVersion != protocol.Version || health.PlannerAdapter != "gemini" {
		t.Fatalf("unexpected health response: %+v", health)
	}

	// 3. No-AI /ping round-trip over POST /v1/messages.
	pingResp, err := client.Send(ctx, protocol.IngressMessage{
		Channel: "http",
		ChatID:  "room-1",
		UserID:  "tester",
		Text:    "/ping",
	})
	if err != nil {
		t.Fatalf("/ping failed: %v", err)
	}
	if !strings.Contains(pingResp.Text, "pong") || !strings.Contains(pingResp.Text, "planner=gemini") {
		t.Fatalf("unexpected /ping response: %+v", pingResp)
	}

	// 4. AI chat turn 1 (/ask) -> invokes Gemini with GOOGLE_CLOUD_PROJECT and saves session_id.
	ask1, err := client.Send(ctx, protocol.IngressMessage{
		Channel: "http",
		ChatID:  "room-1",
		UserID:  "tester",
		Text:    "/ask hello gemini",
	})
	if err != nil {
		t.Fatalf("/ask turn 1 failed: %v", err)
	}
	if ask1.Text != "Gemini says: hello gemini" {
		t.Fatalf("unexpected /ask turn 1 text: %q", ask1.Text)
	}

	// 5. AI chat turn 2 -> verifies persisted session_id is resumed for the same chat room.
	ask2, err := client.Send(ctx, protocol.IngressMessage{
		Channel: "http",
		ChatID:  "room-1",
		UserID:  "tester",
		Text:    "/ask second turn",
	})
	if err != nil {
		t.Fatalf("/ask turn 2 failed: %v", err)
	}
	if ask2.Text != "Gemini says: second turn" {
		t.Fatalf("unexpected /ask turn 2 text: %q", ask2.Text)
	}

	mu.Lock()
	if len(seenReqs) != 2 {
		t.Fatalf("expected 2 gemini calls, got %d", len(seenReqs))
	}
	if seenReqs[0].SessionID != "" || seenReqs[1].SessionID != "gem-sess-42" {
		t.Fatalf("expected session continuity '' -> 'gem-sess-42', got %q -> %q", seenReqs[0].SessionID, seenReqs[1].SessionID)
	}
	if len(seenReqs[0].ExtraEnv) == 0 || seenReqs[0].ExtraEnv[0] != "GOOGLE_CLOUD_PROJECT=demo-gcp-proj" {
		t.Fatalf("expected GOOGLE_CLOUD_PROJECT env passed to gemini, got %v", seenReqs[0].ExtraEnv)
	}
	mu.Unlock()

	// 6. /reset clears session so next /ask starts fresh.
	if _, err := client.Send(ctx, protocol.IngressMessage{Channel: "http", ChatID: "room-1", UserID: "tester", Text: "/reset"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Send(ctx, protocol.IngressMessage{Channel: "http", ChatID: "room-1", UserID: "tester", Text: "/ask fresh"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if seenReqs[2].SessionID != "" {
		t.Fatalf("expected empty session after /reset, got %q", seenReqs[2].SessionID)
	}
	mu.Unlock()

	// 7. SSE async egress subscription receives Broker.Publish events over GET /v1/events.
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	events, err := client.Subscribe(subCtx, "http")
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	broker.Publish(protocol.EgressMessage{
		Channel: "http",
		ChatID:  "room-1",
		Text:    "async push notification",
	})

	select {
	case ev := <-events:
		if ev.Text != "async push notification" || !ev.Async || ev.ChatID != "room-1" {
			t.Fatalf("unexpected SSE event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE async egress message")
	}
}
