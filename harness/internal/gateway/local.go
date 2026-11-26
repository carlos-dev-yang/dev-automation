package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/protocol"
)

// LocalCLIGateway provides an interactive local terminal gateway wired through protocol.Transport,
// exercising the exact same protocol contract as Telegram and HTTP adapters.
type LocalCLIGateway struct {
	transport protocol.Transport
	channel   string
	chatID    string
	userID    string
	in        io.Reader
	out       io.Writer
	mu        sync.Mutex
}

func NewLocalCLIGateway(transport protocol.Transport, chatID, userID string, in io.Reader, out io.Writer) *LocalCLIGateway {
	if chatID == "" {
		chatID = "local-default"
	}
	if userID == "" {
		userID = "local-operator"
	}
	return &LocalCLIGateway{
		transport: transport,
		channel:   "local",
		chatID:    chatID,
		userID:    userID,
		in:        in,
		out:       out,
	}
}

// Notify prints asynchronous protocol events cleanly.
func (g *LocalCLIGateway) Notify(_ context.Context, msg protocol.EgressMessage) {
	if msg.Channel != g.channel || msg.ChatID != g.chatID {
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

// RunOne executes a single command/message through protocol.Transport and writes the reply.
func (g *LocalCLIGateway) RunOne(ctx context.Context, text string) error {
	reply, err := g.transport.Send(ctx, protocol.IngressMessage{
		Channel: g.channel,
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

// RunInteractive starts a REPL connected to protocol.Transport (local in-process or remote HTTP).
func (g *LocalCLIGateway) RunInteractive(ctx context.Context) error {
	events, err := g.transport.Subscribe(ctx, g.channel)
	if err == nil {
		go func() {
			for ev := range events {
				g.Notify(ctx, ev)
			}
		}()
	}

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
		reply, err := g.transport.Send(ctx, protocol.IngressMessage{
			Channel: g.channel,
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

func (g *LocalCLIGateway) printReply(reply protocol.EgressMessage) {
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
