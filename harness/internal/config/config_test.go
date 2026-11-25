package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHarnessConfigDefaultsAndEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("RD_HARNESS_HOME", tmp)
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
	if cfg.Telegram.BotToken != "env-token-123" {
		t.Fatalf("expected env bot token, got %q", cfg.Telegram.BotToken)
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
