//go:build unix

package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A signing helper that forks is the pinentry shape: killing git alone leaves
// the helper's own child waiting for a passphrase nobody will type. The commit
// is cancelled rather than timed out so that the helper is provably running
// when the kill arrives; both reach git through the same cmd.Cancel.
func TestCancelledCommitKillsTheSignersChild(t *testing.T) {
	dir := newRepo(t)
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "helper.pid")
	signer := writeSigner(t, tmp, "sh -c 'echo $$ > "+pidFile+"; exec sleep 60' &\nsleep 60\n")
	signWith(t, dir, signer)

	repo, err := Open(context.Background(), dir, Options{CommitTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, commitErr := repo.Commit(ctx, "feat: add thing")
		done <- commitErr
	}()

	pid := waitForPID(t, pidFile)
	cancel()

	select {
	case commitErr := <-done:
		if !errors.Is(commitErr, context.Canceled) {
			t.Errorf("Commit err = %v, want a cancellation", commitErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Commit ignored the cancellation")
	}

	deadline := time.Now().Add(10 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the signing helper's child (pid %d) outlived the cancelled commit", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if repo.HasCommits(context.Background()) {
		t.Error("a commit landed although signing never finished")
	}
}

// Isolating git costs it the foreground process group, so a helper that reads
// the terminal for a passphrase would be stopped by SIGTTIN. Only the run that
// has nobody to ask may pay that price.
func TestOnlyNonInteractiveGitLeavesAutogitsProcessGroup(t *testing.T) {
	for _, tc := range []struct {
		name        string
		interactive bool
		wantOwn     bool
	}{
		{name: "no-input", interactive: false, wantOwn: true},
		{name: "terminal", interactive: true, wantOwn: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			tmp := t.TempDir()
			pgidFile := filepath.Join(tmp, "helper.pgid")
			signer := writeSigner(t, tmp, "ps -o pgid= -p $$ > "+pgidFile+"\n")
			signWith(t, dir, signer)

			repo, err := Open(context.Background(), dir, Options{Interactive: tc.interactive})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Commit(context.Background(), "feat: add thing"); err == nil {
				t.Fatal("Commit succeeded although the signer produced no signature")
			}

			pgid := waitForPID(t, pgidFile)
			own := pgid != syscall.Getpgrp()
			if own != tc.wantOwn {
				t.Errorf("git pgid = %d, autogit pgid = %d; own group = %v, want %v",
					pgid, syscall.Getpgrp(), own, tc.wantOwn)
			}
		})
	}
}

func writeSigner(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "signer")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func signWith(t *testing.T, dir, signer string) {
	t.Helper()
	runGit(t, dir, "config", "gpg.program", signer)
	runGit(t, dir, "config", "commit.gpgsign", "true")
	runGit(t, dir, "config", "user.signingkey", "DEADBEEF")
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")
}

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the signing helper never recorded a pid in %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
