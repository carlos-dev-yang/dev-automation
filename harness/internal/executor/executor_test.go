package executor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyReadOnlyUnchanged(t *testing.T) {
	if err := VerifyReadOnlyUnchanged(" M a.go\n", " M a.go\n"); err != nil {
		t.Fatalf("expected unchanged to pass: %v", err)
	}
	if err := VerifyReadOnlyUnchanged(" M a.go\n", " M a.go\n?? sneak.go\n"); !errors.Is(err, ErrReadOnlyViolation) {
		t.Fatalf("expected ErrReadOnlyViolation, got %v", err)
	}
}

func TestVerifyAllowedWritePaths(t *testing.T) {
	before := []string{"README.md"}
	afterOK := []string{"README.md", "skills/new-skill/SKILL.md"}
	if err := VerifyAllowedWritePaths(before, afterOK, []string{"skills/"}); err != nil {
		t.Fatalf("expected skills/ change to pass: %v", err)
	}

	afterBad := []string{"README.md", "skills/new-skill/SKILL.md", "harness/internal/core/engine.go"}
	if err := VerifyAllowedWritePaths(before, afterBad, []string{"skills/"}); !errors.Is(err, ErrWriteBoundaryViolation) {
		t.Fatalf("expected ErrWriteBoundaryViolation for harness/ edit, got %v", err)
	}
}

func TestExtractJSONObject(t *testing.T) {
	cases := []string{
		`{"result":"PASS","critical_count":0}`,
		"Here is the review:\n```json\n{\"result\":\"FAIL\",\"critical_count\":1}\n```\nDone.",
		`Warning: something logged first {"result":"PASS","notes":"brace } inside string"}`,
	}
	for i, tc := range cases {
		got, err := ExtractJSONObject(tc)
		if err != nil {
			t.Fatalf("case %d failed: %v", i, err)
		}
		if !strings.Contains(got, `"result"`) {
			t.Fatalf("case %d unexpected json: %s", i, got)
		}
	}
}

func TestBuildPromptWithSkills(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "gate-blueprint-check")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("# Gate 0 Rule\nMust verify open decisions."), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt, err := BuildPromptWithSkills(Request{
		Prompt:       "Review blueprint",
		SystemPrompt: "Act as architect",
		SkillFiles:   []string{skillPath},
		JSONSchema:   `{"type":"object"}`,
	})
	if err != nil {
		t.Fatalf("BuildPromptWithSkills failed: %v", err)
	}
	for _, want := range []string{"Gate 0 Rule", "Review blueprint", "Mandatory Structured Output"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q", want)
		}
	}
}
