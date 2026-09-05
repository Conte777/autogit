package git

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// lefthook runs `go test ./...` from the pre-push hook, where git exports
// GIT_DIR — and GIT_DIR beats cmd.Dir. Until the helpers filtered it, every
// `git init`/`git config` in the suite landed in the developer's own clone:
// TestOpenRejectsBare set core.bare on it and left the checkout unusable.
func TestTestRepoIsIsolatedFromAnInheritedGitDir(t *testing.T) {
	outer := t.TempDir()
	runGit(t, outer, "init", "-b", "main")

	t.Setenv("GIT_DIR", filepath.Join(outer, ".git"))
	t.Setenv("GIT_WORK_TREE", outer)

	dir := newRepo(t)
	runGit(t, dir, "config", "user.email", "inner@example.com")

	if got := strings.TrimSpace(runGit(t, dir, "rev-parse", "--absolute-git-dir")); resolve(t, got) != resolve(t, filepath.Join(dir, ".git")) {
		t.Fatalf("the helper worked on %s, want the repo it created at %s", got, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf("newRepo created no repository of its own: %v", err)
	}

	cfg, err := os.ReadFile(filepath.Join(outer, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "inner@example.com") {
		t.Errorf("the helper wrote into the repo GIT_DIR pointed at:\n%s", cfg)
	}
}

func TestEnvironDropsRedirectionButKeepsTransport(t *testing.T) {
	t.Setenv("GIT_DIR", "/somewhere/.git")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.bare")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	t.Setenv("GIT_SSH_COMMAND", "ssh -i /key")

	env := Environ("LC_ALL=C")

	for _, name := range []string{"GIT_DIR", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		if slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, name+"=") }) {
			t.Errorf("Environ kept %s", name)
		}
	}
	if !slices.Contains(env, "GIT_SSH_COMMAND=ssh -i /key") {
		t.Error("Environ dropped GIT_SSH_COMMAND, which git needs to reach a remote")
	}
	if !slices.Contains(env, "LC_ALL=C") {
		t.Error("Environ dropped the extra it was given")
	}
}

func resolve(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return real
}
