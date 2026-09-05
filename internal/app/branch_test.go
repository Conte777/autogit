package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/preset"
)

func currentBranch(t *testing.T, e *env) string {
	t.Helper()
	return strings.TrimSpace(e.git("branch", "--show-current"))
}

func TestBranchFromDescriptionAndTicketSkipsTheModel(t *testing.T) {
	e := newEnv(t)
	e.cfg.Preset = "ticket"
	e.commitFile("a.txt", "one\n", "init")

	got, err := e.app().Branch(context.Background(), app.BranchRequest{
		Ticket:      "CUS-1234",
		Description: "Add User Auth, now!",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "CUS-1234/add-user-auth-now" {
		t.Errorf("Name = %q", got.Name)
	}
	if e.prov.Sessions != 0 {
		t.Error("the model was asked although both halves of the name were known")
	}
	if currentBranch(t, e) != got.Name {
		t.Errorf("checked out %q, want %q", currentBranch(t, e), got.Name)
	}
}

func TestBranchFromDescriptionAsksOnlyForTheType(t *testing.T) {
	e := newEnv(t, "fix broken-thing")
	e.commitFile("a.txt", "one\n", "init")

	got, err := e.app().Branch(context.Background(), app.BranchRequest{Description: "fix the login redirect"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "fix/fix-the-login-redirect" {
		t.Errorf("Name = %q; the slug must come from the description, the type from the model", got.Name)
	}
}

func TestBranchFromDiff(t *testing.T) {
	e := newEnv(t, "feat add-user-auth")
	e.commitFile("a.txt", "one\n", "init")
	e.write("auth.go", "package auth\n")
	e.git("add", ".")

	got, err := e.app().Branch(context.Background(), app.BranchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "feat/add-user-auth" {
		t.Errorf("Name = %q", got.Name)
	}
}

func TestBranchFromDiffOnRepoWithoutCommits(t *testing.T) {
	e := newEnv(t, "feat add-first-file")
	e.write("a.txt", "one\n")
	e.git("add", ".")

	got, err := e.app().Branch(context.Background(), app.BranchRequest{})
	if err != nil {
		t.Fatalf("a repository with no commits broke branch: %v", err)
	}
	if got.Name != "feat/add-first-file" {
		t.Errorf("Name = %q", got.Name)
	}
}

func TestBranchWithTicketDoesNotAskForAType(t *testing.T) {
	e := newEnv(t, "add-user-auth")
	e.cfg.Preset = "ticket"
	e.commitFile("a.txt", "one\n", "init")
	e.write("auth.go", "package auth\n")
	e.git("add", ".")

	got, err := e.app().Branch(context.Background(), app.BranchRequest{Ticket: "CUS-7"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "CUS-7/add-user-auth" {
		t.Errorf("Name = %q", got.Name)
	}
	if strings.Contains(systemPromptOf(t, e.prov), "<type> <slug>") {
		t.Error("the model was asked for a type although the ticket already supplies the prefix")
	}
}

func TestBranchNoDescriptionNoChanges(t *testing.T) {
	e := newEnv(t)
	e.commitFile("a.txt", "one\n", "init")

	_, err := e.app().Branch(context.Background(), app.BranchRequest{})
	if !errors.Is(err, app.ErrNoBranchInput) {
		t.Fatalf("err = %v, want ErrNoBranchInput", err)
	}
}

func TestBranchNameCollision(t *testing.T) {
	e := newEnv(t)
	e.cfg.Preset = "ticket"
	e.commitFile("a.txt", "one\n", "init")
	e.git("branch", "CUS-1/add-user-auth")

	_, err := e.app().Branch(context.Background(), app.BranchRequest{
		Ticket:      "CUS-1",
		Description: "add user auth",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want a collision error", err)
	}
}

func TestBranchRejectsAMalformedTicket(t *testing.T) {
	e := newEnv(t)
	e.cfg.Preset = "ticket"
	e.commitFile("a.txt", "one\n", "init")

	_, err := e.app().Branch(context.Background(), app.BranchRequest{
		Ticket:      "not-a-ticket",
		Description: "add user auth",
	})
	if err == nil {
		t.Fatal("a ticket that does not match the preset pattern was accepted")
	}
}

func TestBranchCorrectsAnInvalidSlug(t *testing.T) {
	e := newEnv(t, "feat Add_User_Auth", "feat add-user-auth")
	e.cfg.Attempts = 3
	e.commitFile("a.txt", "one\n", "init")
	e.write("auth.go", "package auth\n")
	e.git("add", ".")

	got, err := e.app().Branch(context.Background(), app.BranchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "feat/add-user-auth" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", got.Attempts)
	}
	if e.prov.Sessions != 1 {
		t.Errorf("sessions = %d; the correction must reuse the session", e.prov.Sessions)
	}
}

func TestBranchCustomNameTemplate(t *testing.T) {
	e := newEnv(t)
	e.commitFile("a.txt", "one\n", "init")

	e.repoConfig(`{"presets": {"conventional": {"branch": {"name": "{{.Ticket}}-{{.Slug}}"}}}}`)

	got, err := e.app().Branch(context.Background(), app.BranchRequest{
		Ticket:      "CUS-9",
		Description: "add user auth",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "CUS-9-add-user-auth" {
		t.Errorf("Name = %q", got.Name)
	}
}

func branchFormat(t *testing.T, name string) preset.BranchFormat {
	t.Helper()
	p, ok := preset.Builtin(name)
	if !ok {
		t.Fatalf("no builtin preset %q", name)
	}
	return p.Branch
}

func TestParseBranchArgs(t *testing.T) {
	conventional := branchFormat(t, "conventional")
	ticket := branchFormat(t, "ticket")

	tests := []struct {
		name   string
		args   []string
		format preset.BranchFormat
		want   app.BranchRequest
	}{
		{
			name:   "description shaped like a ticket stays a description",
			args:   []string{"a-1", "fix", "login"},
			format: conventional,
			want:   app.BranchRequest{Description: "a-1 fix login"},
		},
		{
			name:   "ticket matching the preset becomes the prefix",
			args:   []string{"AG-12", "fix", "login"},
			format: conventional,
			want:   app.BranchRequest{Ticket: "AG-12", Description: "fix login"},
		},
		{
			name:   "a lowercase ticket is uppercased",
			args:   []string{"cus-9"},
			format: ticket,
			want:   app.BranchRequest{Ticket: "CUS-9"},
		},
		{
			name:   "a ticket from another preset is description text",
			args:   []string{"AG-12", "fix", "login"},
			format: ticket,
			want:   app.BranchRequest{Description: "AG-12 fix login"},
		},
		{
			name:   "a preset without a pattern guesses no ticket at all",
			args:   []string{"AG-12", "fix", "login"},
			format: preset.BranchFormat{},
			want:   app.BranchRequest{Description: "AG-12 fix login"},
		},
		{
			name:   "an empty leading argument is not a ticket",
			args:   []string{"", "CUS-9", "fix"},
			format: ticket,
			want:   app.BranchRequest{Description: " CUS-9 fix"},
		},
		{
			name:   "a ticket in only part of the word is a description",
			args:   []string{"AG-12-and-more", "fix"},
			format: conventional,
			want:   app.BranchRequest{Description: "AG-12-and-more fix"},
		},
		{
			name:   "no args at all",
			args:   nil,
			format: conventional,
			want:   app.BranchRequest{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := app.ParseBranchArgs(tt.args, tt.format)
			if got != tt.want {
				t.Errorf("ParseBranchArgs(%q) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}
