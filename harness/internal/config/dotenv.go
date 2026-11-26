package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultCoreEnvTemplate = `# rd-harness CORE local secrets (stored outside git repo with 0600 permissions)
# Loaded automatically by rd-harness core on startup. Process env vars override values here.
# Channel adapter credentials (Telegram, Slack, etc.) are managed separately under ~/.dev-automation/adapters/.

# Central Orchestrator (Anthropic HTTP API — default model: claude-haiku-5-5)
ANTHROPIC_API_KEY=

# Optional: custom Anthropic API base URL (leave blank for https://api.anthropic.com)
# ANTHROPIC_BASE_URL=

# Optional: Bearer token for external HTTP/SSE protocol server
# RD_PROTOCOL_TOKEN=
`

const DefaultTelegramEnvTemplate = `# Telegram external channel adapter secrets (stored outside git repo with 0600 permissions)
# Loaded ONLY by the Telegram adapter; harness core remains channel-agnostic.

TELEGRAM_BOT_TOKEN=
TELEGRAM_ALLOWED_USER_IDS=
# TELEGRAM_POLL_TIMEOUT_SECONDS=30
`

// LoadDotEnv parses a simple KEY=VALUE .env file without mutating process env.
// Returns (empty map, nil) if the file does not exist.
func LoadDotEnv(path string) (map[string]string, error) {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("open env file %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key != "" {
			out[key] = val
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read env file %s: %w", path, err)
	}
	return out, nil
}

// EnsureEnvTemplate writes the core .env template with 0600 permissions if it does not already exist.
func EnsureEnvTemplate(path string) (bool, error) {
	return ensureFileTemplate(path, DefaultCoreEnvTemplate)
}

// EnsureTelegramEnvTemplate writes the Telegram adapter .env template with 0600 permissions if missing.
func EnsureTelegramEnvTemplate(path string) (bool, error) {
	return ensureFileTemplate(path, DefaultTelegramEnvTemplate)
}

func ensureFileTemplate(path, content string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("create env dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return false, fmt.Errorf("write env template %s: %w", path, err)
	}
	return true, nil
}

func parseInt64CSV(raw string) []int64 {
	var ids []int64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if v, err := strconv.ParseInt(part, 10, 64); err == nil {
			ids = append(ids, v)
		}
	}
	return ids
}
