package validate

import (
	"regexp"
	"strings"
)

var (
	fenceRe      = regexp.MustCompile("^```[a-zA-Z0-9_-]*$")
	blankRunRe   = regexp.MustCompile(`\n{3,}`)
	trailingWSRe = regexp.MustCompile(`[ \t]+\n`)
)

// Sanitize turns raw model output into a canonical commit message.
// It keeps the block multi-line: a Conventional Commits body and its footers
// must survive, so it strips only wrappers (fences, quotes) and whitespace.
func Sanitize(raw string) string {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = strings.Trim(s, "\n \t")
	s = stripFences(s)
	s = stripWrappingQuotes(s)

	s = trailingWSRe.ReplaceAllString(s+"\n", "\n")
	s = blankRunRe.ReplaceAllString(s, "\n\n")
	s = strings.Trim(s, "\n \t")

	// A subject glued to its body is the common model slip; git needs the blank.
	if i := strings.IndexByte(s, '\n'); i >= 0 && len(s) > i+1 && s[i+1] != '\n' {
		s = s[:i] + "\n\n" + s[i+1:]
	}
	return s
}

func stripFences(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) >= 2 && fenceRe.MatchString(strings.TrimSpace(lines[0])) &&
		strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[1 : len(lines)-1]
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n \t")
}

// stripWrappingQuotes removes a quote pair only when it wraps the whole block —
// an apostrophe in the description must not be mistaken for a wrapper.
func stripWrappingQuotes(s string) string {
	for _, q := range []byte{'"', '\''} {
		if len(s) >= 2 && s[0] == q && s[len(s)-1] == q &&
			!strings.ContainsRune(s[1:len(s)-1], rune(q)) {
			return strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}
