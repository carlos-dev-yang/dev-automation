package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/carlos-dev-yang/dev-automation/harness/internal/config"
)

type CommandResult struct {
	Key        string `json:"key"`
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code"`
	Status     string `json:"status"` // PASS | FAIL | OPEN_UNVERIFIED
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
}

type CheckSuiteReport struct {
	ProjectID  string          `json:"project_id"`
	WorkDir    string          `json:"work_dir"`
	AllPassed  bool            `json:"all_passed"`
	Results    []CommandResult `json:"results"`
	FinishedAt time.Time       `json:"finished_at"`
}

type LinkStatus struct {
	LinkPath string
	Target   string
	Status   string // OK | LINKED | DRY_RUN | CONFLICT
	Message  string
}

var ErrLayoutViolation = errors.New("sibling directory layout violation")
var ErrLinkConflict = errors.New("existing path conflict")

// RunShell executes cmdStr in workDir inside its own process group so timeouts/cancels kill the entire tree.
func RunShell(ctx context.Context, workDir, key, cmdStr string, timeout time.Duration) CommandResult {
	start := time.Now()
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = workDir
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	res := CommandResult{
		Key:     key,
		Command: cmdStr,
	}

	if err := cmd.Start(); err != nil {
		res.ExitCode = -1
		res.Status = "FAIL"
		res.Stderr = err.Error()
		res.DurationMs = time.Since(start).Milliseconds()
		return res
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-done:
	case <-runCtx.Done():
		res.TimedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded)
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		waitErr = <-done
	}

	res.Stdout = truncateTail(stdout.String(), 16000)
	res.Stderr = truncateTail(stderr.String(), 8000)
	res.DurationMs = time.Since(start).Milliseconds()

	if waitErr == nil && runCtx.Err() == nil {
		res.ExitCode = 0
		res.Status = "PASS"
		return res
	}
	res.Status = "FAIL"
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		res.ExitCode = exitErr.ExitCode()
	} else {
		res.ExitCode = -1
		if res.Stderr == "" && runCtx.Err() != nil {
			res.Stderr = runCtx.Err().Error()
		}
	}
	return res
}

// RunChecks executes deterministic No-AI commands defined in rd-project.yaml for Golden Set A evidence.
func RunChecks(ctx context.Context, workDir string, pc *config.ProjectConfig, keys []string, timeout time.Duration) CheckSuiteReport {
	if len(keys) == 0 {
		keys = []string{"static_check", "test", "build_check", "e2e"}
	}
	report := CheckSuiteReport{
		ProjectID: pc.ProjectID,
		WorkDir:   workDir,
		AllPassed: true,
	}

	for _, key := range keys {
		cmdStr, ok := pc.Commands[key]
		if !ok || strings.TrimSpace(cmdStr) == "" {
			report.Results = append(report.Results, CommandResult{
				Key:    key,
				Status: "OPEN_UNVERIFIED",
				Stdout: "command not configured in rd-project.yaml",
			})
			continue
		}
		res := RunShell(ctx, workDir, key, cmdStr, timeout)
		if res.Status != "PASS" {
			report.AllPassed = false
		}
		report.Results = append(report.Results, res)
	}
	report.FinishedAt = time.Now().UTC()
	return report
}

// WriteAtomicJSON persists JSON artifacts atomically via temp file + rename.
func WriteAtomicJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomicBytes(path, data)
}

func WriteAtomicBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EnsureSkillLinks enforces D-12 sibling layout and links .agents/skills and .claude/skills -> ../../dev-automation/skills.
func EnsureSkillLinks(centralRepoDir, projectDir string, dryRun bool) ([]LinkStatus, error) {
	absCentral, err := filepath.Abs(centralRepoDir)
	if err != nil {
		return nil, err
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}

	if filepath.Dir(absProject) != filepath.Dir(absCentral) || filepath.Base(absCentral) != "dev-automation" {
		return nil, fmt.Errorf("%w: central repo (%s) and project (%s) must be sibling directories with central folder named 'dev-automation'", ErrLayoutViolation, absCentral, absProject)
	}

	centralSkills := filepath.Join(absCentral, "skills")
	if st, err := os.Stat(centralSkills); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("central skills directory not found: %s", centralSkills)
	}

	targets := []string{
		filepath.Join(absProject, ".agents", "skills"),
		filepath.Join(absProject, ".claude", "skills"),
	}

	type plannedLink struct {
		linkPath  string
		parentDir string
		relTarget string
	}
	var statuses []LinkStatus
	var toCreate []plannedLink
	var hasConflict bool

	for _, linkPath := range targets {
		parentDir := filepath.Dir(linkPath)
		relTarget, err := filepath.Rel(parentDir, centralSkills)
		if err != nil {
			return statuses, err
		}

		info, lstatErr := os.Lstat(linkPath)
		if lstatErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				cur, readErr := os.Readlink(linkPath)
				if readErr == nil && filepath.Clean(filepath.Join(parentDir, cur)) == filepath.Clean(centralSkills) {
					statuses = append(statuses, LinkStatus{
						LinkPath: linkPath,
						Target:   cur,
						Status:   "OK",
						Message:  "already linked",
					})
					continue
				}
				hasConflict = true
				statuses = append(statuses, LinkStatus{
					LinkPath: linkPath,
					Target:   cur,
					Status:   "CONFLICT",
					Message:  fmt.Sprintf("existing symlink points to %q, expected %q", cur, relTarget),
				})
				continue
			}
			hasConflict = true
			statuses = append(statuses, LinkStatus{
				LinkPath: linkPath,
				Target:   relTarget,
				Status:   "CONFLICT",
				Message:  "real directory/file exists; migrate skills to dev-automation/skills and remove folder first",
			})
			continue
		}

		if dryRun {
			statuses = append(statuses, LinkStatus{
				LinkPath: linkPath,
				Target:   relTarget,
				Status:   "DRY_RUN",
				Message:  "would create symlink",
			})
			continue
		}
		toCreate = append(toCreate, plannedLink{linkPath: linkPath, parentDir: parentDir, relTarget: relTarget})
	}

	if hasConflict {
		return statuses, ErrLinkConflict
	}

	for _, pl := range toCreate {
		if err := os.MkdirAll(pl.parentDir, 0o755); err != nil {
			return statuses, err
		}
		if err := os.Symlink(pl.relTarget, pl.linkPath); err != nil {
			return statuses, err
		}
		statuses = append(statuses, LinkStatus{
			LinkPath: pl.linkPath,
			Target:   pl.relTarget,
			Status:   "LINKED",
			Message:  "symlink created (remember to git add for 120000 tracking)",
		})
	}

	return statuses, nil
}

// GitStatusPorcelain returns exact working-tree status including untracked files.
func GitStatusPorcelain(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git status in %s: %w (%s)", dir, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// GitChangedPaths parses relative file paths from git status --porcelain output.
func GitChangedPaths(porcelain string) []string {
	var paths []string
	for _, line := range strings.Split(porcelain, "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		if idx := strings.Index(p, " -> "); idx >= 0 {
			p = strings.TrimSpace(p[idx+4:])
		}
		p = strings.Trim(p, `"`)
		if p != "" {
			paths = append(paths, filepath.ToSlash(p))
		}
	}
	return paths
}

// GitDiffSummary returns a bounded git diff for evaluation prompts.
func GitDiffSummary(ctx context.Context, dir string, maxBytes int) string {
	cmd := exec.CommandContext(ctx, "git", "diff", "--stat", "HEAD")
	cmd.Dir = dir
	statOut, _ := cmd.CombinedOutput()

	cmd2 := exec.CommandContext(ctx, "git", "diff", "HEAD")
	cmd2.Dir = dir
	diffOut, _ := cmd2.CombinedOutput()

	combined := "=== git diff --stat ===\n" + string(statOut) + "\n=== git diff ===\n" + string(diffOut)
	if maxBytes > 0 && len(combined) > maxBytes {
		return combined[:maxBytes] + "\n... [diff truncated]"
	}
	return combined
}

func truncateTail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "...[truncated]\n" + s[len(s)-max:]
}
