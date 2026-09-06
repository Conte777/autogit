//go:build unix

package git

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A signing helper that forks is the pinentry shape: killing git alone leaves
// the helper's own child waiting for a passphrase nobody will type.
func TestCommitTimeoutKillsTheSignersChild(t *testing.T) {
	dir := newRepo(t)
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "helper.pid")
	signer := filepath.Join(tmp, "forking-gpg")
	body := "#!/bin/sh\nsh -c 'echo $$ > " + pidFile + "; exec sleep 60' &\nsleep 60\n"
	if err := os.WriteFile(signer, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "config", "gpg.program", signer)
	runGit(t, dir, "config", "commit.gpgsign", "true")
	runGit(t, dir, "config", "user.signingkey", "DEADBEEF")

	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")

	repo, err := Open(context.Background(), dir, Options{CommitTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit(context.Background(), "feat: add thing"); err == nil {
		t.Fatal("Commit succeeded with a signer that never answers")
	}

	pid := waitForPID(t, pidFile)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the signing helper's child (pid %d) outlived the timed-out commit", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the signing helper never recorded its child's pid in %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
