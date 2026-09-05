package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lefthook runs `go test ./...` from the pre-push hook, where git exports
// GIT_DIR — and GIT_DIR beats cmd.Dir. Until TestMain cleared it, every
// `git init`/`git config` in the suite landed in the developer's own clone:
// TestOpenRejectsBare set core.bare on it and left the checkout unusable.
func TestTestRepoIsIsolatedFromAnInheritedGitDir(t *testing.T) {
	outer := t.TempDir()
	runGit(t, outer, "init", "-b", "main")

	t.Setenv("GIT_DIR", filepath.Join(outer, ".git"))
	t.Setenv("GIT_WORK_TREE", outer)
	UnsetRepoLocation()

	dir := newRepo(t)
	runGit(t, dir, "config", "user.email", "inner@example.com")

	got := strings.TrimSpace(runGit(t, dir, "rev-parse", "--absolute-git-dir"))
	if realPath(t, got) != realPath(t, filepath.Join(dir, ".git")) {
		t.Fatalf("the helper worked on %s, want the repo it created at %s", got, dir)
	}

	cfg, err := os.ReadFile(filepath.Join(outer, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "inner@example.com") {
		t.Errorf("the helper wrote into the repo GIT_DIR pointed at:\n%s", cfg)
	}
}

// Repo inherits the environment whole (ADR-0001), so the isolation has to come
// from the process, not from the command: this is the path that rewrote the
// real repository through production code rather than a test helper.
func TestOpenIsIsolatedFromAnInheritedGitDir(t *testing.T) {
	outer := t.TempDir()
	runGit(t, outer, "init", "-b", "main")
	work := newRepo(t)

	t.Setenv("GIT_DIR", filepath.Join(outer, ".git"))
	t.Setenv("GIT_WORK_TREE", outer)
	UnsetRepoLocation()

	repo := open(t, work)
	if realPath(t, repo.Root()) != realPath(t, work) {
		t.Errorf("Root() = %s, want %s", repo.Root(), work)
	}
}

func TestUnsetRepoLocationKeepsTransportSettings(t *testing.T) {
	t.Setenv("GIT_DIR", "/somewhere/.git")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.bare")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	t.Setenv("GIT_SSH_COMMAND", "ssh -i /key")

	UnsetRepoLocation()

	for _, name := range []string{"GIT_DIR", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("%s survived as %q", name, v)
		}
	}
	if os.Getenv("GIT_SSH_COMMAND") != "ssh -i /key" {
		t.Error("GIT_SSH_COMMAND was dropped, but git needs it to reach a remote")
	}
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
