package app

import (
	"fmt"
	"strings"

	"github.com/Conte777/autogit/internal/git"
)

// SummaryStyle is who reads the line. The wording differs by audience, not by
// surface: a terminal prints one line under a message the user just approved,
// while the hook and the MCP tool answer a model that never sees the
// repository and needs the whole message plus a reason it was not generated.
type SummaryStyle int

const (
	// SummaryHuman is a single terminal line: the subject and nothing else.
	SummaryHuman SummaryStyle = iota
	// SummaryAgent carries the full message and spells out every caveat.
	SummaryAgent
)

// Summary renders the result as the text a surface shows. A preview carries
// the message alone, so `autogit commit-msg > file` stays usable; SummaryHuman
// leaves the prepared-message note to the caller, which writes it to stderr.
func (r CommitResult) Summary(style SummaryStyle) string {
	if r.Preview {
		if style == SummaryHuman {
			return r.Message
		}
		return r.Message + r.preparedNote(style)
	}
	if style == SummaryHuman {
		return fmt.Sprintf("committed %s: %s%s", r.ShortHash, firstLine(r.Message), r.preparedNote(style))
	}
	return fmt.Sprintf("committed %s\n\n%s%s", r.ShortHash, r.Message, r.preparedNote(style))
}

// preparedNote labels a message git wrote itself, so a reader does not take an
// unvalidated `Merge branch 'x'` for a broken generation. The preview wording
// must not claim a commit that has not happened.
func (r CommitResult) preparedNote(style SummaryStyle) string {
	switch {
	case r.Prepared == git.OpNone:
		return ""
	case r.Preview:
		return fmt.Sprintf("\n\n(git's own %s message; it would be used verbatim, not generated)", r.Prepared)
	case style == SummaryHuman:
		return fmt.Sprintf(" (git's own %s message)", r.Prepared)
	default:
		return fmt.Sprintf("\n\n(git's own %s message, committed verbatim: no message was generated)", r.Prepared)
	}
}

// Summary renders the result as the text a surface shows. Every surface says
// the same thing, so the style plays no part.
func (r BranchResult) Summary() string {
	return "switched to new branch " + r.Name
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
