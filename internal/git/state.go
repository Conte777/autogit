package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Operation is the multi-step git operation the repository is in the middle of.
type Operation string

const (
	OpNone       Operation = ""
	OpMerge      Operation = "merge"
	OpSquash     Operation = "squash merge"
	OpCherryPick Operation = "cherry-pick"
	OpRevert     Operation = "revert"
	OpRebase     Operation = "rebase"
	OpBisect     Operation = "bisect"
	// OpSequence is a multi-commit cherry-pick or revert. Only `--continue`
	// advances its todo list, so committing one step would silently drop the
	// rest, exactly as it would mid-rebase.
	OpSequence Operation = "multi-commit cherry-pick or revert"
	// OpPrepared is a message with no ref beside it. A clean `cherry-pick -n`
	// writes MERGE_MSG and nothing else, so detection by ref alone would miss
	// it and rewrite the original author's message.
	OpPrepared Operation = "prepared"
)

// State is what the git directory says about work already under way.
type State struct {
	Op Operation
	// Locked means another git process holds index.lock. It excludes Op: a
	// repository nobody else may touch is the only thing worth reporting.
	Locked bool
}

// State reads the git directory once and reports what it found. The order
// below is deliberate: a stale SQUASH_MSG outlives the commit it was written
// for, so every real in-progress operation is looked for first.
func (r *Repo) State(ctx context.Context) (State, error) {
	dir, err := r.gitDir(ctx)
	if err != nil {
		return State{}, err
	}
	exists := func(name string) bool {
		_, statErr := os.Stat(filepath.Join(dir, name))
		return statErr == nil
	}
	if exists("index.lock") {
		return State{Locked: true}, nil
	}
	for _, c := range []struct {
		path string
		op   Operation
	}{
		{"rebase-merge", OpRebase},
		{"rebase-apply", OpRebase},
		// Ahead of the two *_HEAD refs it sits beside: a sequence in progress
		// is the more important half of the truth.
		{"sequencer", OpSequence},
		{"BISECT_LOG", OpBisect},
		{"REVERT_HEAD", OpRevert},
		{"CHERRY_PICK_HEAD", OpCherryPick},
		{"MERGE_HEAD", OpMerge},
		{"SQUASH_MSG", OpSquash},
		{"MERGE_MSG", OpPrepared},
	} {
		if exists(c.path) {
			return State{Op: c.op}, nil
		}
	}
	return State{}, nil
}

// Blocked reports the states where an automated commit would do something the
// user did not ask for. A squash merge and a bare message are not among them:
// neither leaves a ref behind, git considers the working tree ordinary, and
// refusing them would break commands that work today.
func (s State) Blocked() error {
	if s.Locked {
		return &StateError{Reason: "index.lock exists: another git process is running"}
	}
	var what string
	switch s.Op {
	case OpMerge:
		what = "a merge is in progress"
	case OpCherryPick:
		what = "a cherry-pick is in progress"
	case OpRevert:
		what = "a revert is in progress"
	case OpRebase:
		what = "a rebase or `git am` is in progress"
	case OpBisect:
		what = "a bisect is in progress"
	case OpSequence:
		return &StateError{Reason: "a multi-commit cherry-pick or revert is in progress; " +
			"only `git cherry-pick --continue` can advance it"}
	default:
		return nil
	}
	return &StateError{Reason: what + "; finish or abort it first"}
}

// HasPreparedMessage reports whether git has already written the message this
// commit should carry.
func (s State) HasPreparedMessage() bool {
	switch s.Op {
	case OpMerge, OpSquash, OpCherryPick, OpRevert, OpPrepared:
		return true
	default:
		return false
	}
}

// PreparedMessage returns the message git wrote for op, comments stripped. A
// missing or empty file is not an error: the caller decides what that means.
func (r *Repo) PreparedMessage(ctx context.Context, op Operation) (string, error) {
	var name string
	switch op {
	case OpMerge, OpCherryPick, OpRevert, OpPrepared:
		name = "MERGE_MSG"
	case OpSquash:
		name = "SQUASH_MSG"
	default:
		return "", nil
	}
	dir, err := r.gitDir(ctx)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a path inside the repository's own git directory
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return dropConflictBlock(string(raw)), nil
}

// conflictHeader matches the `# Conflicts:` line that opens the block git
// appends, allowing for a core.commentChar other than '#'.
var conflictHeader = regexp.MustCompile(`^\S Conflicts:[ \t]*$`)

// dropConflictBlock removes git's trailing conflict listing and nothing else.
// Stripping every comment line instead would delete an author's own body — a
// cherry-picked `#123 the ticket` is content, not commentary.
func dropConflictBlock(text string) string {
	lines := strings.Split(text, "\n")
	for i, ln := range lines {
		if conflictHeader.MatchString(ln) {
			lines = lines[:i]
			break
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), " \t\n")
}

// Unmerged lists the paths still carrying conflict markers.
func (r *Repo) Unmerged(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, defaultTimeout, "", "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil, err
	}
	return splitZ(out), nil
}
