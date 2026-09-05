package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/ui"
)

// envOf turns a map into the lookup build reads instead of the process.
func envOf(kv map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := kv[key]
		return v, ok
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// macOS hands out /var, git reports /private/var; the config layer compares
	// the two, so pin the test to what git will say.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// ticketOf probes the preset the App was actually built with: `AB-12` is a
// ticket under `conventional` and plain description text under `ticket`.
func ticketOf(a *app.App) string {
	return a.ParseBranchArgs([]string{"AB-12", "add", "user", "auth"}).Ticket
}

func TestBuildTakesThePresetFromTheGlobalConfig(t *testing.T) {
	dir := initRepo(t)
	global := writeFile(t, dir, "global.json", `{"preset":"ticket"}`)

	a, err := build(context.Background(),
		&globals{repo: dir, confPath: global, env: envOf(nil)}, ui.Noop{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ticketOf(a); got != "" {
		t.Errorf("ticket = %q; the App did not get the configured preset", got)
	}
}

func TestBuildPresetFlagBeatsTheConfigFile(t *testing.T) {
	dir := initRepo(t)
	global := writeFile(t, dir, "global.json", `{"preset":"ticket"}`)

	a, err := build(context.Background(),
		&globals{repo: dir, confPath: global, preset: "conventional", env: envOf(nil)}, ui.Noop{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ticketOf(a); got != "AB-12" {
		t.Errorf("ticket = %q; --preset lost to the config file", got)
	}
}

func TestBuildProviderFlagBeatsTheConfigFile(t *testing.T) {
	dir := initRepo(t)
	global := writeFile(t, dir, "global.json", `{"provider":"openai"}`)

	_, err := build(context.Background(), &globals{
		repo:     dir,
		confPath: global,
		provider: "anthropic",
		env:      envOf(map[string]string{"OPENAI_API_KEY": "sk-openai"}),
	}, ui.Noop{})
	if err == nil {
		t.Fatal("build used the OpenAI key for --provider anthropic")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("err = %v, want it to name the flag's provider", err)
	}
}

func TestBuildMissingAPIKeyNamesTheVariable(t *testing.T) {
	dir := initRepo(t)
	global := writeFile(t, dir, "global.json", `{"provider":"openai"}`)

	_, err := build(context.Background(),
		&globals{repo: dir, confPath: global, env: envOf(nil)}, ui.Noop{})
	if err == nil {
		t.Fatal("build accepted a key-less provider")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("err = %v, want the variable to set", err)
	}
	// A provider that cannot be built is a configuration problem: nothing was
	// ever sent, and scripts branch on the code.
	if got := ExitCode(err); got != ExitConfig {
		t.Errorf("ExitCode(%v) = %d, want %d", err, got, ExitConfig)
	}
}

// The whole point of the env seam: build reads what it is handed, not what the
// process happens to carry.
func TestBuildIgnoresTheProcessEnvironment(t *testing.T) {
	dir := initRepo(t)
	global := writeFile(t, dir, "global.json", `{"provider":"openai"}`)
	t.Setenv("OPENAI_API_KEY", "sk-from-the-process")

	if _, err := build(context.Background(),
		&globals{repo: dir, confPath: global, env: envOf(nil)}, ui.Noop{}); err == nil {
		t.Fatal("build read the process environment instead of the one it was given")
	}

	a, err := build(context.Background(), &globals{
		repo:     dir,
		confPath: global,
		env:      envOf(map[string]string{"OPENAI_API_KEY": "sk-synthetic"}),
	}, ui.Noop{})
	if err != nil {
		t.Fatal(err)
	}
	if a == nil {
		t.Fatal("build returned no App")
	}
}

func TestBuildRejectsABrokenPreset(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, ".autogit.json",
		`{"presets":{"conventional":{"commit":{"body":{"mode":"sometimes"}}}}}`)

	_, err := build(context.Background(),
		&globals{repo: dir, confPath: filepath.Join(dir, "absent.json"), env: envOf(nil)}, ui.Noop{})
	if err == nil {
		t.Fatal("build accepted a preset that cannot run")
	}
	if got := ExitCode(err); got != ExitConfig {
		t.Errorf("ExitCode(%v) = %d, want %d", err, got, ExitConfig)
	}
}

func TestBuildRejectsAnUnknownPresetName(t *testing.T) {
	dir := initRepo(t)

	_, err := build(context.Background(), &globals{
		repo:     dir,
		confPath: filepath.Join(dir, "absent.json"),
		preset:   "nope",
		env:      envOf(nil),
	}, ui.Noop{})
	if err == nil {
		t.Fatal("build accepted an unknown preset")
	}
	if got := ExitCode(err); got != ExitConfig {
		t.Errorf("ExitCode(%v) = %d, want %d", err, got, ExitConfig)
	}
}

// The repository layer has to be found from wherever the command was pointed,
// not only from the root.
func TestBuildFindsTheRepositoryConfigFromASubdirectory(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, ".autogit.json", `{"preset":"ticket"}`)
	sub := filepath.Join(dir, "internal", "deep")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}

	a, err := build(context.Background(),
		&globals{repo: sub, confPath: filepath.Join(dir, "absent.json"), env: envOf(nil)}, ui.Noop{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ticketOf(a); got != "" {
		t.Errorf("ticket = %q; the repository config was not found from %s", got, sub)
	}
}

func TestBuildOutsideARepository(t *testing.T) {
	dir := t.TempDir()

	_, err := build(context.Background(),
		&globals{repo: dir, confPath: filepath.Join(dir, "absent.json"), env: envOf(nil)}, ui.Noop{})
	if err == nil {
		t.Fatal("build worked outside a repository")
	}
	if got := ExitCode(err); got != ExitRepo {
		t.Errorf("ExitCode(%v) = %d, want %d", err, got, ExitRepo)
	}
}

func TestDoctorReportsABrokenPreset(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, ".autogit.json",
		`{"presets":{"conventional":{"commit":{"body":{"mode":"sometimes"}}}}}`)

	var stdout, stderr bytes.Buffer
	out := ui.New(&stdout, &stderr, strings.NewReader(""), false)
	g := &globals{repo: dir, confPath: filepath.Join(dir, "absent.json"), env: envOf(nil)}

	err := runDoctor(context.Background(), g, out)
	if err == nil {
		t.Fatal("doctor passed a preset that cannot run")
	}
	report := stdout.String() + stderr.String()
	for _, want := range []string{"repository", dir, "preset       BROKEN"} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q:\n%s", want, report)
		}
	}
}
