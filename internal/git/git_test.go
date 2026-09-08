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

func TestCommitOnAnEmptyIndexIsNothingToCommit(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")

	_, err := open(t, dir).Commit(ctx, "feat: nothing to record")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("Commit() on an empty index = %v, want ErrNothingToCommit", err)
	}
}

// `git merge -s ours` records a merge whose tree equals HEAD's, so the empty
// index is the answer git wants, not a state to refuse.
func TestCommitAllowsAMergeWithAnEmptyIndex(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	mergeWithNoDiff(t, dir)

	if _, err := open(t, dir).Commit(ctx, "Merge branch 'side'"); err != nil {
		t.Fatal(err)
	}
	if parents := strings.Fields(runGit(t, dir, "log", "-1", "--format=%P")); len(parents) != 2 {
		t.Errorf("the commit has %d parent(s), want 2", len(parents))
	}
}

// A merge that fails over something else keeps that reason: its empty index is
// normal, so the emptied-index diagnosis would name the wrong cause.
func TestCommitKeepsTheRealFailureDuringAMerge(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	mergeWithNoDiff(t, dir)
	writeHook(t, dir, "pre-commit", "#!/bin/sh\necho 'the hook says no' >&2\nexit 1\n")

	_, err := open(t, dir).Commit(ctx, "Merge branch 'side'")
	if errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("Commit() = %v, want the hook's own refusal", err)
	}
	if err == nil || !strings.Contains(err.Error(), "the hook says no") {
		t.Errorf("Commit() = %v, want the hook's message", err)
	}
}

// The index is emptied between the diff autogit read and the commit it writes:
// the two-runs-at-once race the hook reproduces here.
func TestCommitReportsAnIndexEmptiedUnderIt(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	writeHook(t, dir, "pre-commit", "#!/bin/sh\ngit commit -q --no-verify -m 'the other run'\n")
	write(t, dir, "b.txt", "two\n")
	runGit(t, dir, "add", ".")

	_, err := open(t, dir).Commit(ctx, "feat: lost the race")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("Commit() = %v, want ErrNothingToCommit", err)
	}
}

// git explains an index with nothing in it on stdout and leaves stderr empty.
func TestExecErrorCarriesStdoutWhenStderrIsSilent(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")

	_, err := open(t, dir).run(context.Background(), defaultTimeout, "feat: nothing\n",
		"commit", "--cleanup=whitespace", "-F", "-")
	var execErr *ExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("run() = %v, want an ExecError", err)
	}
	if execErr.Stderr != "" {
		t.Errorf("Stderr = %q, want git to have said it on stdout", execErr.Stderr)
	}
	if !strings.Contains(execErr.Stdout, "nothing to commit") {
		t.Errorf("Stdout = %q, want git's explanation", execErr.Stdout)
	}
	if !strings.Contains(execErr.Error(), "nothing to commit") {
		t.Errorf("Error() = %q, want git's explanation", execErr)
	}
}

func mergeWithNoDiff(t *testing.T, dir string) {
	t.Helper()
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	runGit(t, dir, "switch", "-c", "side")
	write(t, dir, "b.txt", "side\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "side")
	runGit(t, dir, "switch", "main")
	runGit(t, dir, "merge", "--no-commit", "-s", "ours", "side")
}

func writeHook(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestExecErrorPrefersStderrOverStdout(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *ExecError
		want string
	}{
		{
			name: "stderr",
			err:  &ExecError{Args: []string{"commit"}, Stderr: "fatal: bad", Stdout: "ignored", Err: errors.New("exit status 1")},
			want: "git commit: fatal: bad",
		},
		{
			name: "stdout when stderr is empty",
			err:  &ExecError{Args: []string{"commit"}, Stdout: "nothing to commit, working tree clean", Err: errors.New("exit status 1")},
			want: "git commit: nothing to commit, working tree clean",
		},
		{
			name: "the error itself when git said nothing",
			err:  &ExecError{Args: []string{"commit"}, Err: errors.New("exit status 1")},
			want: "git commit: exit status 1",
		},
		{
			name: "several lines on one",
			err:  &ExecError{Args: []string{"commit"}, Stdout: "On branch main\n\nnothing to commit, working tree clean\n", Err: errors.New("exit status 1")},
			want: "git commit: On branch main; nothing to commit, working tree clean",
		},
		{
			name: "a stream too long to carry",
			err:  &ExecError{Args: []string{"diff"}, Stderr: strings.Repeat("x", maxDiagnostic+10), Err: errors.New("exit status 1")},
			want: "git diff: " + strings.Repeat("x", maxDiagnostic) + "…",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
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
