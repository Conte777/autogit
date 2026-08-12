// Package validate holds the pure text rules: parsing, canonicalisation and
// checking of commit messages and branch slugs. It imports nothing but stdlib.
package validate

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Footer is one `Key: Value` trailer line of a commit message.
type Footer struct {
	Key   string
	Value string
}

// Commit is a parsed commit message.
type Commit struct {
	Subject  string
	Prefix   string // the part before ':' minus scope and '!'
	Scope    string
	Breaking bool
	Desc     string // subject text after ": "
	Body     string
	Footers  []Footer
}

// Prefix charset is wider than Conventional Commits allows on purpose: the
// `ticket` preset puts `CUS-1234` where a type would normally go.
var subjectRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)(?:\(([^)]*)\))?(!)?: (.+)$`)

// footerRe matches `Key: value` and `Key #value` trailers. `BREAKING CHANGE` is
// the one key allowed to contain a space, per the Conventional Commits spec.
var footerRe = regexp.MustCompile(`^(BREAKING CHANGE|BREAKING-CHANGE|[A-Za-z0-9-]+)(: | #)(.*)$`)

// ParseCommit splits a message into subject, body and footers.
// It reports an error only when the subject has no parsable `prefix: desc`
// shape — every other rule lives in CommitRules.
func ParseCommit(msg string) (Commit, error) {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	lines := strings.Split(msg, "\n")

	var c Commit
	c.Subject = strings.TrimSpace(lines[0])
	if c.Subject == "" {
		return c, fmt.Errorf("empty subject")
	}
	m := subjectRe.FindStringSubmatch(c.Subject)
	if m == nil {
		return c, fmt.Errorf("subject must look like `type: description`")
	}
	c.Prefix, c.Scope, c.Breaking, c.Desc = m[1], m[2], m[3] == "!", m[4]

	rest := strings.Split(strings.Trim(strings.Join(lines[1:], "\n"), "\n"), "\n")
	if len(rest) == 1 && rest[0] == "" {
		return c, nil
	}

	// Footers are the trailing block: a run of trailer lines with no blank line
	// inside it. Anything above it is body.
	split := len(rest)
	for i := len(rest) - 1; i >= 0; i-- {
		if strings.TrimSpace(rest[i]) == "" {
			break
		}
		if footerRe.MatchString(rest[i]) {
			split = i
			continue
		}
		// A continuation line only counts if a trailer was already found below.
		if split <= i+1 && split < len(rest) {
			split = i + 1
		}
		break
	}
	for _, ln := range rest[split:] {
		if m := footerRe.FindStringSubmatch(ln); m != nil {
			c.Footers = append(c.Footers, Footer{Key: m[1], Value: strings.TrimSpace(m[3])})
			if m[1] == "BREAKING CHANGE" || m[1] == "BREAKING-CHANGE" {
				c.Breaking = true
			}
		}
	}
	c.Body = strings.Trim(strings.Join(rest[:split], "\n"), "\n")
	return c, nil
}

// ScopeMode controls how the scope part of a subject is treated.
type ScopeMode string

const (
	ScopeOff       ScopeMode = "off"       // scope forbidden
	ScopeSuggest   ScopeMode = "suggest"   // any scope accepted, history is offered as a hint
	ScopeWhitelist ScopeMode = "whitelist" // scope must come from Scopes
)

// CommitRules is the checkable half of a preset's commit format. It enforces
// only what a diff-blind checker can prove: "a body is needed here" and "this
// change is breaking" are prompt guidance, not rules, because enforcing them
// burns every retry and then fails a legitimate commit.
type CommitRules struct {
	Types            []string // allowed prefixes; empty means any
	TicketPattern    string   // regexp; a matching prefix is accepted instead of a type
	MaxSubject       int      // 0 = unlimited
	LowercaseDesc    bool
	NoTrailingPeriod bool
	AllowBody        bool
	AllowFooters     bool
	ScopeMode        ScopeMode
	Scopes           []string
	MaxBodyLine      int    // 0 = unlimited
	BranchSlug       string // when set, a description equal to it is rejected
}

// Check canonicalises raw model output and lists everything wrong with it.
// The canonical value is returned even when problems is non-empty, so the
// caller can show the user the last rejected candidate.
func (r CommitRules) Check(raw string) (string, []string) {
	msg := Sanitize(raw)
	if msg == "" {
		return "", []string{"model returned empty output"}
	}

	c, err := ParseCommit(msg)
	if err != nil {
		return msg, []string{r.shapeHint()}
	}

	var problems []string
	problems = append(problems, r.checkPrefix(c)...)

	if r.MaxSubject > 0 && len([]rune(c.Subject)) > r.MaxSubject {
		problems = append(problems, fmt.Sprintf("subject must be at most %d characters (got %d)",
			r.MaxSubject, len([]rune(c.Subject))))
	}
	if r.NoTrailingPeriod && strings.HasSuffix(c.Subject, ".") {
		problems = append(problems, "subject must not end with a period")
	}
	if r.LowercaseDesc && c.Desc != strings.ToLower(c.Desc) {
		problems = append(problems, "description must be lowercase")
	}
	if r.BranchSlug != "" && strings.EqualFold(c.Desc, r.BranchSlug) {
		problems = append(problems, "description is copied from the branch name; describe the diff instead")
	}
	if !r.AllowBody && c.Body != "" {
		problems = append(problems, "this format is a single line: no body")
	}
	if !r.AllowFooters && len(c.Footers) > 0 {
		problems = append(problems, "this format is a single line: no footers")
	}
	if r.MaxBodyLine > 0 {
		for ln := range strings.SplitSeq(c.Body, "\n") {
			if len([]rune(ln)) > r.MaxBodyLine {
				problems = append(problems, fmt.Sprintf("body lines must wrap at %d characters", r.MaxBodyLine))
				break
			}
		}
	}
	return msg, problems
}

func (r CommitRules) checkPrefix(c Commit) []string {
	var problems []string

	if r.TicketPattern != "" {
		if re, err := regexp.Compile("^(?:" + r.TicketPattern + ")$"); err == nil && re.MatchString(c.Prefix) {
			return r.checkScope(c)
		}
	}
	if len(r.Types) > 0 && !slices.Contains(r.Types, c.Prefix) {
		problems = append(problems, r.shapeHint())
	}
	return append(problems, r.checkScope(c)...)
}

func (r CommitRules) checkScope(c Commit) []string {
	switch r.ScopeMode {
	case ScopeOff:
		if c.Scope != "" {
			return []string{"scope is not allowed in this format"}
		}
	case ScopeWhitelist:
		if c.Scope != "" && !slices.Contains(r.Scopes, c.Scope) {
			return []string{fmt.Sprintf("scope %q is not one of: %s", c.Scope, strings.Join(r.Scopes, ", "))}
		}
	case ScopeSuggest:
	}
	return nil
}

func (r CommitRules) shapeHint() string {
	var alts []string
	if r.TicketPattern != "" {
		alts = append(alts, "<ticket>")
	}
	alts = append(alts, r.Types...)
	if len(alts) == 0 {
		return "subject must look like `type: description`"
	}
	return "subject must start with " + strings.Join(alts, "|") + " followed by `: `"
}

// SlugRules checks a branch slug.
type SlugRules struct {
	MaxLen int // 0 = unlimited
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Check canonicalises raw model output into a slug and lists its problems.
func (r SlugRules) Check(raw string) (string, []string) {
	s := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(Sanitize(raw)), " ", "-"))
	if s == "" {
		return "", []string{"model returned empty output"}
	}
	var problems []string
	if !slugRe.MatchString(s) {
		problems = append(problems, "slug must be lowercase kebab-case, [a-z0-9-] only")
	}
	if r.MaxLen > 0 && len(s) > r.MaxLen {
		problems = append(problems, fmt.Sprintf("slug must be at most %d characters (got %d)", r.MaxLen, len(s)))
	}
	return s, problems
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns free text into a kebab-case slug of at most maxWords words.
func Slugify(text string, maxWords int) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(text), "-"), "-")
	if s == "" {
		return ""
	}
	words := strings.Split(s, "-")
	if maxWords > 0 && len(words) > maxWords {
		words = words[:maxWords]
	}
	return strings.Join(words, "-")
}
