package app

import (
	"regexp"
	"strings"

	"github.com/Conte777/autogit/internal/preset"
	"github.com/Conte777/autogit/internal/validate"
)

// genericTicket is the fallback grammar for a preset that declares no ticket
// pattern of its own.
var genericTicket = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+$`)

// ParseBranchArgs splits `branch` arguments into a ticket prefix and free text.
// The leading argument becomes the ticket only when the preset would accept it
// as one, so a description that merely looks like a ticket stays a description.
func ParseBranchArgs(args []string, format preset.BranchFormat) BranchRequest {
	if len(args) > 0 && isTicket(args[0], format.TicketPattern) {
		return BranchRequest{
			Ticket:      strings.ToUpper(args[0]),
			Description: strings.Join(args[1:], " "),
		}
	}
	return BranchRequest{Description: strings.Join(args, " ")}
}

func isTicket(arg, pattern string) bool {
	if pattern == "" {
		return genericTicket.MatchString(arg)
	}
	return validate.ExtractTicket(arg, pattern) == strings.ToUpper(arg)
}

// StageModeFor maps the CLI's mutually exclusive flags onto a StageMode.
func StageModeFor(all, tracked bool) StageMode {
	switch {
	case all:
		return StageAll
	case tracked:
		return StageTracked
	default:
		return StageStaged
	}
}

// ParseStageMode maps the MCP tool's string argument onto a StageMode. Anything
// unrecognised means the default: stage nothing.
func ParseStageMode(s string) StageMode {
	switch StageMode(s) {
	case StageAll:
		return StageAll
	case StageTracked:
		return StageTracked
	default:
		return StageStaged
	}
}
