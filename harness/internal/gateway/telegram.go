package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/protocol"
)

const telegramMaxMessageLen = 4000

// TelegramGateway is an external channel adapter that bridges Telegram Long-Polling
// to the harness exclusively through the channel-agnostic protocol.Transport.
type TelegramGateway struct {
	cfg       *config.HarnessConfig
	transport protocol.Transport
	client    *http.Client
	baseURL   string
	logger    *log.Logger
	offset    int64

	workersMu sync.Mutex
	workers   map[int64]chan tgUpdate
}

func NewTelegramGateway(cfg *config.HarnessConfig, transport protocol.Transport, logger *log.Logger) (*TelegramGateway, error) {
	if strings.TrimSpace(cfg.Telegram.BotToken) == "" {
		return nil, errors.New("telegram bot_token is empty (set in ~/.dev-automation/config.yaml or TELEGRAM_BOT_TOKEN)")
	}
	if transport == nil {
		return nil, errors.New("telegram gateway requires a protocol.Transport")
	}
	if logger == nil {
		logger = log.Default()
	}
	pollSec := cfg.Telegram.PollTimeoutSeconds
	if pollSec <= 0 {
		pollSec = 30
	}
	return &TelegramGateway{
		cfg:       cfg,
		transport: transport,
		baseURL:   "https://api.telegram.org/bot" + cfg.Telegram.BotToken,
		client: &http.Client{
			Timeout: time.Duration(pollSec+15) * time.Second,
		},
		logger:  logger,
		workers: make(map[int64]chan tgUpdate),
	}, nil
}

// SetBaseURL overrides the Telegram API base URL (used in httptest unit tests).
func (g *TelegramGateway) SetBaseURL(url string) {
	g.baseURL = strings.TrimRight(url, "/")
}

// VerifyBot checks token validity via Telegram getMe API before entering the polling loop.
func (g *TelegramGateway) VerifyBot(ctx context.Context) (string, error) {
	resp, _, err := g.callJSON(ctx, "getMe", map[string]any{})
	if err != nil {
		return "", err
	}
	var me struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	}
	if err := json.Unmarshal(resp.Result, &me); err != nil {
		return "", fmt.Errorf("decode getMe: %w", err)
	}
	return fmt.Sprintf("@%s (id=%d)", me.Username, me.ID), nil
}

// Notify delivers an async protocol.EgressMessage to Telegram.
func (g *TelegramGateway) Notify(ctx context.Context, msg protocol.EgressMessage) {
	if msg.Channel != "telegram" || msg.ChatID == "" {
		return
	}
	if err := g.SendOutgoing(ctx, msg); err != nil {
		g.logger.Printf("[telegram] notify error (chat=%s): %v", msg.ChatID, err)
	}
}

// Run subscribes to async protocol events and runs the Telegram Long-Polling loop until ctx is cancelled.
func (g *TelegramGateway) Run(ctx context.Context) error {
	if botIdentity, err := g.VerifyBot(ctx); err != nil {
		g.logger.Printf("[telegram] warning: getMe check failed: %v", err)
	} else {
		g.logger.Printf("[telegram] authenticated as %s", botIdentity)
	}

	events, err := g.transport.Subscribe(ctx, "telegram")
	if err != nil {
		return fmt.Errorf("telegram subscribe protocol events: %w", err)
	}
	go func() {
		for ev := range events {
			g.Notify(ctx, ev)
		}
	}()

	g.logger.Printf("[telegram] long-polling started (allowed_users=%d, poll_timeout=%ds)", len(g.cfg.Telegram.AllowedUserIDs), g.cfg.Telegram.PollTimeoutSeconds)
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, retryAfter, err := g.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			wait := backoff
			if retryAfter > 0 {
				wait = retryAfter
			} else {
				backoff = min(backoff*2, 30*time.Second)
			}
			g.logger.Printf("[telegram] getUpdates error: %v (retrying in %s)", err, wait)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		backoff = time.Second

		for _, u := range updates {
			if u.UpdateID >= g.offset {
				g.offset = u.UpdateID + 1
			}
			g.dispatchUpdate(ctx, u)
		}
	}
}

// dispatchUpdate queues updates per chat_id so slow AI turns in one chat do not block polling or other chats.
func (g *TelegramGateway) dispatchUpdate(ctx context.Context, u tgUpdate) {
	chatID := extractChatID(u)
	if chatID == 0 {
		return
	}
	g.workersMu.Lock()
	ch, ok := g.workers[chatID]
	if !ok {
		ch = make(chan tgUpdate, 32)
		g.workers[chatID] = ch
		go func(queue <-chan tgUpdate) {
			for {
				select {
				case <-ctx.Done():
					return
				case upd := <-queue:
					g.HandleUpdate(ctx, upd)
				}
			}
		}(ch)
	}
	g.workersMu.Unlock()

	select {
	case ch <- u:
	default:
		g.logger.Printf("[telegram] chat=%d worker queue full; dropping update_id=%d", chatID, u.UpdateID)
	}
}

func extractChatID(u tgUpdate) int64 {
	if u.CallbackQuery != nil && u.CallbackQuery.Message != nil {
		return u.CallbackQuery.Message.Chat.ID
	}
	if u.Message != nil {
		return u.Message.Chat.ID
	}
	return 0
}

type tgUpdate struct {
	UpdateID      int64            `json:"update_id"`
	Message       *tgMessage       `json:"message"`
	CallbackQuery *tgCallbackQuery `json:"callback_query"`
}

type tgUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type tgChat struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

type tgMessage struct {
	MessageID int64  `json:"message_id"`
	From      tgUser `json:"from"`
	Chat      tgChat `json:"chat"`
	Text      string `json:"text"`
}

type tgCallbackQuery struct {
	ID      string     `json:"id"`
	From    tgUser     `json:"from"`
	Message *tgMessage `json:"message"`
	Data    string     `json:"data"`
}

type tgAPIResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// HandleUpdate enforces the user_id whitelist (TC-08) and forwards normalized messages over protocol.Transport.
func (g *TelegramGateway) HandleUpdate(ctx context.Context, u tgUpdate) {
	var senderID int64
	var username string
	var chatID int64
	var chatTitle string
	var text string
	var callbackID string
	var msgID string

	if u.CallbackQuery != nil && u.CallbackQuery.Message != nil {
		senderID = u.CallbackQuery.From.ID
		username = u.CallbackQuery.From.Username
		chatID = u.CallbackQuery.Message.Chat.ID
		chatTitle = u.CallbackQuery.Message.Chat.Title
		text = strings.TrimSpace(u.CallbackQuery.Data)
		callbackID = u.CallbackQuery.ID
		msgID = "tg-cb-" + callbackID
	} else if u.Message != nil {
		senderID = u.Message.From.ID
		username = u.Message.From.Username
		chatID = u.Message.Chat.ID
		chatTitle = u.Message.Chat.Title
		text = strings.TrimSpace(u.Message.Text)
		msgID = fmt.Sprintf("tg-%d-%d", chatID, u.Message.MessageID)
	} else {
		return
	}

	if !g.cfg.IsUserAllowed(senderID) {
		g.logger.Printf("[telegram] SECURITY: ignored message from unauthorized user_id=%d chat_id=%d", senderID, chatID)
		return
	}

	if callbackID != "" {
		_ = g.answerCallbackQuery(ctx, callbackID)
	}
	if text == "" {
		return
	}

	chatIDStr := strconv.FormatInt(chatID, 10)
	_ = g.sendChatAction(ctx, chatIDStr, "typing")

	meta := map[string]string{
		"telegram_update_id": strconv.FormatInt(u.UpdateID, 10),
	}
	if username != "" {
		meta["telegram_username"] = username
	}
	if chatTitle != "" {
		meta["telegram_chat_title"] = chatTitle
	}

	reply, err := g.transport.Send(ctx, protocol.IngressMessage{
		MessageID: msgID,
		Channel:   "telegram",
		ChatID:    chatIDStr,
		UserID:    fmt.Sprintf("tg:%d", senderID),
		Text:      text,
		Metadata:  meta,
		SentAt:    time.Now().UTC(),
	})
	if err != nil {
		_ = g.SendOutgoing(ctx, protocol.EgressMessage{
			ProtocolVersion: protocol.Version,
			Channel:         "telegram",
			ChatID:          chatIDStr,
			Text:            fmt.Sprintf("Error: %v", err),
		})
		return
	}
	if err := g.SendOutgoing(ctx, reply); err != nil {
		g.logger.Printf("[telegram] send reply error (chat=%d): %v", chatID, err)
	}
}

func (g *TelegramGateway) getUpdates(ctx context.Context) ([]tgUpdate, time.Duration, error) {
	payload := map[string]any{
		"offset":          g.offset,
		"timeout":         g.cfg.Telegram.PollTimeoutSeconds,
		"allowed_updates": []string{"message", "callback_query"},
	}
	resp, retryAfter, err := g.callJSON(ctx, "getUpdates", payload)
	if err != nil {
		return nil, retryAfter, err
	}
	var updates []tgUpdate
	if err := json.Unmarshal(resp.Result, &updates); err != nil {
		return nil, 0, fmt.Errorf("decode getUpdates: %w", err)
	}
	return updates, 0, nil
}

// SendOutgoing splits messages >4000 chars, attaches inline action buttons to the last chunk, and uploads optional artifact files.
func (g *TelegramGateway) SendOutgoing(ctx context.Context, msg protocol.EgressMessage) error {
	chunks := splitMessage(msg.Text, telegramMaxMessageLen)
	for i, chunk := range chunks {
		payload := map[string]any{
			"chat_id": msg.ChatID,
			"text":    chunk,
		}
		if i == len(chunks)-1 && len(msg.Actions) > 0 {
			var row []map[string]string
			for _, a := range msg.Actions {
				row = append(row, map[string]string{
					"text":          a.Label,
					"callback_data": a.Data,
				})
			}
			payload["reply_markup"] = map[string]any{
				"inline_keyboard": [][]map[string]string{row},
			}
		}
		if _, _, err := g.callJSON(ctx, "sendMessage", payload); err != nil {
			return err
		}
	}

	if msg.Attachment != "" {
		if st, err := os.Stat(msg.Attachment); err == nil && !st.IsDir() && st.Size() > 0 {
			if err := g.sendDocument(ctx, msg.ChatID, msg.Attachment); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *TelegramGateway) sendChatAction(ctx context.Context, chatID, action string) error {
	_, _, err := g.callJSON(ctx, "sendChatAction", map[string]any{
		"chat_id": chatID,
		"action":  action,
	})
	return err
}

func (g *TelegramGateway) answerCallbackQuery(ctx context.Context, callbackID string) error {
	_, _, err := g.callJSON(ctx, "answerCallbackQuery", map[string]any{
		"callback_query_id": callbackID,
	})
	return err
}

func (g *TelegramGateway) callJSON(ctx context.Context, method string, payload any) (*tgAPIResponse, time.Duration, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := g.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, 0, err
	}
	var apiResp tgAPIResponse
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, 0, fmt.Errorf("telegram %s status=%d invalid json: %w", method, res.StatusCode, err)
	}
	if !apiResp.OK {
		var retryAfter time.Duration
		if apiResp.Parameters != nil && apiResp.Parameters.RetryAfter > 0 {
			retryAfter = time.Duration(apiResp.Parameters.RetryAfter) * time.Second
		}
		return &apiResp, retryAfter, fmt.Errorf("telegram %s error %d: %s", method, apiResp.ErrorCode, apiResp.Description)
	}
	return &apiResp, 0, nil
}

func (g *TelegramGateway) sendDocument(ctx context.Context, chatID, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("chat_id", chatID); err != nil {
		return err
	}
	part, err := mw.CreateFormFile("document", filepath.Base(filePath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/sendDocument", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	res, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var apiResp tgAPIResponse
	if err := json.Unmarshal(raw, &apiResp); err != nil || !apiResp.OK {
		return fmt.Errorf("telegram sendDocument failed: status=%d body=%s", res.StatusCode, string(raw))
	}
	return nil
}

func splitMessage(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}
	var chunks []string
	for len(text) > limit {
		cut := strings.LastIndex(text[:limit], "\n")
		if cut <= 0 {
			cut = limit
		}
		chunks = append(chunks, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}
