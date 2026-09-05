package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRejectsNonRepo(t *testing.T) {
	if _, err := Open(context.Background(), t.TempDir(), Options{}); err == nil {
		t.Fatal("Open on a plain directory succeeded, want ErrNotARepo")
	}
}

func TestOpenRejectsBare(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--bare", "-b", "main")

	_, err := Open(context.Background(), dir, Options{})
	var se *StateError
	if !errors.As(err, &se) {
		t.Fatalf("Open on a bare repo err = %v, want *StateError", err)
	}
}

func TestCurrentBranchAndDetached(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	r := open(t, dir)

	got, err := r.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "main" || got.Detached {
		t.Fatalf("Current() = %+v, want {main false}", got)
	}

	runGit(t, dir, "checkout", "--detach", "HEAD")
	got, err = r.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Detached || got.Name == "" {
		t.Fatalf("Current() on a detached HEAD = %+v, want Detached with a short hash", got)
	}
}

func TestUnbornBranchIsNotDetached(t *testing.T) {
	dir := newRepo(t)
	r := open(t, dir)

	got, err := r.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "main" || got.Detached {
		t.Fatalf("Current() in a fresh repo = %+v, want {main false}", got)
	}
	if r.HasCommits(context.Background()) {
		t.Error("HasCommits() = true in a fresh repo")
	}
}

func TestHasStaged(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	r := open(t, dir)

	staged, err := r.HasStaged(ctx)
	if err != nil || staged {
		t.Fatalf("HasStaged() = %v, %v; want false, nil", staged, err)
	}

	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")
	staged, err = r.HasStaged(ctx)
	if err != nil || !staged {
		t.Fatalf("HasStaged() after add = %v, %v; want true, nil", staged, err)
	}
}

func TestStatusSplitsTrackedAndUntracked(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	r := open(t, dir)

	write(t, dir, "a.txt", "two\n")
	write(t, dir, "new.txt", "new\n")

	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModifiedTracked || !st.Untracked {
		t.Fatalf("Status() = %+v, want both flags set", st)
	}
}

func TestCommitKeepsBodyAndHashLines(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")
	r := open(t, dir)

	msg := "feat: add thing\n\nWhy it exists.\n# not a comment, part of the body\n\nRefs: CUS-1"
	res, err := r.Commit(ctx, msg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Message != msg {
		t.Errorf("Commit() message =\n%q\nwant\n%q", res.Message, msg)
	}
	if res.Hash == "" || res.ShortHash == "" {
		t.Errorf("Commit() = %+v, want both hashes", res)
	}
}

func TestCommitReportsHookRewrite(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	hook := filepath.Join(dir, ".git", "hooks", "commit-msg")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'rewritten by hook' > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")

	res, err := open(t, dir).Commit(ctx, "feat: original message")
	if err != nil {
		t.Fatal(err)
	}
	if res.Message != "rewritten by hook" {
		t.Errorf("Commit() message = %q, want what the hook wrote", res.Message)
	}
	if got := strings.TrimSpace(runGit(t, dir, "log", "-1", "--format=%B")); got != res.Message {
		t.Errorf("reported %q, git log says %q", res.Message, got)
	}
}

func TestSubjectsAndBranchLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	for _, s := range []string{"feat(api): one", "fix(cli): two", "chore: three"} {
		write(t, dir, "a.txt", s)
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "-m", s)
	}
	r := open(t, dir)

	subjects, err := r.Subjects(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(subjects) != 3 {
		t.Fatalf("Subjects() = %v, want 3 entries", subjects)
	}

	if r.BranchExists(ctx, "feat/thing") {
		t.Fatal("BranchExists() = true before the branch was created")
	}
	if err := r.CreateBranch(ctx, "feat/thing"); err != nil {
		t.Fatal(err)
	}
	if !r.BranchExists(ctx, "feat/thing") {
		t.Fatal("BranchExists() = false right after CreateBranch")
	}
	if err := r.CreateBranch(ctx, "feat/thing"); err == nil {
		t.Fatal("CreateBranch on an existing name succeeded, want a collision error")
	}
}

func TestSubjectsOnEmptyRepo(t *testing.T) {
	subjects, err := open(t, newRepo(t)).Subjects(context.Background(), 500)
	if err != nil || subjects != nil {
		t.Fatalf("Subjects() on a fresh repo = %v, %v; want nil, nil", subjects, err)
	}
}
