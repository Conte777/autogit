package prompt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/prompt"
)

type probe struct {
	Ticket string
	Items  []string
	Want   bool
}

func TestParseSections(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr string
	}{
		{
			name: "no frontmatter",
			src:  "## System\nbe brief\n\n## User\ndo it",
		},
		{
			name: "user before system",
			src:  "## User\ndo it\n\n## System\nbe brief",
		},
		{
			name: "frontmatter with extra keys",
			src:  "---\ndescription: x\nauthor: someone\n---\n## System\ns\n\n## User\nu",
		},
		{
			name:    "unterminated frontmatter",
			src:     "---\ndescription: x\n## System\ns\n\n## User\nu",
			wantErr: "not closed",
		},
		{
			name:    "frontmatter line without a colon",
			src:     "---\njust words\n---\n## System\ns\n\n## User\nu",
			wantErr: "key: value",
		},
		{
			name:    "missing user section",
			src:     "## System\nbe brief",
			wantErr: "## User",
		},
		{
			name:    "missing system section",
			src:     "## User\ndo it",
			wantErr: "## System",
		},
		{
			name:    "empty system section",
			src:     "## System\n\n## User\ndo it",
			wantErr: "## System",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := prompt.Parse(tt.name, tt.src, probe{})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Parse() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse() = %v, want an error mentioning %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseKeepsFrontmatter(t *testing.T) {
	p, err := prompt.Parse("x", "---\ndescription: \"a prompt\"\n# a comment\n---\n## System\ns\n\n## User\nu", probe{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Meta["description"] != "a prompt" {
		t.Errorf("Meta = %v, want the quotes stripped", p.Meta)
	}
}

func TestUnknownFieldFailsAtLoadNotAtCall(t *testing.T) {
	_, err := prompt.Parse("x", "## System\n{{.Nonexistent}}\n\n## User\nu", probe{})
	if err == nil {
		t.Fatal("Parse accepted a template referencing a field the data type does not have")
	}
	if !strings.Contains(err.Error(), "Nonexistent") {
		t.Errorf("err = %v, want it to name the bad field", err)
	}
}

func TestRenderConditionals(t *testing.T) {
	src := "## System\n{{if .Want}}wants it{{else}}does not{{end}}\n\n" +
		"## User\n{{if .Ticket}}Ticket: {{.Ticket}}{{end}}\n{{range .Items}}- {{.}}\n{{end}}"
	p, err := prompt.Parse("x", src, probe{})
	if err != nil {
		t.Fatal(err)
	}

	system, user, err := p.Render(probe{Ticket: "CUS-1", Items: []string{"a", "b"}, Want: true})
	if err != nil {
		t.Fatal(err)
	}
	if system != "wants it" {
		t.Errorf("system = %q", system)
	}
	if !strings.Contains(user, "Ticket: CUS-1") || !strings.Contains(user, "- b") {
		t.Errorf("user =\n%s", user)
	}

	system, user, err = p.Render(probe{})
	if err != nil {
		t.Fatal(err)
	}
	if system != "does not" {
		t.Errorf("system = %q", system)
	}
	if strings.Contains(user, "Ticket") {
		t.Errorf("the ticket block survived with no ticket:\n%s", user)
	}
}

func TestLoadFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit.md")
	if err := os.WriteFile(path, []byte("## System\ns\n\n## User\nu"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := prompt.Load(path, probe{}); err != nil {
		t.Fatal(err)
	}
	if _, err := prompt.Load(filepath.Join(dir, "missing.md"), probe{}); err == nil {
		t.Fatal("Load succeeded on a missing file")
	}
}
