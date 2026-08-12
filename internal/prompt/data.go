package prompt

// CommitData is the template contract of a commit prompt. Every field a
// template may reference lives here; anything else fails at load time.
type CommitData struct {
	Ticket        string // "" when the branch carries none — never invent one
	Branch        string
	Detached      bool
	Files         []string
	Diff          string
	DiffTruncated bool
	Types         []string
	MaxSubject    int
	Scopes        []string // history vocabulary; empty means "say nothing about scopes"
	ScopeMode     string   // off | suggest | whitelist
	WantBody      bool
	AllowFooters  bool
}

// BranchData is the template contract of a branch prompt.
type BranchData struct {
	Ticket        string
	Description   string // free text from the user; "" when inferred from the diff
	Files         []string
	Diff          string
	DiffTruncated bool
	Types         []string
	MaxWords      int
	NeedType      bool // no ticket, so the model has to pick a type as well
}
