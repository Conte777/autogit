package install_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Conte777/autogit/internal/install"
)

func opts(dir string) install.Options {
	return install.Options{
		Dir:    dir,
		Binary: "autogit",
		Now:    time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC),
	}
}

// claudeDir builds a throwaway `.claude` directory. No test may ever look at
// the real one.
func claudeDir(t *testing.T, settings string) string {
	t.Helper()
	dir := t.TempDir()
	if settings != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func apply(t *testing.T, dir string) {
	t.Helper()
	p, err := install.PlanInstall(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func settingsOf(t *testing.T, dir string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "settings.json"))), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestInstallOnEmptyDir(t *testing.T) {
	dir := claudeDir(t, "")
	apply(t, dir)

	doc := settingsOf(t, dir)
	hooks, _ := doc["hooks"].(map[string]any)
	groups, _ := hooks["UserPromptSubmit"].([]any)
	if len(groups) != 1 {
		t.Fatalf("UserPromptSubmit = %v, want one group", groups)
	}
	raw, _ := json.Marshal(groups[0])
	if !strings.Contains(string(raw), "autogit hook") {
		t.Errorf("hook entry = %s", raw)
	}
	if !strings.Contains(string(raw), "120") {
		t.Errorf("hook entry has no timeout: %s", raw)
	}

	permissions, _ := doc["permissions"].(map[string]any)
	allow, _ := permissions["allow"].([]any)
	got, _ := json.Marshal(allow)
	for _, want := range []string{"mcp__autogit__commit", "mcp__autogit__branch"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("permissions.allow is missing %s: %s", want, got)
		}
	}

	for _, name := range []string{"commit", "commit-msg", "branch"} {
		body := readFile(t, filepath.Join(dir, "commands", name+".md"))
		if !strings.Contains(body, "disable-model-invocation: true") {
			t.Errorf("%s.md lacks disable-model-invocation; Claude would call it itself", name)
		}
	}
}

func TestInstallIsIdempotentByteForByte(t *testing.T) {
	dir := claudeDir(t, `{"model":"opus","permissions":{"allow":["Bash"]}}`)
	apply(t, dir)
	first := readFile(t, filepath.Join(dir, "settings.json"))

	apply(t, dir)
	if second := readFile(t, filepath.Join(dir, "settings.json")); second != first {
		t.Errorf("the second install changed the file:\n--- first\n%s\n--- second\n%s", first, second)
	}

	plan, err := install.PlanInstall(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() {
		t.Errorf("a third plan still wants to change something: %v", plan.Changes)
	}
}

func TestInstallKeepsUnrelatedSettings(t *testing.T) {
	dir := claudeDir(t, `{
	  "model": "opus",
	  "permissions": {"allow": ["Bash", "Read"], "deny": ["WebFetch"]},
	  "hooks": {
	    "PreToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"guard.sh"}]}],
	    "UserPromptSubmit": [{"hooks":[{"type":"command","command":"other-tool hook"}]}]
	  }
	}`)
	apply(t, dir)

	doc := settingsOf(t, dir)
	if doc["model"] != "opus" {
		t.Errorf("model = %v", doc["model"])
	}
	raw, _ := json.Marshal(doc)
	for _, want := range []string{"guard.sh", "other-tool hook", "WebFetch", "Bash", "Read"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("install dropped %q:\n%s", want, raw)
		}
	}
}

func TestInstallReplacesItsOwnOldHookEntry(t *testing.T) {
	dir := claudeDir(t, `{"hooks":{"UserPromptSubmit":[
	  {"hooks":[{"type":"command","command":"/old/path/autogit hook","timeout":60}]}
	]}}`)
	apply(t, dir)

	doc := settingsOf(t, dir)
	raw, _ := json.Marshal(doc["hooks"])
	if strings.Contains(string(raw), "/old/path/") {
		t.Errorf("the stale entry survived:\n%s", raw)
	}
	if strings.Count(string(raw), "autogit hook") != 1 {
		t.Errorf("want exactly one autogit hook entry:\n%s", raw)
	}
}

// An absolute binary path is the normal case, and the entry has to be
// recognised as ours next time or every install appends a duplicate.
func TestInstallIsIdempotentWithAnAbsoluteBinaryPath(t *testing.T) {
	dir := claudeDir(t, `{"hooks":{"UserPromptSubmit":[
	  {"hooks":[{"type":"command","command":"other-tool hook"}]}
	]}}`)
	o := opts(dir)
	o.Binary = "/usr/local/bin/autogit"

	for range 3 {
		p, err := install.PlanInstall(o)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Apply(); err != nil {
			t.Fatal(err)
		}
	}

	raw, _ := json.Marshal(settingsOf(t, dir)["hooks"])
	if got := strings.Count(string(raw), "/usr/local/bin/autogit hook"); got != 1 {
		t.Errorf("%d autogit hook entries after three installs, want 1:\n%s", got, raw)
	}
	if !strings.Contains(string(raw), "other-tool hook") {
		t.Errorf("another tool's hook was dropped:\n%s", raw)
	}
}

func TestInstallWritesThroughASymlink(t *testing.T) {
	real := t.TempDir()
	target := filepath.Join(real, "settings.json")
	if err := os.WriteFile(target, []byte(`{"model":"opus"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	apply(t, dir)

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a real file; the config is now detached from its repo")
	}
	if !strings.Contains(readFile(t, target), "autogit hook") {
		t.Error("the change did not reach the file the symlink points at")
	}
}

func TestInstallBacksUpWithAUniqueName(t *testing.T) {
	dir := claudeDir(t, `{"model":"opus"}`)

	first := opts(dir)
	p, _ := install.PlanInstall(first)
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}

	second := opts(dir)
	second.Now = first.Now.Add(time.Hour)
	// Force a second write so a second backup is taken.
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"model":"sonnet"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _ = install.PlanInstall(second)
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}

	entries, err := filepath.Glob(filepath.Join(dir, "settings.json.autogit-*.bak"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("%d backups, want 2: a fixed .bak name would lose the original", len(entries))
	}
	if !strings.Contains(readFile(t, entries[0]), "opus") {
		t.Error("the first backup does not hold the original content")
	}
}

func TestUninstallRemovesOnlyItsOwn(t *testing.T) {
	dir := claudeDir(t, `{
	  "model": "opus",
	  "permissions": {"allow": ["Bash", "mcp__git__commit"]},
	  "hooks": {"UserPromptSubmit": [{"hooks":[{"type":"command","command":"other-tool hook"}]}]}
	}`)
	apply(t, dir)

	p, err := install.PlanUninstall(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}

	doc := settingsOf(t, dir)
	raw, _ := json.Marshal(doc)
	if strings.Contains(string(raw), "autogit") {
		t.Errorf("uninstall left autogit behind:\n%s", raw)
	}
	for _, want := range []string{"other-tool hook", "mcp__git__commit", "Bash", "opus"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("uninstall removed %q, which it never added:\n%s", want, raw)
		}
	}

	for _, name := range []string{"commit", "commit-msg", "branch"} {
		if _, err := os.Stat(filepath.Join(dir, "commands", name+".md")); !os.IsNotExist(err) {
			t.Errorf("commands/%s.md was not removed", name)
		}
	}
}

func TestUninstallLeavesAReplacedCommandFileAlone(t *testing.T) {
	dir := claudeDir(t, "")
	apply(t, dir)

	// The user rewrote the stub after installing: it is theirs now.
	handWritten := filepath.Join(dir, "commands", "branch.md")
	if err := os.WriteFile(handWritten, []byte("my own command\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p, err := install.PlanUninstall(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, handWritten); got != "my own command\n" {
		t.Errorf("branch.md = %q; uninstall must never delete a file it did not write", got)
	}
}

func TestInstallBacksUpAnExistingCommandFile(t *testing.T) {
	dir := claudeDir(t, "")
	if err := os.MkdirAll(filepath.Join(dir, "commands"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "commands", "commit.md")
	if err := os.WriteFile(path, []byte("the previous stub\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	apply(t, dir)

	backups, err := filepath.Glob(path + ".autogit-*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("%d backups of commit.md, want 1", len(backups))
	}
	if got := readFile(t, backups[0]); got != "the previous stub\n" {
		t.Errorf("backup = %q", got)
	}
}

func TestUninstallOnACleanDirDoesNothing(t *testing.T) {
	dir := claudeDir(t, `{"model":"opus"}`)
	p, err := install.PlanUninstall(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Empty() {
		t.Errorf("uninstall wants to change something it never installed: %v", p.Changes)
	}
}

func TestPlanDoesNotWriteAnything(t *testing.T) {
	dir := claudeDir(t, "")
	if _, err := install.PlanInstall(opts(dir)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("planning touched the disk: %v", entries)
	}
}

func TestBrokenSettingsIsAnError(t *testing.T) {
	dir := claudeDir(t, `{ not json`)
	if _, err := install.PlanInstall(opts(dir)); err == nil {
		t.Fatal("PlanInstall accepted an unparsable settings.json")
	}
}
