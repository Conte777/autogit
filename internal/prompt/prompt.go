// Package prompt loads one-file prompts: optional frontmatter, then `## System`
// and `## User` sections rendered as text/template.
package prompt

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"
)

// Prompt is a loaded, parsed and dry-run-validated prompt file.
type Prompt struct {
	Name string
	Meta map[string]string

	system *template.Template
	user   *template.Template
}

// Parse reads src and checks both templates against probe, so a typo in a
// field name fails at load time rather than in the middle of a commit.
func Parse(name, src string, probe any) (*Prompt, error) {
	meta, body, err := splitFrontmatter(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	system, user, err := splitSections(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	p := &Prompt{Name: name, Meta: meta}
	if p.system, err = compile(name+":system", system, probe); err != nil {
		return nil, err
	}
	if p.user, err = compile(name+":user", user, probe); err != nil {
		return nil, err
	}
	return p, nil
}

// Load reads a prompt file from disk.
func Load(path string, probe any) (*Prompt, error) {
	src, err := os.ReadFile(path) //nolint:gosec // the path comes from the user's own config
	if err != nil {
		return nil, err
	}
	return Parse(path, string(src), probe)
}

func compile(name, text string, probe any) (*template.Template, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if err := t.Execute(&bytes.Buffer{}, probe); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return t, nil
}

// Render produces the system prompt and the first user turn.
func (p *Prompt) Render(data any) (system, user string, err error) {
	var sb, ub bytes.Buffer
	if err := p.system.Execute(&sb, data); err != nil {
		return "", "", fmt.Errorf("%s: system: %w", p.Name, err)
	}
	if err := p.user.Execute(&ub, data); err != nil {
		return "", "", fmt.Errorf("%s: user: %w", p.Name, err)
	}
	return strings.TrimSpace(sb.String()), strings.TrimSpace(ub.String()), nil
}

// splitFrontmatter reads an optional `---` block of `key: value` scalars.
// A hand-rolled reader is enough here: a YAML dependency for two scalars is
// not a trade worth making.
func splitFrontmatter(src string) (map[string]string, string, error) {
	src = strings.TrimLeft(src, "\ufeff \t\n")
	if !strings.HasPrefix(src, "---\n") {
		return map[string]string{}, src, nil
	}
	rest := src[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, "", fmt.Errorf("frontmatter is not closed by `---`")
	}
	block, body := rest[:end], rest[end+len("\n---"):]

	meta := map[string]string{}
	for i, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, "", fmt.Errorf("frontmatter line %d is not `key: value`: %q", i+1, line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, "", fmt.Errorf("frontmatter line %d has an empty key", i+1)
		}
		meta[key] = unquote(strings.TrimSpace(value))
	}
	return meta, strings.TrimPrefix(body, "\n"), nil
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// splitSections finds `## System` and `## User` in either order.
func splitSections(body string) (system, user string, err error) {
	type section struct {
		name  string
		start int
		end   int
	}
	var found []section

	lines := strings.Split(body, "\n")
	offset := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			name := strings.ToLower(strings.TrimSpace(trimmed[3:]))
			if name == "system" || name == "user" {
				if n := len(found); n > 0 {
					found[n-1].end = offset
				}
				found = append(found, section{name: name, start: offset + len(line) + 1, end: len(body)})
			}
		}
		offset += len(line) + 1
	}

	for _, s := range found {
		end := min(s.end, len(body))
		start := min(s.start, end)
		text := strings.TrimSpace(body[start:end])
		switch s.name {
		case "system":
			system = text
		case "user":
			user = text
		}
	}
	if system == "" {
		return "", "", fmt.Errorf("missing or empty `## System` section")
	}
	if user == "" {
		return "", "", fmt.Errorf("missing or empty `## User` section")
	}
	return system, user, nil
}
