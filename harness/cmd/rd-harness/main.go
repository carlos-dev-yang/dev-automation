package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/core"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/executor"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/gateway"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/protocol"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/runner"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch subcmd {
	case "init-config":
		err = cmdInitConfig(args)
	case "init-project":
		err = cmdInitProject(args)
	case "auth":
		err = cmdAuth(args)
	case "chat":
		err = cmdChat(args)
	case "send":
		err = cmdSend(args)
	case "serve":
		err = cmdServe(args)
	case "telegram":
		err = cmdTelegram(args)
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", subcmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`rd-harness — Go thin workflow harness with external HTTP/SSE protocol + two-tier chat + Telegram adapter

Usage:
  rd-harness init-config [--config <path>] [--central <dev-automation-dir>]
  rd-harness init-project --project <sibling-dir> [--dry-run] [--config <path>]
  rd-harness auth chatgpt [--device-auth] [--status] [--config <path>]
  rd-harness serve [--protocol-only] [--addr <host:port>] [--mock] [--config <path>]
  rd-harness telegram [--endpoint http://127.0.0.1:8787] [--token <auth>] [--config <path>]
  rd-harness chat [--endpoint <url>] [--chat-id <id>] [--mock] [--config <path>]
  rd-harness send [--endpoint <url>] [--channel <ch>] [--chat-id <id>] [--json] [--mock] [--config <path>] "<message>"`)
}

func detectCentralRepo() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "dev-automation" {
			if st, err := os.Stat(filepath.Join(dir, "skills")); err == nil && st.IsDir() {
				return dir
			}
		}
	}
	return ""
}

func wireBroker(eng *core.Engine) *protocol.Broker {
	broker := protocol.NewBroker(eng)
	eng.SetNotifier(func(_ context.Context, msg core.OutgoingMessage) {
		broker.Publish(core.ToProtocolEgress(msg, ""))
	})
	return broker
}

func cmdInitConfig(args []string) error {
	fs := flag.NewFlagSet("init-config", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path (default ~/.dev-automation/config.yaml)")
	central := fs.String("central", detectCentralRepo(), "path to dev-automation repo")
	_ = fs.Parse(args)

	cfg := config.DefaultHarnessConfig()
	if *central != "" {
		if abs, err := filepath.Abs(*central); err == nil {
			cfg.CentralRepoDir = abs
		}
	}
	target := *cfgPath
	if target == "" {
		target = filepath.Join(cfg.RuntimeDir, "config.yaml")
	}
	if err := config.WriteConfig(target, cfg); err != nil {
		return err
	}
	envPath := cfg.EnvPath()
	createdEnv, err := config.EnsureEnvTemplate(envPath)
	if err != nil {
		return err
	}
	fmt.Printf("Wrote harness config to %s (0600)\n", target)
	if createdEnv {
		fmt.Printf("Wrote local secrets template to %s (0600)\n", envPath)
	} else {
		fmt.Printf("Kept existing secrets file at %s\n", envPath)
	}
	fmt.Printf("Conversation roles: chat_responder=%s/%s, orchestrator=%s/%s\n",
		cfg.Conversation.ChatResponder.Adapter, cfg.Conversation.ChatResponder.Model,
		cfg.Conversation.Orchestrator.Adapter, cfg.Conversation.Orchestrator.Model)
	fmt.Println("Next:")
	fmt.Printf("  1) Add ANTHROPIC_API_KEY in %s (for Orchestrator: claude-haiku-5-5)\n", envPath)
	fmt.Println("  2) Authenticate ChatGPT subscription into harness auth dir: rd-harness auth chatgpt --device-auth")
	fmt.Println("  3) Test external protocol server first: rd-harness serve --protocol-only --mock")
	fmt.Printf("  4) When ready for Telegram adapter: rd-harness telegram --init (creates %s)\n", cfg.TelegramEnvPath())
	return nil
}

// cmdAuth manages harness-internal provider subscription authentication stored outside the git repo (~/.dev-automation/auth/).
func cmdAuth(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: rd-harness auth chatgpt [--device-auth] [--status] [--config <path>]")
	}
	provider := strings.ToLower(strings.TrimSpace(args[0]))
	if provider != "chatgpt" && provider != "codex" {
		return fmt.Errorf("unsupported auth provider %q (supported: chatgpt)", provider)
	}

	fs := flag.NewFlagSet("auth chatgpt", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	deviceAuth := fs.Bool("device-auth", false, "use OAuth device code login flow (recommended for headless/remote terminals)")
	statusOnly := fs.Bool("status", false, "show stored ChatGPT subscription auth status")
	_ = fs.Parse(args[1:])

	cfg, err := config.LoadHarnessConfig(*cfgPath)
	if err != nil {
		return err
	}
	authDir := cfg.CodexAuthDir()
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		return fmt.Errorf("create harness codex auth dir: %w", err)
	}

	if *statusOnly {
		fmt.Printf("Harness internal auth dir: %s\n", authDir)
		cmd := exec.Command("codex", "login", "status")
		cmd.Env = append(os.Environ(), "CODEX_HOME="+authDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("  [harness-internal] not logged in (%s)\n", strings.TrimSpace(string(out)))
		} else {
			fmt.Printf("  [harness-internal] %s\n", strings.TrimSpace(string(out)))
		}

		globalCmd := exec.Command("codex", "login", "status")
		if gout, gerr := globalCmd.CombinedOutput(); gerr == nil {
			fmt.Printf("  [global ~/.codex]  %s\n", strings.TrimSpace(string(gout)))
		}
		return nil
	}

	loginArgs := []string{"login"}
	if *deviceAuth {
		loginArgs = append(loginArgs, "--device-auth")
	}
	fmt.Printf("Starting ChatGPT subscription login (storing credentials in %s)...\n", authDir)
	cmd := exec.Command("codex", loginArgs...)
	cmd.Env = append(os.Environ(), "CODEX_HOME="+authDir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cmdInitProject(args []string) error {
	fs := flag.NewFlagSet("init-project", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	projectDir := fs.String("project", "", "target sibling project directory")
	dryRun := fs.Bool("dry-run", false, "check layout and conflicts without creating links")
	_ = fs.Parse(args)

	if *projectDir == "" {
		return errors.New("missing required flag: --project <path>")
	}
	cfg, err := config.LoadHarnessConfig(*cfgPath)
	if err != nil {
		return err
	}
	if cfg.CentralRepoDir == "" {
		cfg.CentralRepoDir = detectCentralRepo()
	}
	if cfg.CentralRepoDir == "" {
		return errors.New("central_repo_dir not set (run from dev-automation or set in config.yaml)")
	}

	statuses, linkErr := runner.EnsureSkillLinks(cfg.CentralRepoDir, *projectDir, *dryRun)
	for _, st := range statuses {
		fmt.Printf("[%s] %s -> %s (%s)\n", st.Status, st.LinkPath, st.Target, st.Message)
	}
	if linkErr != nil {
		return linkErr
	}

	absProj, err := filepath.Abs(*projectDir)
	if err != nil {
		return err
	}
	pc, exists, err := config.LoadProjectConfig(absProj)
	if err != nil {
		return err
	}
	if !exists {
		if *dryRun {
			fmt.Printf("[DRY_RUN] Would scaffold %s/rd-project.yaml\n", absProj)
		} else {
			sample := fmt.Sprintf("project_id: %s\nrepo_lock_key: %s\ncommands:\n  status: \"git status -sb\"\n  static_check: \"\"\n  test: \"\"\n  build_check: \"\"\n", pc.ProjectID, pc.RepoLockKey)
			if err := os.WriteFile(filepath.Join(absProj, "rd-project.yaml"), []byte(sample), 0o644); err != nil {
				return err
			}
			fmt.Printf("[CREATED] %s/rd-project.yaml (fill in project CLI commands)\n", absProj)
		}
	} else {
		fmt.Printf("[OK] rd-project.yaml found (project_id=%s, repo_lock_key=%s)\n", pc.ProjectID, pc.RepoLockKey)
	}
	return nil
}

func buildRegistry(cfg *config.HarnessConfig, useMock bool) *executor.Registry {
	if !useMock {
		return executor.NewRegistry(
			executor.NewAnthropicAPIAdapter(cfg.APIs.AnthropicAPIKey, cfg.APIs.AnthropicBaseURL),
			executor.NewClaudeAdapter(),
			executor.NewGeminiAdapter(),
			executor.NewCodexAdapterWithBinary(cfg.Models.CodexBinary, cfg.CodexAuthDir()),
			executor.NewPendingProviderAdapter("openai", "use adapter 'codex' for ChatGPT subscription or wire direct OpenAI API later"),
		)
	}
	codexMock := executor.NewMockAdapter("codex", func(_ context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "Frontline ChatResponder presenting") {
			return &executor.Response{
				Adapter:    "codex",
				SessionID:  "mock-luna-1",
				TextOutput: "[mock gpt-6-luna] 중앙 오케스트레이터(claude-haiku-5-5)가 작업을 우선순위별로 정리했습니다. 다음 순서 작업부터 진행할 수 있습니다.",
			}, nil
		}
		return &executor.Response{
			Adapter:    "codex",
			SessionID:  "mock-luna-1",
			Structured: `{"needs_orchestration":true,"normalized_intent":"Handle incoming user requirement via orchestrator","project_hint":""}`,
		}, nil
	})
	openaiMock := executor.NewMockAdapter("openai", codexMock.Handler)

	orchestratorMockHandler := func(_ context.Context, req executor.Request) (*executor.Response, error) {
		return &executor.Response{
			Adapter:    "anthropic-api",
			Structured: `{"action":"DISPATCH_TASKS","summary":"Prioritized user request into sequential steps","selected_node":"default","selected_skills":["design-feature-spec"],"tasks":[{"id":"T-1","title":"Inspect context & requirements","priority":1,"order":1,"status":"IN_PROGRESS"},{"id":"T-2","title":"Execute primary task","priority":2,"order":2,"status":"PENDING"}],"remaining_count":2,"next_task_id":"T-1"}`,
		}, nil
	}
	anthropicAPIMock := executor.NewMockAdapter("anthropic-api", orchestratorMockHandler)

	claudeMock := executor.NewMockAdapter("claude", func(ctx context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "Central Orchestrator") {
			return orchestratorMockHandler(ctx, req)
		}
		if strings.Contains(req.SystemPrompt, "Stage 2") {
			return &executor.Response{
				Adapter:    "claude",
				Structured: `{"status":"READY_FOR_STAGE_3","summary":"[mock] implemented slice","changed_files":[],"compliance_evidence":["mock-evidence"]}`,
			}, nil
		}
		return &executor.Response{
			Adapter:    "claude",
			Structured: `{"result":"PASS","summary":"[mock] Golden Set A passed","failures":[],"unverified_scope":[]}`,
		}, nil
	})
	geminiMock := executor.NewMockAdapter("gemini", func(_ context.Context, req executor.Request) (*executor.Response, error) {
		if strings.Contains(req.SystemPrompt, "conversational AI assistant") {
			return &executor.Response{Adapter: "gemini", SessionID: "mock-chat-1", TextOutput: fmt.Sprintf("[mock gemini] Echo: %s", req.Prompt)}, nil
		}
		if strings.Contains(req.SystemPrompt, "Stage 1 Planner") {
			return &executor.Response{Adapter: "gemini", SessionID: "mock-plan-1", TextOutput: "[mock gemini planner] Requirements captured. Run /gate0 when ready."}, nil
		}
		if strings.Contains(req.SystemPrompt, "Gate 0") {
			return &executor.Response{
				Adapter:    "gemini",
				Structured: `{"result":"PASS","summary":"[mock] Gate 0 passed","open_decisions":[],"roadmap":["Slice 1"],"acceptance_tcs":["TC-1"]}`,
			}, nil
		}
		if strings.Contains(req.SystemPrompt, "Golden Set B") {
			return &executor.Response{
				Adapter:    "gemini",
				Structured: `{"result":"PASS","critical_count":0,"medium_count":0,"low_count":0,"findings":[],"skill_compliance_notes":["[mock] compliant"]}`,
			}, nil
		}
		return &executor.Response{
			Adapter:    "gemini",
			Structured: `{"sequence":"CODE_CORRECTION_ONLY","summary":"[mock] remediation","root_causes":["RC-1"],"code_directive":"fix","anti_hacking_constraints":[]}`,
		}, nil
	})
	return executor.NewRegistry(anthropicAPIMock, claudeMock, geminiMock, codexMock, openaiMock)
}

func loadRuntime(cfgPath string, useMock bool) (*config.HarnessConfig, *store.Store, *executor.Registry, error) {
	cfg, err := config.LoadHarnessConfig(cfgPath)
	if err != nil {
		return nil, nil, nil, err
	}
	if cfg.CentralRepoDir == "" {
		cfg.CentralRepoDir = detectCentralRepo()
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return nil, nil, nil, err
	}
	return cfg, st, buildRegistry(cfg, useMock), nil
}

func cmdChat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	endpoint := fs.String("endpoint", "", "connect to a running protocol server (e.g. http://127.0.0.1:8787)")
	token := fs.String("token", "", "optional bearer token for protocol server")
	chatID := fs.String("chat-id", "local-default", "local chat room ID")
	useMock := fs.Bool("mock", false, "use deterministic MockAdapter for offline pipeline testing")
	_ = fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if *endpoint != "" {
		client := protocol.NewHTTPClient(*endpoint, *token)
		if st, err := client.Health(ctx); err != nil {
			return fmt.Errorf("protocol health check failed: %w", err)
		} else {
			fmt.Printf("[protocol] connected to %s (version=%s, chat=%s, orchestrator=%s)\n", *endpoint, st.ProtocolVersion, st.ChatResponder, st.Orchestrator)
		}
		localGW := gateway.NewLocalCLIGateway(client, *chatID, "local-operator", os.Stdin, os.Stdout)
		return localGW.RunInteractive(ctx)
	}

	cfg, st, reg, err := loadRuntime(*cfgPath, *useMock)
	if err != nil {
		return err
	}
	defer st.Close()

	eng := core.NewEngine(cfg, st, reg, nil)
	broker := wireBroker(eng)
	localGW := gateway.NewLocalCLIGateway(broker, *chatID, "local-operator", os.Stdin, os.Stdout)

	if recovered, err := eng.RecoverOnStartup(ctx); err == nil && len(recovered) > 0 {
		fmt.Printf("[recovery] moved %d orphaned run(s) to WAITING_HUMAN\n", len(recovered))
	}
	return localGW.RunInteractive(ctx)
}

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	endpoint := fs.String("endpoint", "", "send over external HTTP protocol server (e.g. http://127.0.0.1:8787)")
	token := fs.String("token", "", "optional bearer token for protocol server")
	channel := fs.String("channel", "local", "channel identifier (e.g. local, http, telegram)")
	chatID := fs.String("chat-id", "local-default", "chat room ID")
	userID := fs.String("user-id", "local-operator", "user ID")
	jsonOut := fs.Bool("json", false, "print raw protocol EgressMessage JSON")
	useMock := fs.Bool("mock", false, "use deterministic MockAdapter")
	syncExec := fs.Bool("sync", true, "wait for approved run execution synchronously when /approve is sent (in-process mode)")
	_ = fs.Parse(args)

	if fs.NArg() == 0 {
		return errors.New("usage: rd-harness send [flags] \"<message>\"")
	}
	text := strings.Join(fs.Args(), " ")
	ctx := context.Background()

	if *endpoint != "" {
		client := protocol.NewHTTPClient(*endpoint, *token)
		reply, err := client.Send(ctx, protocol.IngressMessage{
			Channel: *channel,
			ChatID:  *chatID,
			UserID:  *userID,
			Text:    text,
		})
		if err != nil {
			return err
		}
		if *jsonOut {
			data, _ := json.MarshalIndent(reply, "", "  ")
			fmt.Println(string(data))
		} else {
			fmt.Println(reply.Text)
		}
		return nil
	}

	cfg, st, reg, err := loadRuntime(*cfgPath, *useMock)
	if err != nil {
		return err
	}
	defer st.Close()

	eng := core.NewEngine(cfg, st, reg, nil)
	if *syncExec {
		eng.AsyncDispatch = false
	}
	broker := wireBroker(eng)
	localGW := gateway.NewLocalCLIGateway(broker, *chatID, *userID, strings.NewReader(""), os.Stdout)

	if *jsonOut {
		reply, err := broker.Send(ctx, protocol.IngressMessage{
			Channel: *channel,
			ChatID:  *chatID,
			UserID:  *userID,
			Text:    text,
		})
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(reply, "", "  ")
		fmt.Println(string(data))
	} else {
		if err := localGW.RunOne(ctx, text); err != nil {
			return err
		}
	}

	if *syncExec && (strings.HasPrefix(strings.TrimSpace(text), "/approve") || strings.HasPrefix(strings.TrimSpace(text), "/retry")) {
		if proj, err := st.GetBoundProject(ctx, "local", *chatID); err == nil {
			if run, err := st.GetLatestRunForProject(ctx, proj.ID); err == nil && run.State == store.StateQueued {
				return eng.ExecuteRun(ctx, run.ID)
			}
		}
	}
	return nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	addr := fs.String("addr", "", "override protocol HTTP listen address (default from config: 127.0.0.1:8787)")
	protocolOnly := fs.Bool("protocol-only", false, "run only the external HTTP/SSE protocol server without Telegram adapter")
	useMock := fs.Bool("mock", false, "use deterministic MockAdapter for offline protocol/Telegram testing")
	_ = fs.Parse(args)

	cfg, st, reg, err := loadRuntime(*cfgPath, *useMock)
	if err != nil {
		return err
	}
	defer st.Close()

	listenAddr := cfg.Protocol.ListenAddr
	if strings.TrimSpace(*addr) != "" {
		listenAddr = strings.TrimSpace(*addr)
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)
	eng := core.NewEngine(cfg, st, reg, nil)
	broker := wireBroker(eng)
	protoSrv := protocol.NewServer(broker, cfg.Protocol.AuthToken, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if recovered, err := eng.RecoverOnStartup(ctx); err != nil {
		return fmt.Errorf("recover orphaned runs: %w", err)
	} else if len(recovered) > 0 {
		logger.Printf("[recovery] moved %d orphaned run(s) to WAITING_HUMAN", len(recovered))
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return protoSrv.ListenAndServe(gCtx, listenAddr)
	})

	if !*protocolOnly {
		if err := config.LoadTelegramAdapterEnv(cfg); err != nil {
			return err
		}
	}
	if !*protocolOnly && strings.TrimSpace(cfg.Telegram.BotToken) != "" {
		tgGW, err := gateway.NewTelegramGateway(cfg, broker, logger)
		if err != nil {
			return err
		}
		g.Go(func() error {
			return tgGW.Run(gCtx)
		})
	} else if !*protocolOnly {
		logger.Printf("[telegram] disabled (configure %s or TELEGRAM_BOT_TOKEN to enable); running external protocol server only at http://%s", cfg.TelegramEnvPath(), listenAddr)
	}

	return g.Wait()
}

// cmdTelegram runs the Telegram adapter as an independent external module attached to a running harness over HTTP/SSE protocol.
func cmdTelegram(args []string) error {
	fs := flag.NewFlagSet("telegram", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	initOnly := fs.Bool("init", false, "create ~/.dev-automation/adapters/telegram.env template (0600) and exit")
	endpoint := fs.String("endpoint", "", "harness protocol endpoint (default http://<protocol.listen_addr>)")
	token := fs.String("token", "", "optional bearer token for protocol server")
	_ = fs.Parse(args)

	cfg, err := config.LoadHarnessConfig(*cfgPath)
	if err != nil {
		return err
	}
	if *initOnly {
		tgEnv := cfg.TelegramEnvPath()
		created, err := config.EnsureTelegramEnvTemplate(tgEnv)
		if err != nil {
			return err
		}
		if created {
			fmt.Printf("Wrote Telegram adapter secrets template to %s (0600)\n", tgEnv)
		} else {
			fmt.Printf("Kept existing Telegram adapter secrets at %s\n", tgEnv)
		}
		return nil
	}
	if err := config.LoadTelegramAdapterEnv(cfg); err != nil {
		return err
	}
	target := strings.TrimSpace(*endpoint)
	if target == "" {
		target = "http://" + cfg.Protocol.ListenAddr
	}
	authTok := strings.TrimSpace(*token)
	if authTok == "" {
		authTok = cfg.Protocol.AuthToken
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)
	client := protocol.NewHTTPClient(target, authTok)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := client.Health(ctx)
	if err != nil {
		return fmt.Errorf("cannot reach harness protocol server at %s: %w", target, err)
	}
	logger.Printf("[telegram-bridge] connected to protocol server %s (version=%s, chat=%s, orchestrator=%s)", target, st.ProtocolVersion, st.ChatResponder, st.Orchestrator)

	tgGW, err := gateway.NewTelegramGateway(cfg, client, logger)
	if err != nil {
		return err
	}
	return tgGW.Run(ctx)
}
