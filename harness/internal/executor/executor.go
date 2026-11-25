package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type PermissionMode string

const (
	ModeReadOnly       PermissionMode = "read_only"
	ModeWorkspaceWrite PermissionMode = "workspace_write"
)

type Request struct {
	WorkDir        string
	Prompt         string
	SystemPrompt   string
	Mode           PermissionMode
	SessionID      string
	JSONSchema     string
	AdditionalDirs []string
	SkillFiles     []string
	Timeout        time.Duration
	Model          string
	MaxBudgetUSD   float64
}

type Response struct {
	Adapter    string
	SessionID  string
	RawOutput  string
	TextOutput string
	Structured string
	ExitCode   int
	Duration   time.Duration
	TimedOut   bool
}

// AgentExecutor is the single unified interface across coding CLIs (D-09).
type AgentExecutor interface {
	Name() string
	Run(ctx context.Context, req Request) (*Response, error)
}

// Registry resolves adapter names ("claude", "gemini", "mock", etc.) to executors.
type Registry struct {
	executors map[string]AgentExecutor
}

func NewRegistry(execs ...AgentExecutor) *Registry {
	m := make(map[string]AgentExecutor, len(execs))
	for _, e := range execs {
		if e != nil {
			m[e.Name()] = e
		}
	}
	return &Registry{executors: m}
}

func (r *Registry) Get(name string) (AgentExecutor, error) {
	e, ok := r.executors[name]
	if !ok {
		return nil, fmt.Errorf("agent executor %q not registered", name)
	}
	return e, nil
}

// BuildPromptWithSkills injects required SKILL.md contents and optional JSON contract directly into the prompt
// so the agent cannot silently skip loading assigned skills.
func BuildPromptWithSkills(req Request) (string, error) {
	var b strings.Builder

	if strings.TrimSpace(req.SystemPrompt) != "" {
		b.WriteString("## Harness Role Directive\n")
		b.WriteString(strings.TrimSpace(req.SystemPrompt))
		b.WriteString("\n\n")
	}

	if len(req.SkillFiles) > 0 {
		b.WriteString("## Mandatory Skills (Injected by Harness — Follow Strictly)\n")
		for _, sf := range req.SkillFiles {
			data, err := os.ReadFile(sf)
			if err != nil {
				return "", fmt.Errorf("load mandatory skill %s: %w", sf, err)
			}
			b.WriteString(fmt.Sprintf("\n### Skill: %s\n", filepath.Base(filepath.Dir(sf))))
			b.WriteString(string(data))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Task Input\n")
	b.WriteString(req.Prompt)
	b.WriteString("\n")

	if strings.TrimSpace(req.JSONSchema) != "" {
		b.WriteString("\n## Mandatory Structured Output\n")
		b.WriteString("Respond with ONLY a single valid JSON object matching this JSON Schema (no markdown commentary outside JSON):\n")
		b.WriteString(req.JSONSchema)
		b.WriteString("\n")
	}

	return b.String(), nil
}
