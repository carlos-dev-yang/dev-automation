package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHarnessConfigDefaultsAndEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("RD_HARNESS_HOME", tmp)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-env-123")
	t.Setenv("TELEGRAM_BOT_TOKEN", "env-token-123")

	cfg, err := LoadHarnessConfig(filepath.Join(tmp, "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("LoadHarnessConfig failed: %v", err)
	}
	if cfg.RuntimeDir != tmp {
		t.Fatalf("expected runtime dir %q, got %q", tmp, cfg.RuntimeDir)
	}
	if cfg.Roles.Planner != "gemini" || cfg.Roles.Implementer != "claude" || cfg.Roles.GoldenA != "claude" || cfg.Roles.GoldenB != "gemini" || cfg.Roles.Remediation != "gemini" || cfg.Roles.SkillCurator != "gemini" {
		t.Fatalf("unexpected default roles: %+v", cfg.Roles)
	}
	if cfg.APIs.AnthropicAPIKey != "sk-ant-env-123" {
		t.Fatalf("expected env anthropic key, got %q", cfg.APIs.AnthropicAPIKey)
	}
	// Core LoadHarnessConfig must NOT load channel adapter secrets automatically.
	if cfg.Telegram.BotToken != "" {
		t.Fatalf("expected core config to ignore TELEGRAM_BOT_TOKEN until adapter loads it, got %q", cfg.Telegram.BotToken)
	}
	if err := LoadTelegramAdapterEnv(cfg); err != nil {
		t.Fatalf("LoadTelegramAdapterEnv failed: %v", err)
	}
	if cfg.Telegram.BotToken != "env-token-123" {
		t.Fatalf("expected adapter loader to read env bot token, got %q", cfg.Telegram.BotToken)
	}
	if cfg.IsUserAllowed(999) {
		t.Fatalf("empty whitelist must deny all users")
	}
}

func TestWriteAndLoadHarnessConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("RD_HARNESS_HOME", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")

	cfgPath := filepath.Join(tmp, "config.yaml")
	in := DefaultHarnessConfig()
	in.RuntimeDir = tmp
	in.CentralRepoDir = "/tmp/dev-automation"
	in.Telegram.BotToken = "file-token"
	in.Telegram.AllowedUserIDs = []int64{1001, 2002}

	if err := WriteConfig(cfgPath, in); err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions on config file, got %o", info.Mode().Perm())
	}

	out, err := LoadHarnessConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadHarnessConfig failed: %v", err)
	}
	if !out.IsUserAllowed(1001) || out.IsUserAllowed(3003) {
		t.Fatalf("whitelist check mismatch")
	}
	if out.Telegram.BotToken != "file-token" {
		t.Fatalf("expected file-token, got %q", out.Telegram.BotToken)
	}
}

func TestLoadProjectConfig(t *testing.T) {
	projDir := filepath.Join(t.TempDir(), "sample-app")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Missing rd-project.yaml returns default with exists=false
	pc, exists, err := LoadProjectConfig(projDir)
	if err != nil || exists {
		t.Fatalf("expected missing project config fallback, got exists=%v err=%v", exists, err)
	}
	if pc.ProjectID != "sample-app" || pc.RepoLockKey != "sample-app" {
		t.Fatalf("unexpected fallback project config: %+v", pc)
	}

	yamlContent := `project_id: app-factory
repo_lock_key: app-factory-mono
commands:
  static_check: "pnpm typecheck && pnpm lint"
  test: "pnpm test"
`
	if err := os.WriteFile(filepath.Join(projDir, "rd-project.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, exists, err = LoadProjectConfig(projDir)
	if err != nil || !exists {
		t.Fatalf("expected loaded project config, got exists=%v err=%v", exists, err)
	}
	if pc.RepoLockKey != "app-factory-mono" || pc.Commands["test"] != "pnpm test" || pc.Commands["status"] == "" {
		t.Fatalf("unexpected parsed project config: %+v", pc)
	}
}

func TestLoadDotEnvAndPrecedence(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("RD_HARNESS_HOME", tmp)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "proc-token-wins")
	t.Setenv("TELEGRAM_ALLOWED_USER_IDS", "")

	// Core .env holds only core secrets (and even if someone accidentally puts TELEGRAM_* here, core ignores it).
	envFile := filepath.Join(tmp, ".env")
	coreContent := `# comment line
ANTHROPIC_API_KEY="sk-ant-from-dotenv"
TELEGRAM_BOT_TOKEN=should-be-ignored-in-core
`
	if err := os.WriteFile(envFile, []byte(coreContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// Adapter-isolated telegram.env holds Telegram credentials.
	tgEnvFile := filepath.Join(tmp, "adapters", "telegram.env")
	_ = os.MkdirAll(filepath.Dir(tgEnvFile), 0o700)
	tgContent := `TELEGRAM_BOT_TOKEN=dotenv-token-loses
export TELEGRAM_ALLOWED_USER_IDS=111, 222
`
	if err := os.WriteFile(tgEnvFile, []byte(tgContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadHarnessConfig(filepath.Join(tmp, "config.yaml"))
	if err != nil {
		t.Fatalf("LoadHarnessConfig failed: %v", err)
	}
	if cfg.APIs.AnthropicAPIKey != "sk-ant-from-dotenv" {
		t.Fatalf("expected ANTHROPIC_API_KEY from core .env, got %q", cfg.APIs.AnthropicAPIKey)
	}
	if cfg.Telegram.BotToken != "" {
		t.Fatalf("core .env must not load TELEGRAM_BOT_TOKEN, got %q", cfg.Telegram.BotToken)
	}

	if err := LoadTelegramAdapterEnv(cfg); err != nil {
		t.Fatalf("LoadTelegramAdapterEnv failed: %v", err)
	}
	if cfg.Telegram.BotToken != "proc-token-wins" {
		t.Fatalf("expected process env to override telegram.env, got %q", cfg.Telegram.BotToken)
	}
	if !cfg.IsUserAllowed(111) || !cfg.IsUserAllowed(222) || cfg.IsUserAllowed(333) {
		t.Fatalf("expected TELEGRAM_ALLOWED_USER_IDS parsed from telegram.env, got %v", cfg.Telegram.AllowedUserIDs)
	}
	if cfg.Conversation.Orchestrator.Adapter != "anthropic-api" || cfg.Conversation.Orchestrator.Model != "claude-haiku-5-5" {
		t.Fatalf("unexpected default orchestrator binding: %+v", cfg.Conversation.Orchestrator)
	}
}
