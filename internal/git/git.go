// Package git wraps the git binary. It is deliberately not an interface:
// autogit must commit exactly the way the user's git does — GPG/SSH signing,
// core.hooksPath, credential helpers, gitattributes — and only the real binary
// does that.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Conte777/autogit/internal/proc"
)

// EmptyTree is git's well-known empty tree object. `git diff HEAD` fails in a
// repository with no commits; diffing against this sentinel does not.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

const (
	defaultTimeout       = 30 * time.Second
	defaultCommitTimeout = 30 * time.Second
	waitDelay            = 5 * time.Second
)

// ErrNotARepo is returned by Open when path is outside any git repository.
var ErrNotARepo = errors.New("not a git repository")

// StateError reports a repository state that makes committing unsafe.
type StateError struct{ Reason string }

func (e *StateError) Error() string { return e.Reason }

// ExecError carries git's own stderr, which is the only useful diagnostic.
type ExecError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *ExecError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), e.Stderr)
	}
	return fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
}

func (e *ExecError) Unwrap() error { return e.Err }

// Options configures a Repo.
type Options struct {
	// Interactive allows git to ask for input — on the terminal and through
	// the askpass helpers. Off wherever autogit itself has nobody to ask, so
	// that git cannot stop for a question autogit promised would not come.
	// Credential storage helpers answer without asking and stay enabled.
	Interactive bool
	// CommitTimeout bounds `git commit`. A signing setup that waits on
	// pinentry would otherwise hang the caller forever. 0 uses the default.
	CommitTimeout time.Duration
}

// Repo is an open git working tree.
type Repo struct {
	root string
	opts Options
}

// Open locates the working tree containing dir.
func Open(ctx context.Context, dir string, opts Options) (*Repo, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if fi, statErr := os.Stat(abs); statErr != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%w: cannot enter %s", ErrNotARepo, dir)
	}

	probe := &Repo{root: abs, opts: opts}
	bare, err := probe.run(ctx, defaultTimeout, "", "rev-parse", "--is-bare-repository")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotARepo, dir)
	}
	if strings.TrimSpace(bare) == "true" {
		return nil, &StateError{Reason: "bare repository: there is no working tree to commit"}
	}
	top, err := probe.run(ctx, defaultTimeout, "", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotARepo, dir)
	}
	return &Repo{root: strings.TrimSpace(top), opts: opts}, nil
}

// Root is the absolute path of the working tree.
func (r *Repo) Root() string { return r.root }

// Interactive reports whether git is allowed to ask this repo's caller anything.
func (r *Repo) Interactive() bool { return r.opts.Interactive }

func (r *Repo) commitTimeout() time.Duration {
	if r.opts.CommitTimeout > 0 {
		return r.opts.CommitTimeout
	}
	return defaultCommitTimeout
}

func (r *Repo) run(ctx context.Context, timeout time.Duration, stdin string, args ...string) (string, error) {
	out, _, err := r.runBounded(ctx, timeout, 0, stdin, args...)
	return out, err
}

func (r *Repo) runBounded(ctx context.Context, timeout time.Duration, limit int, stdin string, args ...string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	proc.Isolate(cmd)
	cmd.Cancel = func() error { return proc.Kill(cmd) }
	cmd.Dir = r.root
	cmd.Env = r.env()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	stdout := &capped{limit: limit, stop: cancel}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay

	err := cmd.Run()
	if stdout.over && errors.Is(ctx.Err(), context.Canceled) {
		return stdout.String(), true, nil
	}
	if err != nil {
		if ctx.Err() != nil {
			err = fmt.Errorf("%w (%s)", ctx.Err(), timeout)
		}
		return stdout.String(), false, &ExecError{Args: args, Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}
	return stdout.String(), false, nil
}

type capped struct {
	limit int
	stop  context.CancelFunc
	buf   []byte
	over  bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.limit - len(c.buf); c.limit > 0 && room < len(p) {
		if room > 0 {
			c.buf = append(c.buf, p[:room]...)
		}
		if !c.over {
			c.over = true
			c.stop()
		}
		return len(p), nil
	}
	c.buf = append(c.buf, p...)
	return len(p), nil
}

func (c *capped) String() string { return string(c.buf) }

func (r *Repo) env() []string {
	// LC_ALL pins git's messages and porcelain wording to English: everything
	// below parses git output, and the user's locale must not change it.
	// GIT_DIR is inherited too: ADR-0001 binds autogit to behave as the user's
	// own git does, and `git commit` under GIT_DIR=~/.dotfiles commits there.
	// Isolating from it is a test concern — see UnsetRepoLocation.
	env := append(os.Environ(), "LC_ALL=C", "LANG=C")
	if !r.opts.Interactive {
		env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=")
	}
	return env
}

// exitCode reports git's exit status, or -1 when the command did not run.
func exitCode(err error) int {
	var ee *ExecError
	if errors.As(err, &ee) {
		var xe *exec.ExitError
		if errors.As(ee.Err, &xe) {
			return xe.ExitCode()
		}
	}
	return -1
}

// gitDir resolves $GIT_DIR for this working tree, honouring worktrees where
// .git is a file pointing elsewhere.
func (r *Repo) gitDir(ctx context.Context) (string, error) {
	out, err := r.run(ctx, defaultTimeout, "", "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Branch is the checked-out branch. Name is a short hash when Detached.
type Branch struct {
	Name     string
	Detached bool
}

// Current reports the checked-out branch.
func (r *Repo) Current(ctx context.Context) (Branch, error) {
	out, err := r.run(ctx, defaultTimeout, "", "branch", "--show-current")
	if err != nil {
		return Branch{}, err
	}
	if name := strings.TrimSpace(out); name != "" {
		return Branch{Name: name}, nil
	}
	// An unborn branch still reports its name above, so an empty name here can
	// only mean a detached HEAD.
	out, err = r.run(ctx, defaultTimeout, "", "rev-parse", "--short", "HEAD")
	if err != nil {
		return Branch{}, err
	}
	return Branch{Name: strings.TrimSpace(out), Detached: true}, nil
}

// HasCommits reports whether HEAD resolves — false in a fresh repository.
func (r *Repo) HasCommits(ctx context.Context) bool {
	_, err := r.run(ctx, defaultTimeout, "", "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

// StageAll runs `git add -A`.
func (r *Repo) StageAll(ctx context.Context) error {
	_, err := r.run(ctx, defaultTimeout, "", "add", "-A", "--", ".")
	return err
}

// StageTracked runs `git add -u`.
func (r *Repo) StageTracked(ctx context.Context) error {
	_, err := r.run(ctx, defaultTimeout, "", "add", "-u", "--", ".")
	return err
}

// HasStaged reports whether the index differs from HEAD.
func (r *Repo) HasStaged(ctx context.Context) (bool, error) {
	_, err := r.run(ctx, defaultTimeout, "", "diff", "--cached", "--quiet")
	switch exitCode(err) {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		if err == nil {
			return false, nil
		}
		return false, err
	}
}

// WorktreeStatus describes what is left outside the index.
type WorktreeStatus struct {
	ModifiedTracked bool
	Untracked       bool
}

// Status reports unstaged work, split by whether git already tracks the file.
func (r *Repo) Status(ctx context.Context) (WorktreeStatus, error) {
	out, err := r.run(ctx, defaultTimeout, "", "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return WorktreeStatus{}, err
	}
	var st WorktreeStatus
	for _, entry := range splitZ(out) {
		if len(entry) < 3 {
			continue
		}
		switch {
		case entry[:2] == "??":
			st.Untracked = true
		case entry[1] != ' ':
			st.ModifiedTracked = true
		}
	}
	return st, nil
}

func splitZ(s string) []string {
	parts := strings.Split(s, "\x00")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Result is a commit that actually landed.
type Result struct {
	Hash      string
	ShortHash string
	Message   string
}

// Commit writes msg through stdin and reports what git actually recorded.
func (r *Repo) Commit(ctx context.Context, msg string) (Result, error) {
	// -F - because `-m` is not what git's own editor path does, and
	// --cleanup=whitespace because the default strips body lines starting
	// with '#'.
	if _, err := r.run(ctx, r.commitTimeout(), msg, "commit", "--cleanup=whitespace", "-F", "-"); err != nil {
		return Result{}, err
	}
	// A commit-msg hook may have rewritten the message; report what landed.
	out, err := r.run(ctx, defaultTimeout, "", "log", "-1", "--format=%H%x00%h%x00%B")
	if err != nil {
		return Result{}, err
	}
	parts := strings.SplitN(out, "\x00", 3)
	if len(parts) < 3 {
		return Result{}, fmt.Errorf("cannot read back the commit that was just created")
	}
	return Result{
		Hash:      parts[0],
		ShortHash: parts[1],
		Message:   strings.TrimRight(parts[2], "\n"),
	}, nil
}

// BranchExists reports whether a local branch of that name is already there.
func (r *Repo) BranchExists(ctx context.Context, name string) bool {
	_, err := r.run(ctx, defaultTimeout, "", "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// CreateBranch creates name and switches to it.
func (r *Repo) CreateBranch(ctx context.Context, name string) error {
	_, err := r.run(ctx, defaultTimeout, "", "switch", "-c", name)
	return err
}

// Subjects returns the subject lines of the last n non-merge commits.
func (r *Repo) Subjects(ctx context.Context, n int) ([]string, error) {
	if !r.HasCommits(ctx) {
		return nil, nil
	}
	out, err := r.run(ctx, defaultTimeout, "",
		"log", fmt.Sprintf("-n%d", n), "--no-merges", "--format=%s")
	if err != nil {
		return nil, err
	}
	var subjects []string
	for ln := range strings.SplitSeq(out, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			subjects = append(subjects, ln)
		}
	}
	return subjects, nil
}
