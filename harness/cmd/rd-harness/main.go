package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/core"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/executor"
	"github.com/carlos-dev-yang/dev-automation/harness/internal/gateway"
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
	case "chat":
		err = cmdChat(args)
	case "send":
		err = cmdSend(args)
	case "serve":
		err = cmdServe(args)
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
	fmt.Println(`rd-harness — Go thin workflow harness for dev-automation (Stage 1-4 + Telegram)

Usage:
  rd-harness init-config [--config <path>] [--central <dev-automation-dir>]
  rd-harness init-project --project <sibling-dir> [--dry-run] [--config <path>]
  rd-harness chat [--chat-id <id>] [--mock] [--config <path>]
  rd-harness send [--chat-id <id>] [--mock] [--config <path>] "<message>"
  rd-harness serve [--mock] [--config <path>]`)
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
	fmt.Printf("Wrote harness config to %s (0600)\n", target)
	fmt.Println("Next: set telegram.bot_token (or TELEGRAM_BOT_TOKEN) and telegram.allowed_user_ids before running `rd-harness serve`.")
	return nil
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

func buildRegistry(useMock bool) *executor.Registry {
	if !useMock {
		return executor.NewRegistry(
			executor.NewClaudeAdapter(),
			executor.NewGeminiAdapter(),
		)
	}
	claudeMock := executor.NewMockAdapter("claude", func(_ context.Context, req executor.Request) (*executor.Response, error) {
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
	return executor.NewRegistry(claudeMock, geminiMock)
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
	return cfg, st, buildRegistry(useMock), nil
}

func cmdChat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	chatID := fs.String("chat-id", "local-default", "local chat room ID")
	useMock := fs.Bool("mock", false, "use deterministic MockAdapter for offline pipeline testing")
	_ = fs.Parse(args)

	cfg, st, reg, err := loadRuntime(*cfgPath, *useMock)
	if err != nil {
		return err
	}
	defer st.Close()

	var localGW *gateway.LocalCLIGateway
	eng := core.NewEngine(cfg, st, reg, func(ctx context.Context, msg core.OutgoingMessage) {
		if localGW != nil {
			localGW.Notify(ctx, msg)
		}
	})
	localGW = gateway.NewLocalCLIGateway(eng, *chatID, "local-operator", os.Stdin, os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if recovered, err := eng.RecoverOnStartup(ctx); err == nil && len(recovered) > 0 {
		fmt.Printf("[recovery] moved %d orphaned run(s) to WAITING_HUMAN\n", len(recovered))
	}
	return localGW.RunInteractive(ctx)
}

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file path")
	chatID := fs.String("chat-id", "local-default", "local chat room ID")
	useMock := fs.Bool("mock", false, "use deterministic MockAdapter")
	syncExec := fs.Bool("sync", true, "wait for approved run execution synchronously when /approve is sent")
	_ = fs.Parse(args)

	if fs.NArg() == 0 {
		return errors.New("usage: rd-harness send [flags] \"<message>\"")
	}
	text := strings.Join(fs.Args(), " ")

	cfg, st, reg, err := loadRuntime(*cfgPath, *useMock)
	if err != nil {
		return err
	}
	defer st.Close()

	var localGW *gateway.LocalCLIGateway
	eng := core.NewEngine(cfg, st, reg, func(ctx context.Context, msg core.OutgoingMessage) {
		if localGW != nil {
			localGW.Notify(ctx, msg)
		}
	})
	if *syncExec {
		eng.AsyncDispatch = false
	}
	localGW = gateway.NewLocalCLIGateway(eng, *chatID, "local-operator", strings.NewReader(""), os.Stdout)

	ctx := context.Background()
	if err := localGW.RunOne(ctx, text); err != nil {
		return err
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
	useMock := fs.Bool("mock", false, "use deterministic MockAdapter for Telegram bot testing")
	_ = fs.Parse(args)

	cfg, st, reg, err := loadRuntime(*cfgPath, *useMock)
	if err != nil {
		return err
	}
	defer st.Close()

	logger := log.New(os.Stdout, "", log.LstdFlags)
	var tgGW *gateway.TelegramGateway
	eng := core.NewEngine(cfg, st, reg, func(ctx context.Context, msg core.OutgoingMessage) {
		if tgGW != nil {
			tgGW.Notify(ctx, msg)
		}
	})
	tgGW, err = gateway.NewTelegramGateway(cfg, eng, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if recovered, err := eng.RecoverOnStartup(ctx); err != nil {
		return fmt.Errorf("recover orphaned runs: %w", err)
	} else if len(recovered) > 0 {
		logger.Printf("[recovery] moved %d orphaned run(s) to WAITING_HUMAN", len(recovered))
	}

	return tgGW.Run(ctx)
}
