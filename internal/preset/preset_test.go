package preset_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/preset"
	"github.com/Conte777/autogit/internal/prompt"
	"github.com/Conte777/autogit/internal/validate"
)

func TestBuiltinPromptsCompileAndRender(t *testing.T) {
	for _, name := range preset.Names() {
		t.Run(name, func(t *testing.T) {
			p, ok := preset.Builtin(name)
			if !ok {
				t.Fatalf("Builtin(%q) not found", name)
			}
			if err := p.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
			if p.Name() != name {
				t.Fatalf("Name() = %q, want %q", p.Name(), name)
			}

			commit, err := p.CommitPrompt()
			if err != nil {
				t.Fatal(err)
			}
			branch, err := p.BranchPrompt()
			if err != nil {
				t.Fatal(err)
			}

			for _, data := range commitCases(p) {
				system, user, err := commit.Render(data)
				if err != nil {
					t.Fatalf("Render(%+v) = %v", data, err)
				}
				if system == "" || user == "" {
					t.Fatalf("Render produced an empty half: system=%q user=%q", system, user)
				}
			}
			for _, data := range branchCases(p) {
				if _, _, err := branch.Render(data); err != nil {
					t.Fatalf("Render(%+v) = %v", data, err)
				}
			}
		})
	}
}

func commitCases(p preset.Preset) []prompt.CommitData {
	base := prompt.CommitData{
		Branch:       "feat/thing",
		Files:        []string{"a.go"},
		Diff:         "diff --git a/a.go b/a.go",
		Types:        p.Commit.Types,
		MaxSubject:   p.Commit.MaxSubject,
		AllowFooters: p.Commit.Footers,
	}
	var out []prompt.CommitData
	for _, mode := range []validate.ScopeMode{validate.ScopeOff, validate.ScopeSuggest, validate.ScopeWhitelist} {
		for _, ticket := range []string{"", "CUS-42"} {
			for _, body := range []bool{false, true} {
				d := base
				d.ScopeMode, d.Ticket, d.WantBody = mode, ticket, body
				if mode != validate.ScopeOff {
					d.Scopes = []string{"api", "cli"}
				}
				out = append(out, d)
			}
		}
	}
	// The suggest mode with no history must still render.
	d := base
	d.ScopeMode = validate.ScopeSuggest
	d.DiffTruncated = true
	d.Detached = true
	return append(out, d)
}

func branchCases(p preset.Preset) []prompt.BranchData {
	base := prompt.BranchData{
		Files:    []string{"a.go"},
		Diff:     "diff",
		Types:    p.Branch.Types,
		MaxWords: p.Branch.MaxWords,
	}
	var out []prompt.BranchData
	for _, desc := range []string{"", "add user auth"} {
		for _, needType := range []bool{false, true} {
			d := base
			d.Description, d.NeedType = desc, needType
			out = append(out, d)
		}
	}
	return out
}

func TestTicketPromptMentionsTheTicketAsPrefix(t *testing.T) {
	p, _ := preset.Builtin("ticket")
	commit, err := p.CommitPrompt()
	if err != nil {
		t.Fatal(err)
	}

	system, _, err := commit.Render(prompt.CommitData{Ticket: "CUS-42", MaxSubject: 50})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(system, "`CUS-42`") {
		t.Errorf("system prompt does not pin the prefix to the ticket:\n%s", system)
	}

	system, _, err = commit.Render(prompt.CommitData{MaxSubject: 50})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(system, "CUS-") {
		t.Errorf("system prompt mentions a ticket when there is none:\n%s", system)
	}
}

func TestConventionalPromptDropsRefsWithoutTicket(t *testing.T) {
	p, _ := preset.Builtin("conventional")
	commit, _ := p.CommitPrompt()

	system, _, err := commit.Render(prompt.CommitData{AllowFooters: true, ScopeMode: validate.ScopeSuggest})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(system, "Refs:") && !strings.Contains(system, "do not") {
		t.Errorf("footer block invites an invented ticket:\n%s", system)
	}
	if !strings.Contains(system, "invent") {
		t.Errorf("system prompt does not forbid inventing a ticket:\n%s", system)
	}
}

func TestCommitRules(t *testing.T) {
	p, _ := preset.Builtin("ticket")
	rules := p.Commit.CommitRules("add thing", p.Commit.Scope.Resolve(nil))

	if rules.AllowBody {
		t.Error("ticket preset allows a body")
	}
	if _, problems := rules.Check("feat: add thing"); len(problems) == 0 {
		t.Error("the branch-slug check did not reach the rules")
	}

	c, _ := preset.Builtin("conventional")
	if !c.Commit.CommitRules("", c.Commit.Scope.Resolve(nil)).AllowBody {
		t.Error("conventional preset forbids a body")
	}
}

func TestWantBody(t *testing.T) {
	auto := preset.CommitFormat{Body: preset.BodyPolicy{Mode: "auto", MinFiles: 3, MinLines: 40}}
	if auto.WantBody(1, 5) {
		t.Error("auto asked for a body on a one-line fix")
	}
	if !auto.WantBody(4, 5) {
		t.Error("auto skipped the body on a four-file change")
	}
	if !auto.WantBody(1, 100) {
		t.Error("auto skipped the body on a 100-line change")
	}

	if (preset.CommitFormat{Body: preset.BodyPolicy{Mode: "off"}}).WantBody(50, 500) {
		t.Error("off asked for a body")
	}
	if !(preset.CommitFormat{Body: preset.BodyPolicy{Mode: "always"}}).WantBody(1, 1) {
		t.Error("always skipped the body")
	}
}

func TestValidateRejectsBadModes(t *testing.T) {
	p, _ := preset.Builtin("conventional")
	p.Commit.Scope.Mode = "sometimes"
	if err := p.Validate(); err == nil {
		t.Error("Validate accepted an unknown scope mode")
	}

	p, _ = preset.Builtin("conventional")
	p.Commit.Body.Mode = "maybe"
	if err := p.Validate(); err == nil {
		t.Error("Validate accepted an unknown body mode")
	}

	p, _ = preset.Builtin("conventional")
	p.Branch.Name = "plain-string"
	if err := p.Validate(); err == nil {
		t.Error("Validate accepted a branch name that is not a template")
	}
}

func TestBuiltinReturnsACopy(t *testing.T) {
	a, _ := preset.Builtin("ticket")
	a.Commit.Types[0] = "mutated"

	b, _ := preset.Builtin("ticket")
	if b.Commit.Types[0] == "mutated" {
		t.Error("Builtin handed out the shared slice")
	}
}

func TestScopePolicyResolve(t *testing.T) {
	mined := []string{"api", "cli"}

	tests := []struct {
		name        string
		policy      preset.ScopePolicy
		needHistory bool
		wantHint    []string
		wantAllowed []string
	}{
		{
			name:   "off keeps every scope out of the prompt and the rules",
			policy: preset.ScopePolicy{Mode: validate.ScopeOff},
		},
		{
			name:        "suggest offers history and enforces nothing",
			policy:      preset.ScopePolicy{Mode: validate.ScopeSuggest},
			needHistory: true,
			wantHint:    mined,
		},
		{
			name:     "suggest with configured values does not read history",
			policy:   preset.ScopePolicy{Mode: validate.ScopeSuggest, Values: []string{"ui"}},
			wantHint: []string{"ui"},
		},
		{
			name:        "whitelist without values enforces what history produced",
			policy:      preset.ScopePolicy{Mode: validate.ScopeWhitelist},
			needHistory: true,
			wantHint:    mined,
			wantAllowed: mined,
		},
		{
			name:        "whitelist with values enforces them",
			policy:      preset.ScopePolicy{Mode: validate.ScopeWhitelist, Values: []string{"ui"}},
			wantHint:    []string{"ui"},
			wantAllowed: []string{"ui"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.NeedsHistory(); got != tt.needHistory {
				t.Errorf("NeedsHistory() = %v, want %v", got, tt.needHistory)
			}
			var available []string
			if tt.needHistory {
				available = mined
			}
			v := tt.policy.Resolve(available)
			if v.Mode != tt.policy.Mode {
				t.Errorf("Mode = %q, want %q", v.Mode, tt.policy.Mode)
			}
			if !slices.Equal(v.Hint, tt.wantHint) {
				t.Errorf("Hint = %v, want %v", v.Hint, tt.wantHint)
			}
			if !slices.Equal(v.Allowed, tt.wantAllowed) {
				t.Errorf("Allowed = %v, want %v", v.Allowed, tt.wantAllowed)
			}
		})
	}
}
