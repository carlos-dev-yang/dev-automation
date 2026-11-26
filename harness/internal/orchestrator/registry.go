package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/store"
)

// ContextRegistry provides node/project and skill discovery for the central Orchestrator.
type ContextRegistry interface {
	ListNodes(ctx context.Context) ([]NodeInfo, error)
	ResolveBoundNode(ctx context.Context, channel, chatID string) (*NodeInfo, error)
	ListSkills(ctx context.Context) ([]SkillSummary, error)
}

// LocalContextRegistry discovers registered projects from SQLite and scans SKILL.md frontmatter directly (D-02 SSoT).
type LocalContextRegistry struct {
	store     *store.Store
	skillsDir string
}

func NewLocalContextRegistry(st *store.Store, skillsDir string) *LocalContextRegistry {
	return &LocalContextRegistry{
		store:     st,
		skillsDir: skillsDir,
	}
}

func (r *LocalContextRegistry) ListNodes(ctx context.Context) ([]NodeInfo, error) {
	if r.store == nil {
		return nil, nil
	}
	projects, err := r.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]NodeInfo, 0, len(projects))
	for _, p := range projects {
		nodes = append(nodes, NodeInfo{
			ID:          p.ID,
			Name:        p.Name,
			RootPath:    p.RootPath,
			RepoLockKey: p.RepoLockKey,
		})
	}
	return nodes, nil
}

func (r *LocalContextRegistry) ResolveBoundNode(ctx context.Context, channel, chatID string) (*NodeInfo, error) {
	if r.store == nil {
		return nil, nil
	}
	p, err := r.store.GetBoundProject(ctx, channel, chatID)
	if err != nil {
		return nil, nil
	}
	return &NodeInfo{
		ID:          p.ID,
		Name:        p.Name,
		RootPath:    p.RootPath,
		RepoLockKey: p.RepoLockKey,
	}, nil
}

func (r *LocalContextRegistry) ListSkills(_ context.Context) ([]SkillSummary, error) {
	if strings.TrimSpace(r.skillsDir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(r.skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var skills []SkillSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillFile := filepath.Join(r.skillsDir, e.Name(), "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}
		sum := parseSkillFrontmatter(e.Name(), skillFile, string(data))
		skills = append(skills, sum)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		Status string   `yaml:"status"`
		Stages []string `yaml:"stages"`
		Scopes []string `yaml:"scopes"`
	} `yaml:"metadata"`
}

func parseSkillFrontmatter(dirName, filePath, content string) SkillSummary {
	sum := SkillSummary{
		Name: dirName,
		Path: filePath,
	}
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return sum
	}
	rest := strings.TrimPrefix(trimmed, "---")
	endIdx := strings.Index(rest, "\n---")
	if endIdx < 0 {
		return sum
	}
	rawYAML := rest[:endIdx]
	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(rawYAML), &fm); err != nil {
		return sum
	}
	if fm.Name != "" {
		sum.Name = fm.Name
	}
	sum.Description = strings.TrimSpace(fm.Description)
	sum.Status = fm.Metadata.Status
	sum.Stages = fm.Metadata.Stages
	sum.Scopes = fm.Metadata.Scopes
	return sum
}
