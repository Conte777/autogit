package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const credentialQuery = "protocol=https\nhost=example.com\n\n"

// askpass writes a script that records every call and answers a password, and
// points every mechanism git has at it: the two environment variables and the
// config key.
func askpass(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "askpass.sh")
	marker := script + ".called"
	body := "#!/bin/sh\necho \"$*\" >> " + marker + "\necho hunter2\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_ASKPASS", script)
	t.Setenv("SSH_ASKPASS", script)
	runGit(t, dir, "config", "core.askPass", script)
	return marker
}

func wasCalled(t *testing.T, marker string) bool {
	t.Helper()
	_, err := os.Stat(marker)
	return err == nil
}

func credentialFill(t *testing.T, r *Repo) (string, error) {
	t.Helper()
	return r.run(context.Background(), 10*time.Second, credentialQuery, "credential", "fill")
}

// A helper that draws a prompt must fail immediately rather than hang: an empty
// GIT_ASKPASS is what disables all three mechanisms at once, because git reads
// core.askPass and SSH_ASKPASS only when GIT_ASKPASS is unset.
func TestNonInteractiveNeutralisesAskpassHelpers(t *testing.T) {
	dir := newRepo(t)
	marker := askpass(t, dir)

	r, err := Open(context.Background(), dir, Options{Interactive: false})
	if err != nil {
		t.Fatal(err)
	}

	out, err := credentialFill(t, r)
	if err == nil {
		t.Fatalf("credential fill succeeded without a terminal: %q", out)
	}
	if wasCalled(t, marker) {
		t.Error("the askpass helper ran under --no-input")
	}
	if !strings.Contains(err.Error(), "terminal prompts disabled") {
		t.Errorf("err = %v, want git refusing to prompt", err)
	}
}

// Credential storage answers from a keychain without asking anyone, so it must
// keep working where a prompting helper is refused.
func TestNonInteractiveKeepsCredentialStorage(t *testing.T) {
	dir := newRepo(t)
	askpass(t, dir)
	runGit(t, dir, "config", "credential.helper",
		`!f(){ test "$1" = get && echo username=stored && echo password=s3cret; }; f`)

	r, err := Open(context.Background(), dir, Options{Interactive: false})
	if err != nil {
		t.Fatal(err)
	}

	out, err := credentialFill(t, r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "username=stored") || !strings.Contains(out, "password=s3cret") {
		t.Errorf("credential fill = %q, want the stored credentials", out)
	}
}

func TestInteractiveLeavesAskpassHelpersAlone(t *testing.T) {
	dir := newRepo(t)
	marker := askpass(t, dir)

	r, err := Open(context.Background(), dir, Options{Interactive: true})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := credentialFill(t, r); err != nil {
		t.Fatal(err)
	}
	if !wasCalled(t, marker) {
		t.Error("the askpass helper was skipped on an interactive run")
	}
}
