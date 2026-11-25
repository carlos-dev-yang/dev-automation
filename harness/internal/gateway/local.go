package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/core"
)

// LocalCLIGateway provides an interactive local terminal gateway wired to the exact same Chat Core as Telegram.
type LocalCLIGateway struct {
	engine *core.Engine
	chatID string
	userID string
	in     io.Reader
	out    io.Writer
	mu     sync.Mutex
}

func NewLocalCLIGateway(engine *core.Engine, chatID, userID string, in io.Reader, out io.Writer) *LocalCLIGateway {
	if chatID == "" {
		chatID = "local-default"
	}
	if userID == "" {
		userID = "local-operator"
	}
	return &LocalCLIGateway{
		engine: engine,
		chatID: chatID,
		userID: userID,
		in:     in,
		out:    out,
	}
}

// Notify prints asynchronous pipeline updates from core.Engine cleanly.
func (g *LocalCLIGateway) Notify(_ context.Context, msg core.OutgoingMessage) {
	if msg.Channel != "local" || msg.ChatID != g.chatID {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	fmt.Fprintf(g.out, "\n[async] %s\n", msg.Text)
	if msg.Attachment != "" {
		fmt.Fprintf(g.out, "[artifact] %s\n", msg.Attachment)
	}
	if len(msg.Actions) > 0 {
		var labels []string
		for _, a := range msg.Actions {
			labels = append(labels, fmt.Sprintf("%s (%s)", a.Label, a.Data))
		}
		fmt.Fprintf(g.out, "[actions] %s\n", strings.Join(labels, " | "))
	}
	fmt.Fprint(g.out, "> ")
}

// RunOne executes a single command/message against Chat Core and writes the reply.
func (g *LocalCLIGateway) RunOne(ctx context.Context, text string) error {
	reply, err := g.engine.HandleMessage(ctx, core.IncomingMessage{
		Channel: "local",
		ChatID:  g.chatID,
		UserID:  g.userID,
		Text:    text,
	})
	if err != nil {
		return err
	}
	g.printReply(reply)
	return nil
}

// RunInteractive starts a REPL for local end-to-end testing of the harness workflow.
func (g *LocalCLIGateway) RunInteractive(ctx context.Context) error {
	g.mu.Lock()
	fmt.Fprintf(g.out, "rd-harness local runner (chat=%s). Type /help for commands, /quit to exit.\n> ", g.chatID)
	g.mu.Unlock()

	scanner := bufio.NewScanner(g.in)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			g.prompt()
			continue
		}
		if line == "/quit" || line == "/exit" {
			return nil
		}
		reply, err := g.engine.HandleMessage(ctx, core.IncomingMessage{
			Channel: "local",
			ChatID:  g.chatID,
			UserID:  g.userID,
			Text:    line,
		})
		if err != nil {
			g.mu.Lock()
			fmt.Fprintf(g.out, "[error] %v\n> ", err)
			g.mu.Unlock()
			continue
		}
		g.printReply(reply)
		g.prompt()
	}
	return scanner.Err()
}

func (g *LocalCLIGateway) printReply(reply core.OutgoingMessage) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fmt.Fprintln(g.out, reply.Text)
	if reply.Attachment != "" {
		fmt.Fprintf(g.out, "[artifact] %s\n", reply.Attachment)
	}
	if len(reply.Actions) > 0 {
		var labels []string
		for _, a := range reply.Actions {
			labels = append(labels, fmt.Sprintf("%s (%s)", a.Label, a.Data))
		}
		fmt.Fprintf(g.out, "[actions] %s\n", strings.Join(labels, " | "))
	}
}

func (g *LocalCLIGateway) prompt() {
	g.mu.Lock()
	defer g.mu.Unlock()
	fmt.Fprint(g.out, "> ")
}
