package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/prompt"
	"github.com/Conte777/autogit/internal/ui"
	"github.com/Conte777/autogit/internal/validate"
)

// StageMode says what to add to the index before generating.
type StageMode string

const (
	StageStaged  StageMode = "staged"
	StageAll     StageMode = "all"
	StageTracked StageMode = "tracked"
)

// CommitRequest is one commit or commit-msg run.
type CommitRequest struct {
	Stage StageMode
	// Force permits a protected branch. Only a human can set it.
	Force bool
	// Preview generates the message and stops — this is `commit-msg`, which is
	// the same code path so that the preview cannot differ from the commit.
	Preview bool
	NoInput bool
}

// CommitResult is what happened.
type CommitResult struct {
	Message   string
	Hash      string
	ShortHash string
	Preview   bool
	Attempts  int
	Branch    string
}

// Commit stages, generates, checks and commits.
//
// The invariant on every error path: nothing is committed, and the index is
// touched only where the caller explicitly asked for it via Stage.
func (a *App) Commit(ctx context.Context, req CommitRequest) (CommitResult, error) {
	if err := a.Repo.CheckState(ctx); err != nil {
		return CommitResult{}, err
	}
	branch, err := a.Repo.Current(ctx)
	if err != nil {
		return CommitResult{}, err
	}

	if !req.Preview {
		if protErr := a.checkProtected(branch, req); protErr != nil {
			return CommitResult{}, protErr
		}
	}
	if stageErr := a.stage(ctx, req); stageErr != nil {
		return CommitResult{}, stageErr
	}

	diff, err := a.Repo.StagedDiff(ctx, a.diffOptions())
	if err != nil {
		return CommitResult{}, err
	}
	if diff.Empty() {
		return CommitResult{}, ErrNothingToCommit
	}

	result, err := a.generateMessage(ctx, branch, diff)
	if err != nil {
		return CommitResult{}, err
	}
	out := CommitResult{
		Message:  result.Value,
		Attempts: result.Attempts,
		Branch:   branch.Name,
		Preview:  req.Preview,
	}
	if req.Preview {
		return out, nil
	}

	if confirmErr := a.confirmCommit(req, result.Value); confirmErr != nil {
		return CommitResult{}, confirmErr
	}

	landed, err := a.Repo.Commit(ctx, result.Value)
	if err != nil {
		return CommitResult{}, err
	}
	out.Message, out.Hash, out.ShortHash = landed.Message, landed.Hash, landed.ShortHash
	return out, nil
}

func (a *App) checkProtected(branch git.Branch, req CommitRequest) error {
	if branch.Detached || req.Force || !validate.IsProtected(branch.Name, a.Config.ProtectedBranches) {
		return nil
	}
	if a.interactive() && !req.NoInput {
		ok, err := a.Prompt.Confirm(
			fmt.Sprintf("Branch %q is protected. Commit anyway?", branch.Name), false)
		if err != nil {
			return err
		}
		if !ok {
			return ErrCanceled
		}
		return nil
	}
	return &ProtectedBranchError{
		Branch: branch.Name,
		Hint:   "re-run with --force if that is what you meant",
	}
}

// stage fills the index, asking what to take when it is empty and the tree is
// not. Outside a terminal the question becomes an error carrying the command
// the user should have typed.
func (a *App) stage(ctx context.Context, req CommitRequest) error {
	switch req.Stage {
	case StageAll:
		if err := a.Repo.StageAll(ctx); err != nil {
			return err
		}
	case StageTracked:
		if err := a.Repo.StageTracked(ctx); err != nil {
			return err
		}
	}

	// Re-checked here rather than only on entry: an MCP request can be replayed
	// after a partial `git add`, and a second commit must not fall out of that.
	staged, err := a.Repo.HasStaged(ctx)
	if err != nil {
		return err
	}
	if staged {
		return nil
	}

	status, err := a.Repo.Status(ctx)
	if err != nil {
		return err
	}
	if !status.ModifiedTracked && !status.Untracked {
		return ErrNothingToCommit
	}

	if !a.interactive() || req.NoInput {
		return fmt.Errorf("%w: the working tree has changes but the index is empty; "+
			"stage them yourself, or pass --all (everything) or --tracked (tracked files only)",
			ErrNothingToCommit)
	}

	choice, err := a.Prompt.Choose("Nothing is staged, but the working tree is dirty. What should I commit?",
		[]ui.Option{
			{Key: "a", Label: "everything, including untracked files (git add -A)"},
			{Key: "t", Label: "tracked files only (git add -u)"},
			{Key: "c", Label: "cancel"},
		})
	if err != nil {
		return fmt.Errorf("%w: nothing is staged", ErrNothingToCommit)
	}
	switch choice {
	case "a":
		err = a.Repo.StageAll(ctx)
	case "t":
		err = a.Repo.StageTracked(ctx)
	default:
		return ErrCanceled
	}
	if err != nil {
		return err
	}

	staged, err = a.Repo.HasStaged(ctx)
	if err != nil {
		return err
	}
	if !staged {
		return ErrNothingToCommit
	}
	return nil
}

func (a *App) confirmCommit(req CommitRequest, message string) error {
	// `confirm` is a terminal courtesy. Honouring it on mcp/hook would let a
	// global `confirm: true` deadlock the agent path.
	if !a.Config.Confirm || req.NoInput || !a.interactive() {
		return nil
	}
	ok, err := a.Prompt.Confirm("Commit this message?\n\n"+indent(message)+"\n", true)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCanceled
	}
	return nil
}

func indent(s string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(s, "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func (a *App) generateMessage(ctx context.Context, branch git.Branch, diff git.Diff) (gen.Result, error) {
	format := a.Preset.Commit

	var ticket, branchSlug string
	if !branch.Detached {
		ticket = validate.ExtractTicket(branch.Name, format.TicketPattern)
		branchSlug = validate.BranchSlugText(branch.Name)
	}

	scopes, err := a.scopes(ctx)
	if err != nil {
		return gen.Result{}, err
	}

	data := prompt.CommitData{
		Ticket:        ticket,
		Branch:        branch.Name,
		Detached:      branch.Detached,
		Files:         diff.Files,
		Diff:          diff.Text,
		DiffTruncated: diff.Truncated,
		Types:         format.Types,
		MaxSubject:    format.MaxSubject,
		Scopes:        scopes,
		ScopeMode:     format.Scope.Mode,
		WantBody:      format.WantBody(len(diff.Files), countChangedLines(diff.Text)),
		AllowFooters:  format.Footers,
	}

	tmpl, err := a.Preset.CommitPrompt(a.PresetName)
	if err != nil {
		return gen.Result{}, err
	}
	system, user, err := tmpl.Render(data)
	if err != nil {
		return gen.Result{}, err
	}

	// WantBody shapes the prompt only. "a body is unnecessary here" is a
	// heuristic, and enforcing it would burn every retry on a legitimate body.
	rules := format.CommitRules(branchSlug, scopes)
	return a.generate(ctx, gen.Request{System: system, Prompt: user, Validator: rules})
}

// scopes mines the repository's own scope vocabulary. Nothing is passed to the
// model when the history is too thin to be a good example.
func (a *App) scopes(ctx context.Context) ([]string, error) {
	scope := a.Preset.Commit.Scope
	if scope.Mode == "off" || len(scope.Values) > 0 {
		return scope.Values, nil
	}
	depth := scope.HistoryDepth
	if depth <= 0 {
		depth = 500
	}
	subjects, err := a.Repo.Subjects(ctx, depth)
	if err != nil {
		return nil, err
	}
	return validate.CollectScopes(subjects, scope.Top, scope.MinConventional), nil
}

func countChangedLines(diffText string) int {
	n := 0
	for line := range strings.SplitSeq(diffText, "\n") {
		if len(line) == 0 {
			continue
		}
		if (line[0] == '+' || line[0] == '-') && !strings.HasPrefix(line, "+++") && !strings.HasPrefix(line, "---") {
			n++
		}
	}
	return n
}

func (a *App) diffOptions() git.DiffOptions {
	d := a.Config.Diff
	return git.DiffOptions{
		MaxBytes:         d.MaxBytes,
		Context:          d.Context,
		IgnoreSubmodules: d.IgnoreSubmodules,
		ExcludePathspecs: d.ExcludePathspecs,
	}
}

// IsNothingToCommit reports whether err is the empty-index case.
func IsNothingToCommit(err error) bool { return errors.Is(err, ErrNothingToCommit) }
