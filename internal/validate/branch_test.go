package validate

import "testing"

func TestExtractTicket(t *testing.T) {
	const anchored = `^[A-Z][A-Z0-9]+-[0-9]+`
	tests := []struct {
		name, branch, pattern, want string
	}{
		{"ticket as the first segment", "ABC-123/add-login", anchored, "ABC-123"},
		{"ticket as the whole name", "ABC-123", anchored, "ABC-123"},
		{"ticket leading a jira-style name", "ABC-123-add-login", anchored, "ABC-123"},
		{"version number in the slug", "feat/release-v0-3-2", anchored, ""},
		{"acronym with a number in the slug", "fix/utf-8-paths", anchored, ""},
		{"ticket after a type is not at the start", "feat/ABC-123-login", anchored, ""},
		{"lowercase ticket under a case-sensitive pattern", "abc-123/login", anchored, ""},
		{"lowercase ticket under a pattern that asks for it", "abc-123/login", `(?i)` + anchored, "abc-123"},
		{"unanchored pattern searches the whole name", "feat/ABC-123-login", `[A-Z]+-[0-9]+`, "ABC-123"},
		{"no pattern", "ABC-123/login", "", ""},
		{"invalid pattern", "ABC-123/login", "[", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractTicket(tt.branch, tt.pattern); got != tt.want {
				t.Errorf("ExtractTicket(%q, %q) = %q, want %q", tt.branch, tt.pattern, got, tt.want)
			}
		})
	}
}
