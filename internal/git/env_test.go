package git

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A git hook exports GIT_DIR, and GIT_DIR beats cmd.Dir. Inheriting it makes
// every command land in the hook's repository: autogit run from a hook would
// commit somewhere else, and the test suite rewrote the developer's own
// .git/config until this was fixed.
func TestGitDirFromAHookDoesNotRedirectTheRepo(t *testing.T) {
	work := newRepo(t)
	write(t, work, "a.txt", "one\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "seed")

	elsewhere := newRepo(t)
	t.Setenv("GIT_DIR", filepath.Join(elsewhere, ".git"))
	t.Setenv("GIT_WORK_TREE", elsewhere)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(elsewhere, ".git", "index"))

	repo := open(t, work)
	branch, err := repo.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if branch.Name != "main" {
		t.Errorf("Current() = %q, want the branch of the repo we opened", branch.Name)
	}

	dir, err := repo.gitDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := resolve(t, dir); got != resolve(t, filepath.Join(work, ".git")) {
		t.Errorf("gitDir() = %s, want %s: GIT_DIR from the environment won", got, work)
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
