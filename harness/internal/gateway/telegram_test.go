package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
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

// TC-08: Unauthorized Telegram user_id must be ignored; authorized user can bind, plan, click inline button, and receive chunked messages + attachments.
func TestTelegramGatewayWhitelistAndFlow(t *testing.T) {
	root := t.TempDir()
	centralDir := filepath.Join(root, "dev-automation")
	projDir := filepath.Join(root, "demo-app")
	runtimeDir := filepath.Join(root, "runtime")
	_ = os.MkdirAll(filepath.Join(centralDir, "skills"), 0o755)
	_ = os.MkdirAll(projDir, 0o755)

	cfg := config.DefaultHarnessConfig()
	cfg.RuntimeDir = runtimeDir
	cfg.CentralRepoDir = centralDir
	cfg.Telegram.BotToken = "test-token"
	cfg.Telegram.AllowedUserIDs = []int64{4242}

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	reg := executor.NewRegistry(
		executor.NewMockAdapter("claude", nil),
		executor.NewMockAdapter("gemini", func(_ context.Context, req executor.Request) (*executor.Response, error) {
			if strings.Contains(req.SystemPrompt, "Gate 0") {
				return &executor.Response{
					Adapter:    "gemini",
					Structured: `{"result":"PASS","summary":"Gate 0 green","open_decisions":[],"roadmap":["S1"],"acceptance_tcs":["TC-1"]}`,
				}, nil
			}
			return &executor.Response{Adapter: "gemini", SessionID: "s1", TextOutput: "Plan noted."}, nil
		}),
	)

	eng := core.NewEngine(cfg, st, reg, nil)
	eng.AsyncDispatch = false
	broker := protocol.NewBroker(eng)

	var mu sync.Mutex
	var sentMessages []map[string]any
	var sentDocs int
	var answeredCallbacks int
	var chatActions int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/sendMessage":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			sentMessages = append(sentMessages, payload)
		case "/sendDocument":
			sentDocs++
		case "/answerCallbackQuery":
			answeredCallbacks++
		case "/sendChatAction":
			chatActions++
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	tg, err := NewTelegramGateway(cfg, broker, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	tg.SetBaseURL(srv.URL)

	ctx := context.Background()

	// TC-08: Unauthorized user must be dropped without mutating DB or sending Telegram reply
	tg.HandleUpdate(ctx, tgUpdate{
		UpdateID: 1,
		Message: &tgMessage{
			From: tgUser{ID: 9999},
			Chat: tgChat{ID: -1001},
			Text: "/bind demo-app",
		},
	})
	if _, err := st.GetBoundProject(ctx, "telegram", "-1001"); err == nil {
		t.Fatalf("TC-08 violation: unauthorized user was able to bind project!")
	}
	if len(sentMessages) != 0 {
		t.Fatalf("expected 0 replies to unauthorized user, got %d", len(sentMessages))
	}

	// Authorized user binds project, creates run, and clicks Gate 0 inline button
	tg.HandleUpdate(ctx, tgUpdate{
		UpdateID: 2,
		Message:  &tgMessage{From: tgUser{ID: 4242}, Chat: tgChat{ID: -1001}, Text: "/bind demo-app"},
	})
	tg.HandleUpdate(ctx, tgUpdate{
		UpdateID: 3,
		Message:  &tgMessage{From: tgUser{ID: 4242}, Chat: tgChat{ID: -1001}, Text: "/new Build feature"},
	})
	tg.HandleUpdate(ctx, tgUpdate{
		UpdateID: 4,
		CallbackQuery: &tgCallbackQuery{
			ID:      "cb-1",
			From:    tgUser{ID: 4242},
			Message: &tgMessage{Chat: tgChat{ID: -1001}},
			Data:    "/gate0",
		},
	})

	mu.Lock()
	defer mu.Unlock()
	if answeredCallbacks != 1 {
		t.Fatalf("expected 1 callback answered, got %d", answeredCallbacks)
	}
	if chatActions != 3 {
		t.Fatalf("expected 3 typing chat actions, got %d", chatActions)
	}
	if sentDocs != 1 {
		t.Fatalf("expected 1 gate0 artifact document uploaded, got %d", sentDocs)
	}
	if len(sentMessages) != 3 {
		t.Fatalf("expected 3 messages sent, got %d", len(sentMessages))
	}

	// Verify 4000+ char chunk splitting
	longText := strings.Repeat("line of output\n", 400) // ~6000 chars
	chunks := splitMessage(longText, telegramMaxMessageLen)
	if len(chunks) < 2 {
		t.Fatalf("expected long message to split into >=2 chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > telegramMaxMessageLen {
			t.Fatalf("chunk exceeded telegram limit: %d", len(c))
		}
	}
}

// TestTelegramAdapterOverHTTPProtocol verifies TelegramGateway acting purely as an external module
// talking to the harness over the HTTP/JSON + SSE protocol and receiving Gemini AI responses.
func TestTelegramAdapterOverHTTPProtocol(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultHarnessConfig()
	cfg.RuntimeDir = filepath.Join(root, "rt")
	cfg.Telegram.BotToken = "test-token"
	cfg.Telegram.AllowedUserIDs = []int64{7777}

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	reg := executor.NewRegistry(
		executor.NewMockAdapter("gemini", func(_ context.Context, req executor.Request) (*executor.Response, error) {
			return &executor.Response{
				Adapter:    "gemini",
				SessionID:  "tg-gem-1",
				TextOutput: "[gemini-reply] " + req.Prompt,
			}, nil
		}),
	)

	eng := core.NewEngine(cfg, st, reg, nil)
	broker := protocol.NewBroker(eng)
	eng.SetNotifier(func(_ context.Context, msg core.OutgoingMessage) {
		broker.Publish(core.ToProtocolEgress(msg, ""))
	})

	// Harness exposes external HTTP/SSE protocol server
	protoSrv := protocol.NewServer(broker, "bridge-secret", log.New(io.Discard, "", 0))
	protoHTTP := httptest.NewServer(protoSrv.Handler())
	defer protoHTTP.Close()

	// Fake Telegram API endpoint
	var mu sync.Mutex
	var sentTexts []string
	var delivered = make(chan string, 10)
	tgAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sendMessage":
			var payload struct {
				Text string `json:"text"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			sentTexts = append(sentTexts, payload.Text)
			mu.Unlock()
			delivered <- payload.Text
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer tgAPI.Close()

	// Telegram adapter attaches ONLY via protocol.HTTPClient (no direct Engine reference)
	httpClient := protocol.NewHTTPClient(protoHTTP.URL, "bridge-secret")
	tg, err := NewTelegramGateway(cfg, httpClient, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	tg.SetBaseURL(tgAPI.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. /ping and /ask travel: Telegram -> HTTPClient -> ProtocolServer -> Chat Core -> Gemini -> Telegram sendMessage
	tg.HandleUpdate(ctx, tgUpdate{
		UpdateID: 10,
		Message:  &tgMessage{MessageID: 1, From: tgUser{ID: 7777}, Chat: tgChat{ID: -2002}, Text: "/ping"},
	})
	tg.HandleUpdate(ctx, tgUpdate{
		UpdateID: 11,
		Message:  &tgMessage{MessageID: 2, From: tgUser{ID: 7777}, Chat: tgChat{ID: -2002}, Text: "/ask 테스트 메시지입니다"},
	})

	// 2. Async SSE push from harness arrives at Telegram adapter
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	events, err := httpClient.Subscribe(subCtx, "telegram")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for ev := range events {
			tg.Notify(subCtx, ev)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	broker.Publish(protocol.EgressMessage{
		Channel: "telegram",
		ChatID:  "-2002",
		Text:    "async update via SSE",
	})

	// Expect 3 messages delivered to Telegram API
	for i := 0; i < 3; i++ {
		select {
		case <-delivered:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for message %d/3", i+1)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sentTexts) != 3 {
		t.Fatalf("expected 3 telegram messages, got %d: %v", len(sentTexts), sentTexts)
	}
	if !strings.Contains(sentTexts[0], "pong") {
		t.Fatalf("expected pong reply, got %q", sentTexts[0])
	}
	if sentTexts[1] != "[gemini-reply] 테스트 메시지입니다" {
		t.Fatalf("expected gemini reply over HTTP protocol, got %q", sentTexts[1])
	}
	if sentTexts[2] != "async update via SSE" {
		t.Fatalf("expected async SSE push, got %q", sentTexts[2])
	}
}

func TestLocalCLIGatewayRunOne(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultHarnessConfig()
	cfg.RuntimeDir = filepath.Join(root, "rt")
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	eng := core.NewEngine(cfg, st, executor.NewRegistry(), nil)
	broker := protocol.NewBroker(eng)
	var out bytes.Buffer
	gw := NewLocalCLIGateway(broker, "test-chat", "tester", strings.NewReader(""), &out)
	if err := gw.RunOne(context.Background(), "/help"); err != nil {
		t.Fatalf("RunOne /help failed: %v", err)
	}
	if !strings.Contains(out.String(), "/bind") || !strings.Contains(out.String(), "/ask") {
		t.Fatalf("unexpected /help output: %s", out.String())
	}
}
