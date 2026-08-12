package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCommitTimesOutOnAHangingSigner is the pinentry case: a signing setup that
// waits for input must fail with a deadline, not wedge the caller forever.
func TestCommitTimesOutOnAHangingSigner(t *testing.T) {
	dir := newRepo(t)
	signer := filepath.Join(t.TempDir(), "hanging-gpg")
	if err := os.WriteFile(signer, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "config", "gpg.program", signer)
	runGit(t, dir, "config", "commit.gpgsign", "true")
	runGit(t, dir, "config", "user.signingkey", "DEADBEEF")

	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")

	repo, err := Open(context.Background(), dir, Options{CommitTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = repo.Commit(context.Background(), "feat: add thing")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Commit succeeded with a signer that never answers")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want a deadline error the CLI can map to exit 6", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("Commit took %s; the timeout did not fire", elapsed)
	}
	if repo.HasCommits(context.Background()) {
		t.Error("a commit landed although signing never finished")
	}
}
