package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/provider/mock"
	"github.com/Conte777/autogit/internal/ui"
)

func TestCommitStagedOnly(t *testing.T) {
	e := newEnv(t, "feat: add the greeting file")
	e.write("a.txt", "hello\n")
	e.git("add", "a.txt")
	e.write("b.txt", "not staged\n")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if err != nil {
		t.Fatal(err)
	}
	if got.Message != "feat: add the greeting file" {
		t.Errorf("Message = %q", got.Message)
	}
	if got.ShortHash == "" {
		t.Error("ShortHash is empty")
	}
	if files := e.git("show", "--name-only", "--format=", "HEAD"); strings.Contains(files, "b.txt") {
		t.Errorf("an unstaged file was committed:\n%s", files)
	}
}

func TestCommitAllPicksUpUntracked(t *testing.T) {
	e := newEnv(t, "feat: add both files")
	e.write("a.txt", "one\n")
	e.write("b.txt", "two\n")

	if _, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageAll}); err != nil {
		t.Fatal(err)
	}
	files := e.git("show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, "a.txt") || !strings.Contains(files, "b.txt") {
		t.Errorf("--all missed an untracked file:\n%s", files)
	}
}

func TestCommitTrackedIgnoresUntracked(t *testing.T) {
	e := newEnv(t, "fix: update the tracked file")
	e.commitFile("a.txt", "one\n", "init")
	e.write("a.txt", "two\n")
	e.write("new.txt", "untracked\n")

	if _, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageTracked}); err != nil {
		t.Fatal(err)
	}
	files := e.git("show", "--name-only", "--format=", "HEAD")
	if strings.Contains(files, "new.txt") {
		t.Errorf("--tracked committed an untracked file:\n%s", files)
	}
}

func TestCommitEmptyStageIsNothingToCommit(t *testing.T) {
	e := newEnv(t, "feat: never asked for")
	e.commitFile("a.txt", "one\n", "init")

	_, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if !app.IsNothingToCommit(err) {
		t.Fatalf("err = %v, want ErrNothingToCommit", err)
	}
	if e.prov.Sessions != 0 {
		t.Error("the provider was called with nothing to describe")
	}
}

func TestCommitEmptyStageDirtyTreeSuggestsTheFlags(t *testing.T) {
	e := newEnv(t, "feat: never asked for")
	e.commitFile("a.txt", "one\n", "init")
	e.write("a.txt", "two\n")

	_, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if !app.IsNothingToCommit(err) {
		t.Fatalf("err = %v, want ErrNothingToCommit", err)
	}
	if !strings.Contains(err.Error(), "--all") || !strings.Contains(err.Error(), "--tracked") {
		t.Errorf("err = %v, want it to name the flags that would fix it", err)
	}
}

func TestCommitAsksWhatToStageInATerminal(t *testing.T) {
	e := newEnv(t, "feat: add everything")
	e.commitFile("a.txt", "one\n", "init")
	e.write("a.txt", "two\n")
	e.write("new.txt", "untracked\n")

	a := e.answering(scripted{choice: "a"})
	if _, err := a.Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged}); err != nil {
		t.Fatal(err)
	}
	if files := e.git("show", "--name-only", "--format=", "HEAD"); !strings.Contains(files, "new.txt") {
		t.Errorf("answering 'everything' did not stage the untracked file:\n%s", files)
	}
}

func TestCommitCancelledAtTheStagingQuestion(t *testing.T) {
	e := newEnv(t, "feat: add everything")
	e.commitFile("a.txt", "one\n", "init")
	e.write("a.txt", "two\n")
	before := e.head()

	a := e.answering(scripted{choice: "c"})
	_, err := a.Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if !errors.Is(err, app.ErrCanceled) {
		t.Fatalf("err = %v, want ErrCanceled", err)
	}
	if e.head() != before {
		t.Error("cancelling still created a commit")
	}
}

func TestProtectedBranchNeedsForce(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.ProtectedBranches = []string{"main", "release/*"}
	e.write("a.txt", "one\n")
	e.git("add", ".")

	_, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	var prot *app.ProtectedBranchError
	if !errors.As(err, &prot) {
		t.Fatalf("err = %v, want *ProtectedBranchError on main", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("err = %v, want the fix spelled out", err)
	}

	if _, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged, Force: true}); err != nil {
		t.Fatalf("--force did not get through: %v", err)
	}
}

func TestProtectedBranchGlobMatches(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.ProtectedBranches = []string{"main", "release/*"}
	e.commitFile("a.txt", "one\n", "init")
	e.git("switch", "-c", "release/1.2")
	e.write("a.txt", "two\n")
	e.git("add", ".")

	_, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	var prot *app.ProtectedBranchError
	if !errors.As(err, &prot) {
		t.Fatalf("err = %v; release/* must match release/1.2", err)
	}
}

func TestProtectedBranchQuestionInATerminal(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.ProtectedBranches = []string{"main"}
	e.write("a.txt", "one\n")
	e.git("add", ".")

	if _, err := e.answering(scripted{confirm: true}).Commit(
		context.Background(), app.CommitRequest{Stage: app.StageStaged}); err != nil {
		t.Fatalf("answering yes did not let the commit through: %v", err)
	}
}

// consentRecorder is the channel to the user an MCP-like surface supplies.
type consentRecorder struct {
	grant    bool
	err      error
	branches []string
}

func (c *consentRecorder) ask(_ context.Context, branch string) (bool, error) {
	c.branches = append(c.branches, branch)
	return c.grant, c.err
}

func TestProtectedBranchConsentLetsTheCommitThrough(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.ProtectedBranches = []string{"main"}
	e.cfg.MCP.AllowProtectedBranch = true
	e.write("a.txt", "one\n")
	e.git("add", ".")

	consent := &consentRecorder{grant: true}
	if _, err := e.app().Commit(context.Background(), app.CommitRequest{
		Stage: app.StageStaged, Consent: consent.ask,
	}); err != nil {
		t.Fatalf("consent did not let the commit through: %v", err)
	}
	if want := []string{"main"}; len(consent.branches) != 1 || consent.branches[0] != want[0] {
		t.Errorf("asked about %v, want %v", consent.branches, want)
	}
}

func TestProtectedBranchWithheldConsentCommitsNothing(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.ProtectedBranches = []string{"main"}
	e.cfg.MCP.AllowProtectedBranch = true
	e.write("a.txt", "one\n")
	e.git("add", ".")

	consent := &consentRecorder{grant: false}
	_, err := e.app().Commit(context.Background(), app.CommitRequest{
		Stage: app.StageStaged, Consent: consent.ask,
	})
	var withheld *app.ConsentError
	if !errors.As(err, &withheld) {
		t.Fatalf("err = %v, want *ConsentError", err)
	}
	if withheld.Branch != "main" {
		t.Errorf("Branch = %q", withheld.Branch)
	}
	if e.head() != "" {
		t.Error("a refusal still created a commit")
	}
}

func TestProtectedBranchConsentNotOfferedUntilConfigured(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.ProtectedBranches = []string{"main"}
	e.write("a.txt", "one\n")
	e.git("add", ".")

	consent := &consentRecorder{grant: true}
	_, err := e.app().Commit(context.Background(), app.CommitRequest{
		Stage: app.StageStaged, Consent: consent.ask,
	})
	var prot *app.ProtectedBranchError
	if !errors.As(err, &prot) {
		t.Fatalf("err = %v, want *ProtectedBranchError while mcp.allowProtectedBranch is off", err)
	}
	if len(consent.branches) != 0 {
		t.Errorf("the user was asked anyway: %v", consent.branches)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("err = %v, want the human path spelled out", err)
	}
}

func TestProtectedBranchUnreachableUserIsNotARefusal(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.ProtectedBranches = []string{"main"}
	e.cfg.MCP.AllowProtectedBranch = true
	e.write("a.txt", "one\n")
	e.git("add", ".")

	unreachable := errors.New("client cannot ask the user")
	consent := &consentRecorder{err: unreachable}
	_, err := e.app().Commit(context.Background(), app.CommitRequest{
		Stage: app.StageStaged, Consent: consent.ask,
	})
	if !errors.Is(err, unreachable) {
		t.Fatalf("err = %v, want the channel's own failure", err)
	}
	var withheld *app.ConsentError
	if errors.As(err, &withheld) {
		t.Error("a client that cannot ask was reported as a user saying no")
	}
	if e.head() != "" {
		t.Error("the commit landed anyway")
	}
}

func TestForceSkipsTheConsentChannel(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.ProtectedBranches = []string{"main"}
	e.cfg.MCP.AllowProtectedBranch = true
	e.write("a.txt", "one\n")
	e.git("add", ".")

	consent := &consentRecorder{grant: false}
	if _, err := e.app().Commit(context.Background(), app.CommitRequest{
		Stage: app.StageStaged, Force: true, Consent: consent.ask,
	}); err != nil {
		t.Fatalf("--force did not get through: %v", err)
	}
	if len(consent.branches) != 0 {
		t.Errorf("--force still asked the user: %v", consent.branches)
	}
}

func TestPreviewNeverAsksForConsent(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.ProtectedBranches = []string{"main"}
	e.cfg.MCP.AllowProtectedBranch = true
	e.write("a.txt", "one\n")
	e.git("add", ".")

	consent := &consentRecorder{grant: false}
	if _, err := e.app().Commit(context.Background(), app.CommitRequest{
		Stage: app.StageStaged, Preview: true, Consent: consent.ask,
	}); err != nil {
		t.Fatalf("commit-msg refused on a protected branch: %v", err)
	}
	if len(consent.branches) != 0 {
		t.Errorf("a preview asked the user: %v", consent.branches)
	}
}

func TestCommitMsgWorksOnProtectedBranchAndCommitsNothing(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.ProtectedBranches = []string{"main"}
	e.write("a.txt", "one\n")
	e.git("add", ".")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged, Preview: true})
	if err != nil {
		t.Fatalf("commit-msg refused on a protected branch: %v", err)
	}
	if !got.Preview || got.Message == "" {
		t.Errorf("result = %+v", got)
	}
	if e.head() != "" {
		t.Error("commit-msg created a commit")
	}
}

func TestCommitMsgOnRepoWithoutCommits(t *testing.T) {
	e := newEnv(t, "feat: add the first file")
	e.write("a.txt", "one\n")
	e.git("add", ".")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged, Preview: true})
	if err != nil {
		t.Fatalf("a repository with no commits broke commit-msg: %v", err)
	}
	if got.Message == "" {
		t.Error("no message produced")
	}
}

func TestCommitOnDetachedHeadSkipsTicketExtraction(t *testing.T) {
	e := newEnv(t, "feat: add the second file")
	e.cfg.Preset = "ticket"
	e.commitFile("a.txt", "one\n", "init")
	e.git("switch", "-c", "CUS-42/add-thing")
	e.commitFile("b.txt", "two\n", "second")
	e.git("checkout", "--detach", "HEAD")

	e.write("c.txt", "three\n")
	e.git("add", ".")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if err != nil {
		t.Fatalf("commit on a detached HEAD failed: %v", err)
	}
	if got.Message != "feat: add the second file" {
		t.Errorf("Message = %q", got.Message)
	}
	if prompt := e.prov.SessionTurns(0)[0]; strings.Contains(prompt, "CUS-42") {
		t.Errorf("a ticket was extracted from a detached HEAD:\n%s", prompt)
	}
}

// diverge builds `main` and `side` with conflicting edits to a.txt, and an
// `extra` branch touching only c.txt, which merges into anything cleanly.
func (e *env) diverge() {
	e.t.Helper()
	e.commitFile("a.txt", "one\n", "init")
	e.git("switch", "-c", "extra")
	e.commitFile("c.txt", "extra only\n", "extra")
	e.git("switch", "-c", "side", "main")
	e.commitFile("a.txt", "side\n", "side")
	e.git("switch", "main")
	e.commitFile("a.txt", "main\n", "main")
}

func TestCommitUsesGitsOwnMergeMessage(t *testing.T) {
	e := newEnv(t)
	e.diverge()
	_ = tryGit(e.dir, "merge", "side")
	e.write("a.txt", "resolved\n")
	e.git("add", "a.txt")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if err != nil {
		t.Fatal(err)
	}
	if got.Message != "Merge branch 'side'" {
		t.Errorf("Message = %q, want %q", got.Message, "Merge branch 'side'")
	}
	if got.Prepared != git.OpMerge {
		t.Errorf("Prepared = %q, want %q", got.Prepared, git.OpMerge)
	}
	if e.prov.Sessions != 0 {
		t.Errorf("the provider was asked %d time(s) for a message git had already written", e.prov.Sessions)
	}
	if parents := strings.Fields(e.git("log", "-1", "--format=%P")); len(parents) != 2 {
		t.Errorf("the commit has %d parent(s), want 2", len(parents))
	}
}

// `git merge -s ours` records a merge whose tree equals HEAD's. Refusing it for
// an empty diff would leave the user with a merge they cannot finish.
func TestCommitAllowsAMergeWithNoDiff(t *testing.T) {
	e := newEnv(t)
	e.diverge()
	e.git("merge", "--no-commit", "-s", "ours", "side")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Message, "Merge branch 'side'") {
		t.Errorf("Message = %q", got.Message)
	}
	if e.prov.Sessions != 0 {
		t.Errorf("the provider was asked %d time(s)", e.prov.Sessions)
	}
}

func TestCommitUsesTheSquashMessage(t *testing.T) {
	e := newEnv(t)
	e.diverge()
	e.git("merge", "--squash", "extra")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Message, "Squashed commit of the following:") {
		t.Errorf("Message = %q, want git's SQUASH_MSG", got.Message)
	}
	if got.Prepared != git.OpSquash {
		t.Errorf("Prepared = %q, want %q", got.Prepared, git.OpSquash)
	}
	if e.prov.Sessions != 0 {
		t.Errorf("the provider was asked %d time(s)", e.prov.Sessions)
	}
}

// A clean `cherry-pick -n` leaves MERGE_MSG and no ref at all, so the message
// is only reachable if a bare MERGE_MSG counts. Rewriting it would replace the
// original author's message with our own.
func TestCommitKeepsACleanCherryPicksMessage(t *testing.T) {
	e := newEnv(t)
	e.diverge()
	e.git("cherry-pick", "-n", "extra")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if err != nil {
		t.Fatal(err)
	}
	if got.Message != "extra" {
		t.Errorf("Message = %q, want the picked commit's own message", got.Message)
	}
	if got.Prepared != git.OpPrepared {
		t.Errorf("Prepared = %q, want %q", got.Prepared, git.OpPrepared)
	}
	if e.prov.Sessions != 0 {
		t.Errorf("the provider was asked %d time(s)", e.prov.Sessions)
	}
}

// The conflict check runs before staging: `--all` would otherwise commit a file
// full of conflict markers.
func TestCommitRefusesUnresolvedConflicts(t *testing.T) {
	e := newEnv(t)
	e.diverge()
	_ = tryGit(e.dir, "merge", "side")
	before := e.head()

	_, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageAll})
	var state *git.StateError
	if !errors.As(err, &state) || !strings.Contains(err.Error(), "a.txt") {
		t.Fatalf("err = %v, want a *git.StateError naming a.txt", err)
	}
	if e.head() != before {
		t.Error("a commit was created despite the open conflict")
	}
	if unmerged := e.git("diff", "--name-only", "--diff-filter=U"); !strings.Contains(unmerged, "a.txt") {
		t.Errorf("a.txt was staged behind the user's back; unmerged = %q", unmerged)
	}
}

func TestCommitMsgPreviewsThePreparedMessage(t *testing.T) {
	e := newEnv(t)
	e.diverge()
	_ = tryGit(e.dir, "merge", "side")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{
		Stage: app.StageStaged, Preview: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Message != "Merge branch 'side'" {
		t.Errorf("Message = %q", got.Message)
	}
	if !got.Preview || got.Prepared != git.OpMerge {
		t.Errorf("result = %+v, want a merge preview", got)
	}
}

// preparedMessage: false must reproduce the behaviour that shipped before the
// passthrough existed, in both directions.
func TestPreparedMessageOffRestoresTheOldBehaviour(t *testing.T) {
	t.Run("merge is refused", func(t *testing.T) {
		e := newEnv(t)
		e.cfg.PreparedMessage = false
		e.diverge()
		_ = tryGit(e.dir, "merge", "side")
		e.write("a.txt", "resolved\n")
		e.git("add", "a.txt")

		_, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
		if err == nil || !strings.Contains(err.Error(), "a merge is in progress") {
			t.Fatalf("err = %v, want a merge-in-progress refusal", err)
		}
	})

	// A conflicted `merge --squash` blocks nothing, so with passthrough off the
	// conflict guard is the only thing standing between `--all` and a commit
	// full of markers. It must not be reachable from a repository config.
	t.Run("conflicts are still refused", func(t *testing.T) {
		e := newEnv(t, "feat: whatever")
		e.cfg.PreparedMessage = false
		e.diverge()
		_ = tryGit(e.dir, "merge", "--squash", "side")
		before := e.head()

		_, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageAll})
		var state *git.StateError
		if !errors.As(err, &state) {
			t.Fatalf("err = %v, want a *git.StateError", err)
		}
		if e.head() != before {
			t.Fatal("conflict markers were committed")
		}
	})

	t.Run("squash generates", func(t *testing.T) {
		e := newEnv(t, "feat: add the extra file")
		e.cfg.PreparedMessage = false
		e.diverge()
		e.git("merge", "--squash", "extra")

		got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
		if err != nil {
			t.Fatal(err)
		}
		if got.Message != "feat: add the extra file" {
			t.Errorf("Message = %q, want the generated message", got.Message)
		}
		if got.Prepared != git.OpNone {
			t.Errorf("Prepared = %q, want empty", got.Prepared)
		}
	})
}

func TestBodyLinesWithHashSurviveCleanup(t *testing.T) {
	e := newEnv(t, "feat: add the config\n\nWhy it exists.\n# this line starts with a hash\n\nRefs: CUS-1")
	e.write("a.txt", "one\n")
	e.git("add", ".")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Message, "# this line starts with a hash") {
		t.Errorf("--cleanup dropped the hash line:\n%s", got.Message)
	}
	if !strings.Contains(got.Message, "Refs: CUS-1") {
		t.Errorf("the footer did not survive:\n%s", got.Message)
	}
}

func TestCommitMsgHookRewriteIsReported(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.write(".git/hooks/commit-msg", "#!/bin/sh\necho 'chore: rewritten by the hook' > \"$1\"\n")
	chmodExec(t, filepath.Join(e.dir, ".git", "hooks", "commit-msg"))

	e.write("a.txt", "one\n")
	e.git("add", "a.txt")

	got, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if err != nil {
		t.Fatal(err)
	}
	if got.Message != "chore: rewritten by the hook" {
		t.Errorf("Message = %q, want what the hook actually wrote", got.Message)
	}
}

func TestInvariantNothingCommittedOnProviderFailure(t *testing.T) {
	e := newEnv(t)
	e.commitFile("a.txt", "one\n", "init")
	e.write("a.txt", "two\n")
	e.git("add", ".")

	before, beforeDiff := e.head(), e.stagedDiff()
	e.prov.StartErr = errors.New("claude: command not found")

	_, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	var provErr *gen.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("err = %v, want *gen.ProviderError", err)
	}
	if e.head() != before {
		t.Error("HEAD moved although the provider never answered")
	}
	if e.stagedDiff() != beforeDiff {
		t.Error("the index changed although nothing asked it to")
	}
}

func TestValidationFailureCarriesTheLastCandidate(t *testing.T) {
	e := newEnv(t, "not a commit message", "still not one", "nope")
	e.cfg.Attempts = 3
	e.write("a.txt", "one\n")
	e.git("add", ".")

	_, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged})
	var fail *gen.FailureError
	if !errors.As(err, &fail) {
		t.Fatalf("err = %v, want *gen.FailureError", err)
	}
	if fail.Last != "nope" {
		t.Errorf("Last = %q, want the final candidate", fail.Last)
	}
	if e.head() != "" {
		t.Error("a commit was created despite failing validation")
	}
}

func TestLargeDiffKeepsTheWholeFileList(t *testing.T) {
	e := newEnv(t, "feat: add many files")
	e.cfg.Diff.MaxBytes = 8000
	for i := range 200 {
		e.write(fmt.Sprintf("f%03d.txt", i), strings.Repeat(fmt.Sprintf("line %d\n", i), 40))
	}
	e.git("add", ".")

	if _, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged}); err != nil {
		t.Fatal(err)
	}
	turn := e.prov.SessionTurns(0)[0]
	for _, name := range []string{"f000.txt", "f100.txt", "f199.txt"} {
		if !strings.Contains(turn, name) {
			t.Errorf("the file list dropped %s", name)
		}
	}
	if !strings.Contains(turn, "abbreviated") {
		t.Error("the prompt does not tell the model the diff was cut")
	}
	if strings.HasSuffix(strings.TrimSpace(turn), "@@") {
		t.Error("the diff ends on a bare hunk header")
	}
}

func TestScopeVocabularyNeedsEnoughHistory(t *testing.T) {
	e := newEnv(t, "feat(api): add the file")
	for i := range 30 {
		scope := "api"
		if i%3 == 0 {
			scope = "cli"
		}
		e.commitFile("a.txt", fmt.Sprintf("v%d\n", i), fmt.Sprintf("feat(%s): change %d", scope, i))
	}
	e.write("b.txt", "new\n")
	e.git("add", ".")

	if _, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged}); err != nil {
		t.Fatal(err)
	}
	system := systemPromptOf(t, e.prov)
	if !strings.Contains(system, "api") || !strings.Contains(system, "cli") {
		t.Errorf("the mined scopes never reached the prompt:\n%s", system)
	}

	thin := newEnv(t, "feat: add the file")
	thin.cfg.ProtectedBranches = nil
	for i := range 3 {
		thin.commitFile("a.txt", fmt.Sprintf("v%d\n", i), fmt.Sprintf("feat(api): change %d", i))
	}
	thin.write("b.txt", "new\n")
	thin.git("add", ".")

	if _, err := thin.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(systemPromptOf(t, thin.prov), "already used in this repository") {
		t.Error("a two-example vocabulary was offered to the model anyway")
	}
}

func TestConfirmIsIgnoredWhereNobodyCanAnswer(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.Confirm = true
	e.write("a.txt", "one\n")
	e.git("add", ".")

	a := e.app()

	if _, err := a.Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged}); err != nil {
		t.Fatalf("confirm:true blocked the agent path: %v", err)
	}
}

func TestConfirmDeclinedCommitsNothing(t *testing.T) {
	e := newEnv(t, "feat: add the file")
	e.cfg.Confirm = true
	e.write("a.txt", "one\n")
	e.git("add", ".")

	_, err := e.answering(scripted{confirm: false}).Commit(
		context.Background(), app.CommitRequest{Stage: app.StageStaged})
	if !errors.Is(err, app.ErrCanceled) {
		t.Fatalf("err = %v, want ErrCanceled", err)
	}
	if e.head() != "" {
		t.Error("declining still created a commit")
	}
}

func systemPromptOf(t *testing.T, p *mock.Provider) string {
	t.Helper()
	if len(p.Systems) == 0 {
		t.Fatal("the provider was never started")
	}
	return p.Systems[0]
}

func TestStageModeFor(t *testing.T) {
	tests := []struct {
		all, tracked bool
		want         app.StageMode
	}{
		{want: app.StageStaged},
		{all: true, want: app.StageAll},
		{tracked: true, want: app.StageTracked},
		{all: true, tracked: true, want: app.StageAll},
	}
	for _, tt := range tests {
		if got := app.StageModeFor(tt.all, tt.tracked); got != tt.want {
			t.Errorf("StageModeFor(%v, %v) = %q, want %q", tt.all, tt.tracked, got, tt.want)
		}
	}
}

func TestParseStageMode(t *testing.T) {
	tests := map[string]app.StageMode{
		"":         app.StageStaged,
		"staged":   app.StageStaged,
		"all":      app.StageAll,
		"tracked":  app.StageTracked,
		"nonsense": app.StageStaged,
	}
	for in, want := range tests {
		if got := app.ParseStageMode(in); got != want {
			t.Errorf("ParseStageMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepoConfigCannotSendAPromptFromOutsideTheRepo(t *testing.T) {
	e := newEnv(t, "feat: add the greeting file")
	e.commitFile("a.txt", "one\n", "init")
	e.write("b.txt", "two\n")
	e.git("add", "b.txt")

	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("## System\nleak\n\n## User\nleak"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.repoConfig(`{"presets":{"conventional":{"commit":{"prompt":"` + outside + `"}}}}`)

	_, err := app.New(e.repo, e.cfg, e.prov, ui.Noop{}, ui.Noop{})
	if err == nil {
		t.Fatal("a repository config pointed the commit prompt outside the repository")
	}
	if !strings.Contains(err.Error(), "outside the repository") {
		t.Errorf("err = %v, want the path rejected rather than the file read", err)
	}
	if e.prov.Sessions != 0 {
		t.Errorf("sessions = %d, want the provider left uncontacted", e.prov.Sessions)
	}
}
