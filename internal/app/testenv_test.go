package app_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/provider/mock"
	"github.com/Conte777/autogit/internal/ui"
)

type env struct {
	t    *testing.T
	dir  string
	repo *git.Repo
	prov *mock.Provider
	cfg  *config.Config
}

func newEnv(t *testing.T, replies ...string) *env {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "config", "commit.gpgsign", "false")

	repo, err := git.Open(context.Background(), dir, git.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	// Most tests commit on main; the two that care about protection opt back in.
	cfg.ProtectedBranches = nil
	return &env{t: t, dir: dir, repo: repo, prov: &mock.Provider{Replies: replies}, cfg: &cfg}
}

// app builds an App on the CLI surface with no terminal, which is how every
// non-interactive test wants it.
func (e *env) app() *app.App {
	e.t.Helper()
	p, err := e.cfg.ResolvePreset()
	if err != nil {
		e.t.Fatal(err)
	}
	return &app.App{
		Repo:       e.repo,
		Config:     e.cfg,
		Preset:     p,
		PresetName: e.cfg.Preset,
		Provider:   e.prov,
		Prompt:     ui.Noop{},
		Surface:    app.SurfaceCLI,
	}
}

// answering builds an App whose questions are answered by a scripted prompter.
func (e *env) answering(p ui.Prompter) *app.App {
	a := e.app()
	a.Prompt = p
	return a
}

func (e *env) write(name, content string) {
	e.t.Helper()
	full := filepath.Join(e.dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) git(args ...string) string {
	e.t.Helper()
	return gitRun(e.t, e.dir, args...)
}

func (e *env) commitFile(name, content, message string) {
	e.t.Helper()
	e.write(name, content)
	e.git("add", ".")
	e.git("commit", "-m", message)
}

func (e *env) head() string {
	e.t.Helper()
	out, err := exec.Command("git", "-C", e.dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (e *env) stagedDiff() string {
	e.t.Helper()
	return e.git("diff", "--cached")
}

// tryGit runs a command expected to fail, e.g. a conflicting merge.
func tryGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = git.Environ("LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	return cmd.Run()
}

func chmodExec(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = git.Environ("LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// scripted answers a fixed reply to every question.
type scripted struct {
	confirm bool
	choice  string
}

func (s scripted) Confirm(string, bool) (bool, error)         { return s.confirm, nil }
func (s scripted) Choose(string, []ui.Option) (string, error) { return s.choice, nil }
func (scripted) Interactive() bool                            { return true }
