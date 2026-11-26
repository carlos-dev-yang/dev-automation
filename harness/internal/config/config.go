package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// RoleModel binds a logical agent role to a specific provider adapter and model name.
type RoleModel struct {
	Adapter string `yaml:"adapter"`
	Model   string `yaml:"model"`
}

// ConversationRoles separates the frontline user-facing chat model from the central orchestrator model.
// Default: ChatResponder = gpt-6-luna (user dialogue & result presentation), Orchestrator = claude-haiku (context/skill routing, task ordering & remaining work calculation).
type ConversationRoles struct {
	ChatResponder RoleModel `yaml:"chat_responder"`
	Orchestrator  RoleModel `yaml:"orchestrator"`
}

// RoleConfig maps workflow worker roles to AgentExecutor adapter names.
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

// AdapterModels holds optional model, binary, auth project, and budget overrides per CLI adapter.
type AdapterModels struct {
	ClaudeModel        string  `yaml:"claude_model,omitempty"`
	GeminiModel        string  `yaml:"gemini_model,omitempty"`
	GeminiCloudProject string  `yaml:"gemini_cloud_project,omitempty"`
	CodexBinary        string  `yaml:"codex_binary,omitempty"`
	ClaudeMaxBudgetUSD float64 `yaml:"claude_max_budget_usd,omitempty"`
}

// ProtocolConfig holds settings for the external channel-agnostic HTTP/JSON + SSE ingress/egress protocol.
type ProtocolConfig struct {
	ListenAddr string `yaml:"listen_addr"`
	AuthToken  string `yaml:"auth_token,omitempty"`
}

// APIConfig holds direct HTTP API provider credentials loaded primarily from ~/.dev-automation/.env or process env.
type APIConfig struct {
	AnthropicAPIKey  string `yaml:"anthropic_api_key,omitempty"`
	AnthropicBaseURL string `yaml:"anthropic_base_url,omitempty"`
}

// HarnessConfig is the runtime configuration stored outside the repo (default ~/.dev-automation/config.yaml).
type HarnessConfig struct {
	RuntimeDir         string            `yaml:"runtime_dir"`
	CentralRepoDir     string            `yaml:"central_repo_dir"`
	MaxRetries         int               `yaml:"max_retries"`
	StepTimeoutSeconds int               `yaml:"step_timeout_seconds"`
	Conversation       ConversationRoles `yaml:"conversation"`
	Roles              RoleConfig        `yaml:"roles"`
	Models             AdapterModels     `yaml:"models"`
	APIs               APIConfig         `yaml:"apis,omitempty"`
	Protocol           ProtocolConfig    `yaml:"protocol"`
	Telegram           TelegramConfig    `yaml:"telegram"`
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
		Conversation: ConversationRoles{
			ChatResponder: RoleModel{
				Adapter: "codex",
				Model:   "gpt-6-luna",
			},
			Orchestrator: RoleModel{
				Adapter: "anthropic-api",
				Model:   "claude-haiku-5-5",
			},
		},
		Roles: RoleConfig{
			Planner:      "gemini",
			Implementer:  "claude",
			GoldenA:      "claude",
			GoldenB:      "gemini",
			Remediation:  "gemini",
			SkillCurator: "gemini",
		},
		Protocol: ProtocolConfig{
			ListenAddr: "127.0.0.1:8787",
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

	// Load core secrets from ~/.dev-automation/.env (or RD_ENV_FILE); process env vars override.
	// Channel adapter secrets (e.g. Telegram) are intentionally NOT loaded from core .env.
	envFile := os.Getenv("RD_ENV_FILE")
	if envFile == "" {
		envFile = cfg.EnvPath()
	}
	dotEnv, err := LoadDotEnv(envFile)
	if err != nil {
		return nil, err
	}
	getCoreEnv := func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return dotEnv[key]
	}

	if v := getCoreEnv("ANTHROPIC_API_KEY"); v != "" {
		cfg.APIs.AnthropicAPIKey = v
	}
	if v := getCoreEnv("ANTHROPIC_BASE_URL"); v != "" {
		cfg.APIs.AnthropicBaseURL = v
	}
	if v := getCoreEnv("RD_CENTRAL_REPO"); v != "" {
		cfg.CentralRepoDir = v
	}
	if v := getCoreEnv("RD_PROTOCOL_ADDR"); v != "" {
		cfg.Protocol.ListenAddr = v
	}
	if v := getCoreEnv("RD_PROTOCOL_TOKEN"); v != "" {
		cfg.Protocol.AuthToken = v
	}
	if v := getCoreEnv("CODEX_BINARY"); v != "" {
		cfg.Models.CodexBinary = v
	}
	if v := getCoreEnv("GOOGLE_CLOUD_PROJECT"); v != "" && cfg.Models.GeminiCloudProject == "" {
		cfg.Models.GeminiCloudProject = v
	} else if v := getCoreEnv("GOOGLE_CLOUD_PROJECT_ID"); v != "" && cfg.Models.GeminiCloudProject == "" {
		cfg.Models.GeminiCloudProject = v
	}

	applyConfigDefaults(cfg)
	return cfg, nil
}

// LoadTelegramAdapterEnv loads Telegram adapter-only credentials from ~/.dev-automation/adapters/telegram.env
// (or RD_TELEGRAM_ENV_FILE) and process env vars, keeping channel secrets isolated from harness core .env.
func LoadTelegramAdapterEnv(cfg *HarnessConfig) error {
	envFile := os.Getenv("RD_TELEGRAM_ENV_FILE")
	if envFile == "" {
		envFile = cfg.TelegramEnvPath()
	}
	dotEnv, err := LoadDotEnv(envFile)
	if err != nil {
		return err
	}
	getEnv := func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return dotEnv[key]
	}
	if v := getEnv("TELEGRAM_BOT_TOKEN"); v != "" {
		cfg.Telegram.BotToken = v
	}
	if v := getEnv("TELEGRAM_ALLOWED_USER_IDS"); v != "" {
		if parsed := parseInt64CSV(v); len(parsed) > 0 {
			cfg.Telegram.AllowedUserIDs = parsed
		}
	}
	if v := getEnv("TELEGRAM_POLL_TIMEOUT_SECONDS"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			cfg.Telegram.PollTimeoutSeconds = sec
		}
	}
	return nil
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
	if cfg.Conversation.ChatResponder.Adapter == "" {
		cfg.Conversation.ChatResponder.Adapter = def.Conversation.ChatResponder.Adapter
	}
	if cfg.Conversation.ChatResponder.Model == "" {
		cfg.Conversation.ChatResponder.Model = def.Conversation.ChatResponder.Model
	}
	if cfg.Conversation.Orchestrator.Adapter == "" {
		cfg.Conversation.Orchestrator.Adapter = def.Conversation.Orchestrator.Adapter
	}
	if cfg.Conversation.Orchestrator.Model == "" {
		cfg.Conversation.Orchestrator.Model = def.Conversation.Orchestrator.Model
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
	if cfg.Protocol.ListenAddr == "" {
		cfg.Protocol.ListenAddr = def.Protocol.ListenAddr
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

func (c *HarnessConfig) EnvPath() string {
	return filepath.Join(c.RuntimeDir, ".env")
}

// TelegramEnvPath returns the isolated secrets path for the Telegram channel adapter.
func (c *HarnessConfig) TelegramEnvPath() string {
	return filepath.Join(c.RuntimeDir, "adapters", "telegram.env")
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

// CodexAuthDir returns the harness-internal directory (outside the repo, under ~/.dev-automation/)
// where ChatGPT subscription auth for Codex CLI is stored.
func (c *HarnessConfig) CodexAuthDir() string {
	return filepath.Join(c.RuntimeDir, "auth", "codex")
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
