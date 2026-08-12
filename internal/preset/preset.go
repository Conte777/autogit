// Package preset holds the named formats: prompt files plus the rules that
// check what comes back. Two are built in; the config file can override any
// field of either, or add its own.
package preset

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/Conte777/autogit/assets"
	"github.com/Conte777/autogit/internal/prompt"
	"github.com/Conte777/autogit/internal/validate"
)

// Preset is one named format, in the shape the config file uses.
type Preset struct {
	Commit CommitFormat `json:"commit"`
	Branch BranchFormat `json:"branch"`
}

// CommitFormat describes both the commit prompt and its checks.
type CommitFormat struct {
	Prompt           string      `json:"prompt,omitempty" jsonschema:"path to a prompt file; relative paths resolve against the config file that declares them"`
	Types            []string    `json:"types,omitempty" jsonschema:"allowed subject types"`
	TicketPattern    string      `json:"ticketPattern,omitempty" jsonschema:"regexp matching a ticket id in the branch name"`
	MaxSubject       int         `json:"maxSubject,omitempty" jsonschema:"subject length limit in characters"`
	LowercaseDesc    bool        `json:"lowercaseDesc"`
	NoTrailingPeriod bool        `json:"noTrailingPeriod"`
	MaxBodyLine      int         `json:"maxBodyLine,omitempty" jsonschema:"body wrap width; 0 means no limit"`
	Footers          bool        `json:"footers"`
	Body             BodyPolicy  `json:"body"`
	Scope            ScopePolicy `json:"scope"`
}

// BodyPolicy decides whether the model is asked for a body at all. On a
// one-line fix a body is noise, so `auto` gates it on the size of the diff.
type BodyPolicy struct {
	Mode     string `json:"mode" jsonschema:"off, auto or always"`
	MinFiles int    `json:"minFiles,omitempty" jsonschema:"auto: ask for a body from this many changed files"`
	MinLines int    `json:"minLines,omitempty" jsonschema:"auto: ask for a body from this many changed lines"`
}

// ScopePolicy decides what the model may put in the scope slot.
type ScopePolicy struct {
	Mode            string   `json:"mode" jsonschema:"off, suggest or whitelist"`
	Values          []string `json:"values,omitempty" jsonschema:"whitelist: the allowed scopes"`
	Top             int      `json:"top,omitempty" jsonschema:"suggest: how many scopes to mine from history"`
	MinConventional int      `json:"minConventional,omitempty" jsonschema:"suggest: skip the vocabulary below this many conventional commits in history"`
	HistoryDepth    int      `json:"historyDepth,omitempty" jsonschema:"suggest: how many commits to read"`
}

// BranchFormat describes the branch prompt and the name that comes out of it.
type BranchFormat struct {
	Prompt        string   `json:"prompt,omitempty"`
	Types         []string `json:"types,omitempty"`
	TicketPattern string   `json:"ticketPattern,omitempty"`
	MaxWords      int      `json:"maxWords,omitempty"`
	MaxSlugLen    int      `json:"maxSlugLen,omitempty"`
	Name          string   `json:"name,omitempty" jsonschema:"branch name template over .Prefix, .Type, .Ticket and .Slug"`
}

// Names lists the built-in presets.
func Names() []string { return slices.Sorted(maps.Keys(builtin)) }

var builtin = map[string]Preset{
	"conventional": {
		Commit: CommitFormat{
			Types: []string{
				"feat", "fix", "docs", "style", "refactor",
				"perf", "test", "build", "ci", "chore", "revert",
			},
			TicketPattern:    `[A-Z][A-Z0-9]+-[0-9]+`,
			MaxSubject:       72,
			LowercaseDesc:    true,
			NoTrailingPeriod: true,
			MaxBodyLine:      100,
			Footers:          true,
			Body:             BodyPolicy{Mode: "auto", MinFiles: 3, MinLines: 40},
			Scope:            ScopePolicy{Mode: "suggest", Top: 20, MinConventional: 10, HistoryDepth: 500},
		},
		Branch: BranchFormat{
			Types:         []string{"feat", "fix"},
			TicketPattern: `[A-Z][A-Z0-9]+-[0-9]+`,
			MaxWords:      4,
			MaxSlugLen:    40,
			Name:          "{{.Prefix}}/{{.Slug}}",
		},
	},
	"ticket": {
		Commit: CommitFormat{
			Types:            []string{"feat", "fix"},
			TicketPattern:    `CUS-[0-9]+`,
			MaxSubject:       50,
			LowercaseDesc:    true,
			NoTrailingPeriod: true,
			Footers:          false,
			Body:             BodyPolicy{Mode: "off"},
			Scope:            ScopePolicy{Mode: "off"},
		},
		Branch: BranchFormat{
			Types:         []string{"feat", "fix"},
			TicketPattern: `CUS-[0-9]+`,
			MaxWords:      4,
			MaxSlugLen:    40,
			Name:          "{{.Prefix}}/{{.Slug}}",
		},
	},
}

// Builtin returns a copy of a built-in preset, ready to be overridden.
func Builtin(name string) (Preset, bool) {
	p, ok := builtin[name]
	if !ok {
		return Preset{}, false
	}
	p.Commit.Types = slices.Clone(p.Commit.Types)
	p.Commit.Scope.Values = slices.Clone(p.Commit.Scope.Values)
	p.Branch.Types = slices.Clone(p.Branch.Types)
	return p, true
}

// EmbeddedPrompt returns the built-in prompt text for an operation.
func EmbeddedPrompt(preset, op string) (string, error) {
	data, err := assets.Presets.ReadFile(path.Join("presets", preset, op+".md"))
	if err != nil {
		return "", fmt.Errorf("preset %q has no built-in %s prompt", preset, op)
	}
	return string(data), nil
}

// CommitPrompt loads the commit prompt, from disk when the preset points at a
// file and from the embedded assets otherwise.
func (p Preset) CommitPrompt(name string) (*prompt.Prompt, error) {
	if p.Commit.Prompt != "" {
		return prompt.Load(p.Commit.Prompt, prompt.CommitData{})
	}
	src, err := EmbeddedPrompt(name, "commit")
	if err != nil {
		return nil, err
	}
	return prompt.Parse(name+"/commit.md", src, prompt.CommitData{})
}

// BranchPrompt loads the branch prompt.
func (p Preset) BranchPrompt(name string) (*prompt.Prompt, error) {
	if p.Branch.Prompt != "" {
		return prompt.Load(p.Branch.Prompt, prompt.BranchData{})
	}
	src, err := EmbeddedPrompt(name, "branch")
	if err != nil {
		return nil, err
	}
	return prompt.Parse(name+"/branch.md", src, prompt.BranchData{})
}

// CommitRules turns the format into the checker gen.Generate will use.
// branchSlug and scopes come from the repository, so they are arguments rather
// than preset fields.
func (f CommitFormat) CommitRules(branchSlug string, scopes []string) validate.CommitRules {
	rules := validate.CommitRules{
		Types:            f.Types,
		TicketPattern:    f.TicketPattern,
		MaxSubject:       f.MaxSubject,
		LowercaseDesc:    f.LowercaseDesc,
		NoTrailingPeriod: f.NoTrailingPeriod,
		AllowBody:        f.Body.Mode != "off",
		AllowFooters:     f.Footers,
		MaxBodyLine:      f.MaxBodyLine,
		BranchSlug:       branchSlug,
		ScopeMode:        validate.ScopeMode(f.Scope.Mode),
		Scopes:           f.Scope.Values,
	}
	if rules.ScopeMode == validate.ScopeWhitelist && len(rules.Scopes) == 0 {
		rules.Scopes = scopes
	}
	return rules
}

// WantBody decides whether the prompt should ask for a body.
func (f CommitFormat) WantBody(files, lines int) bool {
	switch f.Body.Mode {
	case "always":
		return true
	case "auto":
		return files >= f.Body.MinFiles || lines >= f.Body.MinLines
	default:
		return false
	}
}

// Validate rejects a preset that would produce nonsense at runtime.
func (p Preset) Validate() error {
	switch p.Commit.Body.Mode {
	case "off", "auto", "always":
	default:
		return fmt.Errorf("commit.body.mode must be off, auto or always (got %q)", p.Commit.Body.Mode)
	}
	switch p.Commit.Scope.Mode {
	case string(validate.ScopeOff), string(validate.ScopeSuggest), string(validate.ScopeWhitelist):
	default:
		return fmt.Errorf("commit.scope.mode must be off, suggest or whitelist (got %q)", p.Commit.Scope.Mode)
	}
	if p.Branch.Name != "" && !strings.Contains(p.Branch.Name, "{{") {
		return fmt.Errorf("branch.name must be a template, e.g. {{.Prefix}}/{{.Slug}}")
	}
	return nil
}
