package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// diverged builds `main` and `side` with conflicting edits to a.txt, plus an
// `extra` branch touching only c.txt, which every branch can take cleanly.
func diverged(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")

	runGit(t, dir, "switch", "-c", "extra")
	write(t, dir, "c.txt", "extra only\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "extra")

	runGit(t, dir, "switch", "-c", "side", "main")
	write(t, dir, "a.txt", "side\n")
	write(t, dir, "b.txt", "side only\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "side")

	runGit(t, dir, "switch", "main")
	write(t, dir, "a.txt", "main\n")
	runGit(t, dir, "commit", "-am", "main")
	return dir
}

func state(t *testing.T, dir string) State {
	t.Helper()
	st, err := open(t, dir).State(context.Background())
	if err != nil {
		t.Fatalf("State(): %v", err)
	}
	return st
}

func TestStateDetectsEachOperation(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		dir := diverged(t)
		st := state(t, dir)
		if st.Op != OpNone || st.Locked {
			t.Fatalf("State() = %+v, want the zero state", st)
		}
		if err := st.Blocked(); err != nil {
			t.Fatalf("Blocked() on a clean repo = %v, want nil", err)
		}
	})

	t.Run("merge", func(t *testing.T) {
		dir := diverged(t)
		tryGit(dir, "merge", "side")
		assertState(t, state(t, dir), OpMerge, "a merge is in progress")
	})

	t.Run("squash merge", func(t *testing.T) {
		dir := diverged(t)
		// A squash merge writes SQUASH_MSG and no ref: git considers the tree
		// ordinary afterwards, and so must Blocked().
		tryGit(dir, "merge", "--squash", "side")
		st := state(t, dir)
		if st.Op != OpSquash {
			t.Fatalf("State() after merge --squash = %+v, want OpSquash", st)
		}
		if err := st.Blocked(); err != nil {
			t.Fatalf("Blocked() after merge --squash = %v, want nil", err)
		}
		if !st.HasPreparedMessage() {
			t.Fatal("HasPreparedMessage() = false after merge --squash")
		}
	})

	t.Run("cherry-pick", func(t *testing.T) {
		dir := diverged(t)
		tryGit(dir, "cherry-pick", "side")
		assertState(t, state(t, dir), OpCherryPick, "a cherry-pick is in progress")
	})

	t.Run("revert", func(t *testing.T) {
		dir := diverged(t)
		runGit(t, dir, "switch", "side")
		// Reverting the commit that created a.txt's `side` content conflicts
		// with nothing, so -n is what keeps REVERT_HEAD around. A conflicting
		// revert would work too; this shape is the one autogit must handle.
		tryGit(dir, "revert", "--no-commit", "HEAD")
		st := state(t, dir)
		if st.Op != OpRevert {
			t.Fatalf("State() after revert -n = %+v, want OpRevert", st)
		}
		if err := st.Blocked(); err == nil || !strings.Contains(err.Error(), "revert") {
			t.Fatalf("Blocked() = %v, want a revert StateError", err)
		}
	})

	t.Run("rebase", func(t *testing.T) {
		dir := diverged(t)
		tryGit(dir, "rebase", "side")
		assertState(t, state(t, dir), OpRebase, "rebase")
	})

	t.Run("bisect", func(t *testing.T) {
		dir := diverged(t)
		runGit(t, dir, "bisect", "start")
		runGit(t, dir, "bisect", "bad")
		runGit(t, dir, "bisect", "good", "HEAD~1")
		assertState(t, state(t, dir), OpBisect, "a bisect is in progress")
	})

	t.Run("index.lock", func(t *testing.T) {
		dir := diverged(t)
		if err := os.WriteFile(filepath.Join(dir, ".git", "index.lock"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		st := state(t, dir)
		if !st.Locked || st.Op != OpNone {
			t.Fatalf("State() with index.lock = %+v, want {OpNone true}", st)
		}
		if err := st.Blocked(); err == nil || !strings.Contains(err.Error(), "index.lock") {
			t.Fatalf("Blocked() = %v, want an index.lock StateError", err)
		}
		if st.HasPreparedMessage() {
			t.Fatal("HasPreparedMessage() = true while another git process holds the index")
		}
	})
}

func assertState(t *testing.T, st State, want Operation, reason string) {
	t.Helper()
	if st.Op != want {
		t.Fatalf("State() = %+v, want Op %q", st, want)
	}
	err := st.Blocked()
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("Blocked() = %v, want a StateError mentioning %q", err, reason)
	}
}

// TestStatePrefersTheOperationOverAStaleSquashMsg pins the detection order: a
// SQUASH_MSG that outlived its commit must not hide a merge that is still open.
func TestStatePrefersTheOperationOverAStaleSquashMsg(t *testing.T) {
	dir := diverged(t)
	tryGit(dir, "merge", "side")
	if err := os.WriteFile(filepath.Join(dir, ".git", "SQUASH_MSG"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := state(t, dir); st.Op != OpMerge {
		t.Fatalf("State() = %+v, want OpMerge", st)
	}
}

func TestPreparedMessageStripsTheConflictBlock(t *testing.T) {
	dir := diverged(t)
	tryGit(dir, "merge", "side")

	raw, err := os.ReadFile(filepath.Join(dir, ".git", "MERGE_MSG"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "# Conflicts:") {
		t.Fatalf("MERGE_MSG carries no comment block to strip:\n%s", raw)
	}

	msg, err := open(t, dir).PreparedMessage(context.Background(), OpMerge)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "Merge branch 'side'" {
		t.Fatalf("PreparedMessage() = %q, want %q", msg, "Merge branch 'side'")
	}
}

func TestPreparedMessageReadsSquashMsg(t *testing.T) {
	dir := diverged(t)
	tryGit(dir, "merge", "--squash", "side")

	msg, err := open(t, dir).PreparedMessage(context.Background(), OpSquash)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(msg, "Squashed commit of the following:") {
		t.Fatalf("PreparedMessage(OpSquash) = %q", msg)
	}
}

// The two are not symmetric on the installed git: a clean `revert -n` leaves
// REVERT_HEAD, a clean `cherry-pick -n` leaves only MERGE_MSG. Detection by ref
// alone would miss the pick and rewrite the original author's message.
func TestCleanNoCommitPickAndRevert(t *testing.T) {
	for _, tc := range []struct {
		name string
		want Operation
		run  func(dir string)
	}{
		{"cherry-pick", OpPrepared, func(dir string) {
			runGit(t, dir, "cherry-pick", "-n", "extra")
		}},
		{"revert", OpRevert, func(dir string) {
			runGit(t, dir, "switch", "side")
			runGit(t, dir, "revert", "-n", "HEAD")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := diverged(t)
			tc.run(dir)

			st := state(t, dir)
			if st.Op != tc.want {
				t.Fatalf("State() after a clean `%s -n` = %+v, want Op %q", tc.name, st, tc.want)
			}
			if !st.HasPreparedMessage() {
				t.Fatal("HasPreparedMessage() = false, so the message would be regenerated")
			}
			// A bare MERGE_MSG was invisible to the old check, so finding it
			// must not turn into a refusal that did not exist before.
			if st.Op == OpPrepared && st.Blocked() != nil {
				t.Fatalf("Blocked() = %v, want nil", st.Blocked())
			}

			msg, err := open(t, dir).PreparedMessage(context.Background(), st.Op)
			if err != nil {
				t.Fatal(err)
			}
			if msg == "" {
				t.Fatalf("git wrote no message for a clean `%s -n`", tc.name)
			}
			if strings.Contains(msg, "#") {
				t.Fatalf("PreparedMessage() kept a comment: %q", msg)
			}
		})
	}
}

// A commit consumes MERGE_MSG and SQUASH_MSG, which is what keeps the bare
// MERGE_MSG fallback from reusing one message forever.
func TestCommittingClearsThePreparedMessage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(dir string)
	}{
		{"merge", func(dir string) {
			runGit(t, dir, "merge", "--no-commit", "extra")
		}},
		{"squash", func(dir string) {
			runGit(t, dir, "merge", "--squash", "extra")
		}},
		{"cherry-pick", func(dir string) {
			runGit(t, dir, "cherry-pick", "-n", "extra")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := diverged(t)
			tc.setup(dir)
			if !state(t, dir).HasPreparedMessage() {
				t.Fatal("no prepared message to begin with")
			}
			runGit(t, dir, "commit", "--no-edit")

			st := state(t, dir)
			if st.Op != OpNone {
				t.Fatalf("State() after the commit = %+v, want the zero state", st)
			}
		})
	}
}

func TestUnmergedListsTheConflictedPaths(t *testing.T) {
	ctx := context.Background()
	dir := diverged(t)
	r := open(t, dir)

	if paths, err := r.Unmerged(ctx); err != nil || len(paths) != 0 {
		t.Fatalf("Unmerged() before the merge = %v, %v, want empty", paths, err)
	}

	tryGit(dir, "merge", "side")
	paths, err := r.Unmerged(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "a.txt" {
		t.Fatalf("Unmerged() = %v, want [a.txt]", paths)
	}

	write(t, dir, "a.txt", "resolved\n")
	runGit(t, dir, "add", "a.txt")
	if paths, err = r.Unmerged(ctx); err != nil || len(paths) != 0 {
		t.Fatalf("Unmerged() after resolving = %v, %v, want empty", paths, err)
	}
}
