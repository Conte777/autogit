package validate

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseCommit(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Commit
		wantErr bool
	}{
		{
			name: "subject only",
			in:   "feat: add telegram notifier",
			want: Commit{Subject: "feat: add telegram notifier", Prefix: "feat", Desc: "add telegram notifier"},
		},
		{
			name: "subject and body",
			in:   "feat: add notifier\n\nIt posts to the channel.\nSecond line.",
			want: Commit{
				Subject: "feat: add notifier", Prefix: "feat", Desc: "add notifier",
				Body: "It posts to the channel.\nSecond line.",
			},
		},
		{
			name: "subject body footers",
			in:   "feat: add notifier\n\nWhy it exists.\n\nRefs: CUS-42\nReviewed-by: Ann",
			want: Commit{
				Subject: "feat: add notifier", Prefix: "feat", Desc: "add notifier",
				Body:    "Why it exists.",
				Footers: []Footer{{Key: "Refs", Value: "CUS-42"}, {Key: "Reviewed-by", Value: "Ann"}},
			},
		},
		{
			name: "breaking bang",
			in:   "feat(api)!: drop v1 endpoints",
			want: Commit{
				Subject: "feat(api)!: drop v1 endpoints", Prefix: "feat", Scope: "api",
				Breaking: true, Desc: "drop v1 endpoints",
			},
		},
		{
			name: "breaking change footer sets flag",
			in:   "feat: rework auth\n\nBREAKING CHANGE: tokens are no longer accepted",
			want: Commit{
				Subject: "feat: rework auth", Prefix: "feat", Desc: "rework auth", Breaking: true,
				Footers: []Footer{{Key: "BREAKING CHANGE", Value: "tokens are no longer accepted"}},
			},
		},
		{
			name: "scope with slashes and dots",
			in:   "fix(api/v2.1): handle empty body",
			want: Commit{
				Subject: "fix(api/v2.1): handle empty body", Prefix: "fix",
				Scope: "api/v2.1", Desc: "handle empty body",
			},
		},
		{
			name: "ticket prefix",
			in:   "CUS-1234: add config",
			want: Commit{Subject: "CUS-1234: add config", Prefix: "CUS-1234", Desc: "add config"},
		},
		{
			name: "crlf",
			in:   "feat: add notifier\r\n\r\nBody here.\r\n",
			want: Commit{Subject: "feat: add notifier", Prefix: "feat", Desc: "add notifier", Body: "Body here."},
		},
		{
			name: "footer with hash separator",
			in:   "fix: patch leak\n\nCloses #17",
			want: Commit{
				Subject: "fix: patch leak", Prefix: "fix", Desc: "patch leak",
				Footers: []Footer{{Key: "Closes", Value: "17"}},
			},
		},
		{name: "no colon", in: "just some words", wantErr: true},
		{name: "empty", in: "", wantErr: true},
		{name: "colon but no space", in: "feat:add thing", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCommit(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseCommit(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseCommit(%q) mismatch (-want +got):\n%s", tt.in, diff)
			}
		})
	}
}

// ticketRules mirrors the `ticket` preset — the format git_gen.py enforced.
func ticketRules(branchSlug string) CommitRules {
	return CommitRules{
		Types:            []string{"feat", "fix"},
		TicketPattern:    `CUS-[0-9]+`,
		MaxSubject:       50,
		LowercaseDesc:    true,
		NoTrailingPeriod: true,
		ScopeMode:        ScopeOff,
		BranchSlug:       branchSlug,
	}
}

func conventionalRules() CommitRules {
	return CommitRules{
		Types: []string{
			"feat", "fix", "docs", "style", "refactor",
			"perf", "test", "build", "ci", "chore", "revert",
		},
		MaxSubject:       72,
		LowercaseDesc:    true,
		NoTrailingPeriod: true,
		AllowBody:        true,
		AllowFooters:     true,
		ScopeMode:        ScopeSuggest,
	}
}

// Ported from git_gen.py::self_check(): these asserts are the spec of `ticket`.
func TestCommitRulesTicket(t *testing.T) {
	tests := []struct {
		name       string
		msg        string
		branchSlug string
		ok         bool
	}{
		{name: "plain feat", msg: "feat: add telegram notifier", branchSlug: "x", ok: true},
		{name: "ticket prefix", msg: "CUS-1234: add telegram notifier config", branchSlug: "foo", ok: true},
		{name: "capitalised type and desc", msg: "Feat: Add Thing", branchSlug: "x"},
		{name: "trailing period", msg: "feat: add thing.", branchSlug: "x"},
		{name: "type outside the format", msg: "chore: whatever", branchSlug: "x"},
		{name: "too long", msg: "feat: " + strings.Repeat("x", 60), branchSlug: "x"},
		{name: "copied from branch slug", msg: "feat: add feature", branchSlug: "add feature"},
		{name: "body not allowed", msg: "feat: add thing\n\nAnd why.", branchSlug: "x"},
		{name: "scope not allowed", msg: "feat(api): add thing", branchSlug: "x"},
		{name: "no colon", msg: "add thing", branchSlug: "x"},
		{name: "empty", msg: "", branchSlug: "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, problems := ticketRules(tt.branchSlug).Check(tt.msg)
			if tt.ok && len(problems) > 0 {
				t.Errorf("Check(%q) = %v, want no problems", tt.msg, problems)
			}
			if !tt.ok && len(problems) == 0 {
				t.Errorf("Check(%q) accepted, want problems", tt.msg)
			}
		})
	}
}

func TestCommitRulesConventional(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		ok   bool
	}{
		{name: "subject only", msg: "feat: add notifier", ok: true},
		{name: "with body and footer", msg: "feat(api): add notifier\n\nWhy.\n\nRefs: CUS-1", ok: true},
		{name: "breaking", msg: "feat(api)!: drop v1", ok: true},
		{name: "subject over 72", msg: "feat: " + strings.Repeat("x", 70)},
		{name: "unknown type", msg: "wip: something"},
		{name: "trailing period", msg: "feat: add notifier."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, problems := conventionalRules().Check(tt.msg)
			if tt.ok != (len(problems) == 0) {
				t.Errorf("Check(%q) problems = %v, want ok=%v", tt.msg, problems, tt.ok)
			}
		})
	}
}

func TestCommitRulesScopeWhitelist(t *testing.T) {
	r := conventionalRules()
	r.ScopeMode = ScopeWhitelist
	r.Scopes = []string{"api", "cli"}

	if _, problems := r.Check("feat(api): add thing"); len(problems) > 0 {
		t.Errorf("whitelisted scope rejected: %v", problems)
	}
	if _, problems := r.Check("feat(db): add thing"); len(problems) == 0 {
		t.Error("scope outside the whitelist was accepted")
	}
	if _, problems := r.Check("feat: add thing"); len(problems) > 0 {
		t.Errorf("missing scope rejected: %v", problems)
	}
}

func TestCommitRulesReturnsCandidateOnFailure(t *testing.T) {
	got, problems := ticketRules("x").Check("```\nfeat: add thing.\n```")
	if len(problems) == 0 {
		t.Fatal("want problems for a trailing period")
	}
	if got != "feat: add thing." {
		t.Errorf("candidate = %q, want the sanitized message even on failure", got)
	}
}

func TestSlugRules(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "add-user-auth", want: "add-user-auth", ok: true},
		{in: "fix-login", want: "fix-login", ok: true},
		{in: "Add User", want: "add-user", ok: true},
		{in: "Add_User", want: "add_user"},
		{in: "trailing-", want: "trailing-"},
		{in: strings.Repeat("a", 41), want: strings.Repeat("a", 41)},
		{in: "  ", want: ""},
	}

	r := SlugRules{MaxLen: 40}
	for _, tt := range tests {
		got, problems := r.Check(tt.in)
		if got != tt.want {
			t.Errorf("Check(%q) value = %q, want %q", tt.in, got, tt.want)
		}
		if tt.ok != (len(problems) == 0) {
			t.Errorf("Check(%q) problems = %v, want ok=%v", tt.in, problems, tt.ok)
		}
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Add User Auth, now!", "add-user-auth-now"},
		{"one two three four five", "one-two-three-four"},
		{"Привет мир", ""},
		{"!!! ???", ""},
		{"already-kebab", "already-kebab"},
		{"CUS-1234 fix the thing", "cus-1234-fix-the"},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in, 4); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
