// Package hook implements the Claude Code UserPromptSubmit hook. It fires on
// every prompt the user types, so a non-matching prompt must cost nothing:
// no config read, no repository probe, just a pattern check and exit.
package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// Budget is autogit's own deadline. Claude Code kills the hook at 120s and a
// killed hook shows the user nothing at all, so we stop first and say why.
const Budget = 110 * time.Second

// ActiveEnv guards against the hook re-entering itself through a provider that
// shells out to `claude`.
const ActiveEnv = "AUTOGIT_ACTIVE"

// Kind is the operation a prompt asks for.
type Kind string

const (
	KindCommit    Kind = "commit"
	KindCommitMsg Kind = "commit-msg"
	KindBranch    Kind = "branch"
)

// Command is a parsed slash command.
type Command struct {
	Kind        Kind
	All         bool
	Tracked     bool
	Force       bool
	DryRun      bool
	Ticket      string
	Description string
}

// Runner performs the command and returns the line to show the user.
type Runner func(ctx context.Context, cmd Command) (string, error)

// decision is the JSON Claude Code expects back.
type decision struct {
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	SystemMessage string `json:"systemMessage,omitempty"`
}

// commit-msg must be tried before commit: it is a prefix of it.
var trigger = regexp.MustCompile(`^\s*/(commit-msg|commit|branch)(\s|$)`)

var ticketArg = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+$`)

// Run reads the hook payload, and blocks the prompt when it matched. Anything
// that did not match leaves the prompt alone and exits silently.
func Run(ctx context.Context, in io.Reader, out io.Writer, env func(string) (string, bool), run Runner) error {
	if v, ok := env(ActiveEnv); ok && v == "1" {
		return nil
	}

	var payload struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(in).Decode(&payload); err != nil {
		return nil
	}
	cmd, ok := Parse(payload.Prompt)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, Budget)
	defer cancel()

	message, err := run(ctx, cmd)
	if err != nil {
		message = fmt.Sprintf("autogit %s failed: %v", cmd.Kind, err)
	}
	return block(out, message)
}

func block(out io.Writer, reason string) error {
	first := reason
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	return json.NewEncoder(out).Encode(decision{
		Decision:      "block",
		Reason:        reason,
		SystemMessage: first,
	})
}

// Parse understands both the historical bare-word grammar (`/commit all force`)
// and flags (`/commit --all --force`).
func Parse(prompt string) (Command, bool) {
	m := trigger.FindStringSubmatch(prompt)
	if m == nil {
		return Command{}, false
	}
	kind := Kind(m[1])
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prompt), "/"+string(kind)))

	cmd := Command{Kind: kind}
	if kind == KindBranch {
		parseBranchArgs(&cmd, strings.Fields(rest))
		return cmd, true
	}

	for _, word := range strings.Fields(rest) {
		switch strings.ToLower(strings.TrimPrefix(word, "--")) {
		case "all", "a":
			cmd.All = true
		case "tracked", "u":
			cmd.Tracked = true
		case "force", "f":
			cmd.Force = true
		case "dry-run", "dryrun":
			cmd.DryRun = true
		}
	}
	return cmd, true
}

func parseBranchArgs(cmd *Command, fields []string) {
	if len(fields) > 0 && ticketArg.MatchString(fields[0]) {
		cmd.Ticket = strings.ToUpper(fields[0])
		fields = fields[1:]
	}
	cmd.Description = strings.Join(fields, " ")
}
