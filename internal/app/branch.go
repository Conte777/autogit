package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/template"

	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/preset"
	"github.com/Conte777/autogit/internal/prompt"
	"github.com/Conte777/autogit/internal/validate"
)

// BranchRequest is one `autogit branch` run.
type BranchRequest struct {
	// Ticket, when set, becomes the branch prefix and skips the type question.
	Ticket string
	// Description is free text. Empty means: work it out from the diff.
	Description string
}

// ParseBranchArgs splits `branch` arguments into a ticket prefix and free text.
// The leading argument becomes the ticket only when the preset would accept it
// as one, so a description that merely looks like a ticket stays a description.
func (a *App) ParseBranchArgs(args []string) BranchRequest {
	return parseBranchArgs(args, a.preset.Branch)
}

func parseBranchArgs(args []string, format preset.BranchFormat) BranchRequest {
	if len(args) > 0 && ticketMatches(args[0], format.TicketPattern) {
		return BranchRequest{
			Ticket:      strings.ToUpper(args[0]),
			Description: strings.Join(args[1:], " "),
		}
	}
	return BranchRequest{Description: strings.Join(args, " ")}
}

// ticketMatches reports whether arg is exactly what pattern describes. A preset
// that declares no pattern describes nothing: there a ticket can only arrive
// named, never guessed out of free text.
func ticketMatches(arg, pattern string) bool {
	return arg != "" && pattern != "" && validate.ExtractTicket(arg, pattern) == strings.ToUpper(arg)
}

// BranchResult is the branch that was created.
type BranchResult struct {
	Name     string
	Attempts int
}

// ErrNoBranchInput means there is neither a description nor a diff to describe.
var ErrNoBranchInput = errors.New("no description and no changes to describe")

// Branch creates and switches to <prefix>/<slug>.
func (a *App) Branch(ctx context.Context, req BranchRequest) (BranchResult, error) {
	st, stateErr := a.repo.State(ctx)
	if stateErr != nil {
		return BranchResult{}, stateErr
	}
	if blocked := st.Blocked(); blocked != nil {
		return BranchResult{}, blocked
	}

	format := a.preset.Branch
	ticket := strings.ToUpper(strings.TrimSpace(req.Ticket))
	if ticket != "" && format.TicketPattern != "" && !ticketMatches(ticket, format.TicketPattern) {
		return BranchResult{}, fmt.Errorf("%q does not look like a ticket id for this preset", req.Ticket)
	}

	desc := strings.TrimSpace(req.Description)
	prefix, typ, slug := ticket, "", ""
	attempts := 0

	switch {
	case desc != "" && ticket != "":
		slug = validate.Slugify(desc, format.MaxWords)

	case desc != "":
		stop := a.progress.Start(branchProgressLabel)
		answer, err := a.askBranch(ctx, prompt.BranchData{
			Description: desc,
			Types:       format.Types,
			MaxWords:    format.MaxWords,
			NeedType:    true,
		})
		stop()
		if err != nil {
			return BranchResult{}, err
		}
		attempts, typ, prefix = answer.Attempts, answer.Type, answer.Type
		slug = validate.Slugify(desc, format.MaxWords)

	default:
		answer, err := a.branchFromDiff(ctx, format, ticket)
		if err != nil {
			return BranchResult{}, err
		}
		attempts, slug = answer.Attempts, answer.Slug
		if ticket == "" {
			typ, prefix = answer.Type, answer.Type
		}
	}

	if slug == "" {
		return BranchResult{}, ErrNoBranchInput
	}
	if prefix == "" && len(format.Types) > 0 {
		typ, prefix = format.Types[0], format.Types[0]
	}

	name, err := renderBranchName(format.Name, prefix, typ, ticket, slug)
	if err != nil {
		return BranchResult{}, err
	}
	if a.repo.BranchExists(ctx, name) {
		return BranchResult{}, fmt.Errorf("branch %q already exists", name)
	}
	if err := a.repo.CreateBranch(ctx, name); err != nil {
		return BranchResult{}, err
	}
	return BranchResult{Name: name, Attempts: attempts}, nil
}

const branchProgressLabel = "Generating branch name…"

// branchFromDiff covers the slow pair with one report: reading the worktree
// diff is as much of the wait as the generation that follows it.
func (a *App) branchFromDiff(
	ctx context.Context,
	format preset.BranchFormat,
	ticket string,
) (branchAnswer, error) {
	stop := a.progress.Start(branchProgressLabel)
	defer stop()

	diff, err := a.repo.WorktreeDiff(ctx, a.diffOptions())
	if err != nil {
		return branchAnswer{}, err
	}
	if diff.Empty() {
		return branchAnswer{}, ErrNoBranchInput
	}
	return a.askBranch(ctx, prompt.BranchData{
		Files:         diff.Files,
		Diff:          diff.Text,
		DiffTruncated: diff.Truncated,
		Types:         format.Types,
		MaxWords:      format.MaxWords,
		NeedType:      ticket == "",
	})
}

type branchAnswer struct {
	Type     string
	Slug     string
	Attempts int
}

func (a *App) askBranch(ctx context.Context, data prompt.BranchData) (branchAnswer, error) {
	tmpl, err := a.preset.BranchPrompt()
	if err != nil {
		return branchAnswer{}, err
	}
	system, user, err := tmpl.Render(data)
	if err != nil {
		return branchAnswer{}, err
	}

	v := branchValidator{
		needType: data.NeedType,
		types:    data.Types,
		slug:     validate.SlugRules{MaxLen: a.preset.Branch.MaxSlugLen},
	}
	result, err := a.generate(ctx, gen.Request{System: system, Prompt: user, Validator: v})
	if err != nil {
		return branchAnswer{}, err
	}
	typ, slug := v.split(result.Value)
	return branchAnswer{Type: typ, Slug: slug, Attempts: result.Attempts}, nil
}

// branchValidator accepts `<type> <slug>` or a bare `<slug>`, depending on
// whether the branch already has a ticket to use as its prefix.
type branchValidator struct {
	needType bool
	types    []string
	slug     validate.SlugRules
}

func (v branchValidator) Check(raw string) (string, []string) {
	fields := strings.Fields(validate.Sanitize(raw))
	if len(fields) == 0 {
		return "", []string{"model returned empty output"}
	}

	if !v.needType {
		value, problems := v.slug.Check(fields[len(fields)-1])
		return value, problems
	}

	typ := strings.ToLower(strings.TrimRight(fields[0], ":"))
	if !slices.Contains(v.types, typ) {
		return strings.Join(fields, " "),
			[]string{fmt.Sprintf("first word must be one of: %s", strings.Join(v.types, ", "))}
	}
	if len(fields) < 2 {
		return typ, []string{"expected `<type> <slug>`, e.g. `feat add-user-auth`"}
	}
	slug, problems := v.slug.Check(strings.Join(fields[1:], "-"))
	return typ + " " + slug, problems
}

func (v branchValidator) split(value string) (typ, slug string) {
	if !v.needType {
		return "", value
	}
	typ, slug, _ = strings.Cut(value, " ")
	return typ, slug
}

func renderBranchName(nameTmpl, prefix, typ, ticket, slug string) (string, error) {
	if nameTmpl == "" {
		nameTmpl = "{{.Prefix}}/{{.Slug}}"
	}
	t, err := template.New("branch-name").Option("missingkey=error").Parse(nameTmpl)
	if err != nil {
		return "", fmt.Errorf("branch.name: %w", err)
	}
	var b bytes.Buffer
	err = t.Execute(&b, struct{ Prefix, Type, Ticket, Slug string }{
		Prefix: prefix, Type: typ, Ticket: ticket, Slug: slug,
	})
	if err != nil {
		return "", fmt.Errorf("branch.name: %w", err)
	}
	return strings.TrimSpace(b.String()), nil
}
