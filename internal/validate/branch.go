package validate

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// IsProtected matches a branch name against protected-branch patterns.
// Patterns are shell globs, so `release/*` covers `release/1.2`.
func IsProtected(branch string, patterns []string) bool {
	for _, p := range patterns {
		if strings.EqualFold(branch, p) {
			return true
		}
		if ok, err := path.Match(strings.ToLower(p), strings.ToLower(branch)); err == nil && ok {
			return true
		}
	}
	return false
}

// BranchSlugText turns a branch name into the phrase a lazy model would copy
// into the description: the last path segment, hyphens as spaces.
func BranchSlugText(branch string) string {
	seg := branch
	if i := strings.LastIndexByte(seg, '/'); i >= 0 {
		seg = seg[i+1:]
	}
	return strings.ToLower(strings.ReplaceAll(seg, "-", " "))
}

// ExtractTicket returns the first substring of branch matching pattern,
// upper-cased, or "" when there is none.
func ExtractTicket(branch, pattern string) string {
	if pattern == "" {
		return ""
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return ""
	}
	return strings.ToUpper(re.FindString(branch))
}

var scopeRe = regexp.MustCompile(`^[a-z]+\(([^)]{1,20})\)!?:`)

// CollectScopes mines the scope vocabulary out of commit subjects, most used
// first. Below minConventional matching subjects it returns nothing: a sample
// of two examples teaches the model worse than no examples at all.
func CollectScopes(subjects []string, top, minConventional int) []string {
	counts := map[string]int{}
	conventional := 0
	for _, s := range subjects {
		if !subjectRe.MatchString(s) {
			continue
		}
		conventional++
		if m := scopeRe.FindStringSubmatch(s); m != nil {
			counts[m[1]]++
		}
	}
	if conventional < minConventional {
		return nil
	}

	scopes := make([]string, 0, len(counts))
	for s := range counts {
		scopes = append(scopes, s)
	}
	sort.Slice(scopes, func(i, j int) bool {
		if counts[scopes[i]] != counts[scopes[j]] {
			return counts[scopes[i]] > counts[scopes[j]]
		}
		return scopes[i] < scopes[j]
	})
	if top > 0 && len(scopes) > top {
		scopes = scopes[:top]
	}
	return scopes
}
