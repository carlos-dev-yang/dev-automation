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

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/core"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/executor"
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

	var mu sync.Mutex
	var sentMessages []map[string]any
	var sentDocs int
	var answeredCallbacks int

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
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	tg, err := NewTelegramGateway(cfg, eng, log.New(io.Discard, "", 0))
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
	var out bytes.Buffer
	gw := NewLocalCLIGateway(eng, "test-chat", "tester", strings.NewReader(""), &out)
	if err := gw.RunOne(context.Background(), "/help"); err != nil {
		t.Fatalf("RunOne /help failed: %v", err)
	}
	if !strings.Contains(out.String(), "/bind") || !strings.Contains(out.String(), "/gate0") {
		t.Fatalf("unexpected /help output: %s", out.String())
	}
}
