package git

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// DiffOptions shapes both the git invocation and the truncation ladder.
type DiffOptions struct {
	MaxBytes         int
	Context          int
	IgnoreSubmodules bool
	ExcludePathspecs []string
}

// Diff is what gets shown to the model.
type Diff struct {
	Files     []string // always complete, never truncated
	Text      string
	Truncated bool
}

// Empty reports whether there is anything to describe.
func (d Diff) Empty() bool { return len(d.Files) == 0 }

// StagedDiff describes the index against HEAD.
func (r *Repo) StagedDiff(ctx context.Context, opts DiffOptions) (Diff, error) {
	return r.diff(ctx, opts, []string{"diff", "--cached"})
}

// WorktreeDiff describes tracked changes against HEAD, falling back to the
// empty-tree sentinel in a repository that has no commits yet.
func (r *Repo) WorktreeDiff(ctx context.Context, opts DiffOptions) (Diff, error) {
	base := "HEAD"
	if !r.HasCommits(ctx) {
		base = EmptyTree
	}
	return r.diff(ctx, opts, []string{"diff", base})
}

const diffReadFactor = 8

func (r *Repo) diff(ctx context.Context, opts DiffOptions, base []string) (Diff, error) {
	files, err := r.diffFiles(ctx, base)
	if err != nil || len(files) == 0 {
		return Diff{Files: files}, err
	}

	limit := opts.MaxBytes * diffReadFactor
	body, over, err := r.runBounded(ctx, defaultTimeout, limit, "", r.diffArgs(opts, base, false)...)
	if err != nil {
		return Diff{}, err
	}
	if !over && (opts.MaxBytes <= 0 || len(body) <= opts.MaxBytes) {
		return Diff{Files: files, Text: body}, nil
	}
	if over {
		body = ""
	}

	stat, _, err := r.runBounded(ctx, defaultTimeout, limit, "", r.diffArgs(opts, base, true)...)
	if err != nil {
		return Diff{}, err
	}
	return Diff{Files: files, Text: shrink(body, stat, opts.MaxBytes), Truncated: true}, nil
}

func (r *Repo) diffFiles(ctx context.Context, base []string) ([]string, error) {
	// No exclude pathspecs here on purpose: the truncation ladder may drop a
	// file's body, but the model must always see the full list of what changed.
	args := append(append([]string{}, base...), "--name-only", "-z")
	out, err := r.run(ctx, defaultTimeout, "", args...)
	if err != nil {
		return nil, err
	}
	return splitZ(out), nil
}

func (r *Repo) diffArgs(opts DiffOptions, base []string, statOnly bool) []string {
	args := append([]string{}, base...)
	args = append(args, "--no-ext-diff", "--no-color")
	if opts.IgnoreSubmodules {
		args = append(args, "--ignore-submodules=all")
	}
	if statOnly {
		args = append(args, "--stat=200")
	} else {
		ctxLines := opts.Context
		if ctxLines <= 0 {
			ctxLines = 3
		}
		args = append(args, fmt.Sprintf("-U%d", ctxLines))
	}
	if len(opts.ExcludePathspecs) > 0 {
		args = append(args, "--")
		args = append(args, opts.ExcludePathspecs...)
	}
	return args
}

const truncNote = "\n[diff truncated: bodies of the largest files were dropped; the file list above is complete]\n"

// shrink drops whole per-file sections, largest first, until the diff fits.
// Cutting raw bytes instead would end mid-hunk and the model would describe a
// removal that is really just the cut.
func shrink(body, stat string, maxBytes int) string {
	head := stat + truncNote
	if len(head) >= maxBytes {
		return cutLines(stat, maxBytes) + truncNote
	}

	sections := splitDiffSections(body)
	budget := maxBytes - len(head)

	order := make([]int, len(sections))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return len(sections[order[a]]) < len(sections[order[b]])
	})

	keep := make([]bool, len(sections))
	for _, i := range order {
		if len(sections[i]) > budget {
			break
		}
		budget -= len(sections[i])
		keep[i] = true
	}

	var b strings.Builder
	b.WriteString(head)
	for i, s := range sections {
		if keep[i] {
			b.WriteString(s)
		}
	}
	return b.String()
}

// splitDiffSections cuts a unified diff at its per-file boundaries. A `diff
// --git ` at column 0 is unambiguous: every line inside a hunk is prefixed by
// a space, '+', '-' or '\'.
func splitDiffSections(body string) []string {
	const marker = "diff --git "
	var sections []string
	start := -1
	for offset := 0; offset < len(body); {
		end := strings.IndexByte(body[offset:], '\n')
		lineEnd := len(body)
		if end >= 0 {
			lineEnd = offset + end + 1
		}
		if strings.HasPrefix(body[offset:], marker) {
			if start >= 0 {
				sections = append(sections, body[start:offset])
			}
			start = offset
		}
		offset = lineEnd
	}
	if start >= 0 {
		sections = append(sections, body[start:])
	} else if body != "" {
		sections = append(sections, body)
	}
	return sections
}

func cutLines(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := s[:maxBytes]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		return cut[:i+1]
	}
	return cut
}
