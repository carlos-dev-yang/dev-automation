package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// RoleConfig maps workflow roles to AgentExecutor adapter names.
// Default mapping follows D-13: gemini for planning/review/remediation, claude for implementation/golden-a (separate OS process).
type RoleConfig struct {
	Planner      string `yaml:"planner"`
	Implementer  string `yaml:"implementer"`
	GoldenA      string `yaml:"golden_a"`
	GoldenB      string `yaml:"golden_b"`
	Remediation  string `yaml:"remediation"`
	SkillCurator string `yaml:"skill_curator"`
}

// TelegramConfig holds Telegram Long-Polling gateway configuration.
type TelegramConfig struct {
	BotToken           string  `yaml:"bot_token"`
	AllowedUserIDs     []int64 `yaml:"allowed_user_ids"`
	PollTimeoutSeconds int     `yaml:"poll_timeout_seconds"`
}

// AdapterModels holds optional model and budget overrides per CLI adapter.
type AdapterModels struct {
	ClaudeModel        string  `yaml:"claude_model,omitempty"`
	GeminiModel        string  `yaml:"gemini_model,omitempty"`
	ClaudeMaxBudgetUSD float64 `yaml:"claude_max_budget_usd,omitempty"`
}

// HarnessConfig is the runtime configuration stored outside the repo (default ~/.dev-automation/config.yaml).
type HarnessConfig struct {
	RuntimeDir         string         `yaml:"runtime_dir"`
	CentralRepoDir     string         `yaml:"central_repo_dir"`
	MaxRetries         int            `yaml:"max_retries"`
	StepTimeoutSeconds int            `yaml:"step_timeout_seconds"`
	Roles              RoleConfig     `yaml:"roles"`
	Models             AdapterModels  `yaml:"models"`
	Telegram           TelegramConfig `yaml:"telegram"`
}

// ProjectConfig defines the CLI-Operable contract in <project-root>/rd-project.yaml.
type ProjectConfig struct {
	ProjectID      string            `yaml:"project_id"`
	RepoLockKey    string            `yaml:"repo_lock_key"`
	Commands       map[string]string `yaml:"commands"`
	ProtectedPaths []string          `yaml:"protected_paths,omitempty"`
}

// DefaultHarnessConfig returns safe defaults complying with D-09..D-13.
func DefaultHarnessConfig() *HarnessConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	runtimeDir := filepath.Join(home, ".dev-automation")
	if envHome := os.Getenv("RD_HARNESS_HOME"); envHome != "" {
		runtimeDir = envHome
	}
	return &HarnessConfig{
		RuntimeDir:         runtimeDir,
		MaxRetries:         2,
		StepTimeoutSeconds: 600,
		Roles: RoleConfig{
			Planner:      "gemini",
			Implementer:  "claude",
			GoldenA:      "claude",
			GoldenB:      "gemini",
			Remediation:  "gemini",
			SkillCurator: "gemini",
		},
		Telegram: TelegramConfig{
			PollTimeoutSeconds: 30,
		},
	}
}

// LoadHarnessConfig loads YAML config if present and applies defaults and env overrides.
func LoadHarnessConfig(path string) (*HarnessConfig, error) {
	cfg := DefaultHarnessConfig()

	if path == "" {
		path = filepath.Join(cfg.RuntimeDir, "config.yaml")
	}

	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse harness config %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read harness config %s: %w", path, err)
	}

	if envHome := os.Getenv("RD_HARNESS_HOME"); envHome != "" {
		cfg.RuntimeDir = envHome
	}
	if envToken := os.Getenv("TELEGRAM_BOT_TOKEN"); envToken != "" {
		cfg.Telegram.BotToken = envToken
	}
	if envCentral := os.Getenv("RD_CENTRAL_REPO"); envCentral != "" {
		cfg.CentralRepoDir = envCentral
	}

	applyConfigDefaults(cfg)
	return cfg, nil
}

func applyConfigDefaults(cfg *HarnessConfig) {
	def := DefaultHarnessConfig()
	if cfg.RuntimeDir == "" {
		cfg.RuntimeDir = def.RuntimeDir
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = def.MaxRetries
	}
	if cfg.StepTimeoutSeconds <= 0 {
		cfg.StepTimeoutSeconds = def.StepTimeoutSeconds
	}
	if cfg.Roles.Planner == "" {
		cfg.Roles.Planner = def.Roles.Planner
	}
	if cfg.Roles.Implementer == "" {
		cfg.Roles.Implementer = def.Roles.Implementer
	}
	if cfg.Roles.GoldenA == "" {
		cfg.Roles.GoldenA = def.Roles.GoldenA
	}
	if cfg.Roles.GoldenB == "" {
		cfg.Roles.GoldenB = def.Roles.GoldenB
	}
	if cfg.Roles.Remediation == "" {
		cfg.Roles.Remediation = def.Roles.Remediation
	}
	if cfg.Roles.SkillCurator == "" {
		cfg.Roles.SkillCurator = def.Roles.SkillCurator
	}
	if cfg.Telegram.PollTimeoutSeconds <= 0 {
		cfg.Telegram.PollTimeoutSeconds = def.Telegram.PollTimeoutSeconds
	}
}

// WriteConfig writes config YAML with restrictive permissions (0600) to protect tokens.
func WriteConfig(path string, cfg *HarnessConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal harness config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write harness config: %w", err)
	}
	return nil
}

func (c *HarnessConfig) DBPath() string {
	return filepath.Join(c.RuntimeDir, "state.db")
}

func (c *HarnessConfig) RunsDir() string {
	return filepath.Join(c.RuntimeDir, "runs")
}

func (c *HarnessConfig) RunDir(runID string) string {
	return filepath.Join(c.RunsDir(), runID)
}

func (c *HarnessConfig) StepTimeout() time.Duration {
	return time.Duration(c.StepTimeoutSeconds) * time.Second
}

func (c *HarnessConfig) SkillsDir() string {
	if c.CentralRepoDir == "" {
		return ""
	}
	return filepath.Join(c.CentralRepoDir, "skills")
}

// IsUserAllowed checks the strict Telegram whitelist. Empty list denies all users.
func (c *HarnessConfig) IsUserAllowed(userID int64) bool {
	for _, id := range c.Telegram.AllowedUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// LoadProjectConfig loads <projectRoot>/rd-project.yaml.
// Returns (cfg, exists, err). If the file does not exist, returns a minimal default with exists=false.
func LoadProjectConfig(projectRoot string) (*ProjectConfig, bool, error) {
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, false, err
	}
	cfgPath := filepath.Join(absRoot, "rd-project.yaml")
	baseName := filepath.Base(absRoot)

	data, err := os.ReadFile(cfgPath)
	if errors.Is(err, os.ErrNotExist) {
		return &ProjectConfig{
			ProjectID:   baseName,
			RepoLockKey: baseName,
			Commands: map[string]string{
				"status": "git status -sb",
			},
		}, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", cfgPath, err)
	}

	var pc ProjectConfig
	if err := yaml.Unmarshal(data, &pc); err != nil {
		return nil, true, fmt.Errorf("parse %s: %w", cfgPath, err)
	}
	if pc.ProjectID == "" {
		pc.ProjectID = baseName
	}
	if pc.RepoLockKey == "" {
		pc.RepoLockKey = pc.ProjectID
	}
	if pc.Commands == nil {
		pc.Commands = map[string]string{}
	}
	if _, ok := pc.Commands["status"]; !ok {
		pc.Commands["status"] = "git status -sb"
	}
	return &pc, true, nil
}
