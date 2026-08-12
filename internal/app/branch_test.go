package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/app"
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

	a := e.app()
	a.Preset.Branch.Name = "{{.Ticket}}-{{.Slug}}"
	got, err := a.Branch(context.Background(), app.BranchRequest{
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
