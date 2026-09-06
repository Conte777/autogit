package autogit_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/Conte777/autogit/internal/hook"
)

const repoRoot = "../.."

type marketplace struct {
	Name    string `json:"name"`
	Plugins []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	} `json:"plugins"`
}

type manifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type hookGroup struct {
	Matcher string      `json:"matcher"`
	Hooks   []hookEntry `json:"hooks"`
}

type hookFile struct {
	Hooks map[string][]hookGroup `json:"hooks"`
}

type mcpServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

type mcpFile struct {
	Servers map[string]mcpServer `json:"mcpServers"`
}

func readJSON[T any](t *testing.T, path string) T {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return v
}

func TestMarketplacePointsAtThePlugin(t *testing.T) {
	m := readJSON[marketplace](t, filepath.Join(repoRoot, ".claude-plugin", "marketplace.json"))

	if m.Name != "autogit" {
		t.Errorf("marketplace name = %q, want %q", m.Name, "autogit")
	}
	if len(m.Plugins) != 1 {
		t.Fatalf("marketplace lists %d plugins, want 1", len(m.Plugins))
	}
	entry := m.Plugins[0]
	if entry.Name != "autogit" {
		t.Errorf("plugin name = %q, want %q: commands publish as /<name>:<command>", entry.Name, "autogit")
	}

	source := filepath.Join(repoRoot, filepath.FromSlash(entry.Source))
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		t.Fatalf("source %q does not resolve to a directory: %v", entry.Source, err)
	}
	if _, err := os.Stat(filepath.Join(source, ".claude-plugin", "plugin.json")); err != nil {
		t.Errorf("source %q holds no plugin manifest: %v", entry.Source, err)
	}
}

func TestPluginManifest(t *testing.T) {
	p := readJSON[manifest](t, filepath.Join(".claude-plugin", "plugin.json"))

	if p.Name != "autogit" {
		t.Errorf("plugin name = %q, want %q", p.Name, "autogit")
	}
	if p.Version == "" {
		t.Error("plugin version is empty")
	}
}

// The release workflow refuses a tag the manifest disagrees with, but by then
// the tag is public and the only way out is moving it. A forgotten bump is
// caught here instead, on the pull request that forgot it.
func TestPluginVersionIsNotBehindTheNewestTag(t *testing.T) {
	newest := newestTag(t)
	if newest == "" {
		t.Skip("no tags reachable; a shallow clone cannot know what shipped")
	}
	p := readJSON[manifest](t, filepath.Join(".claude-plugin", "plugin.json"))

	got, err := semver(p.Version)
	if err != nil {
		t.Fatalf("plugin version %q: %v", p.Version, err)
	}
	want, err := semver(strings.TrimPrefix(newest, "v"))
	if err != nil {
		t.Skipf("newest tag %q is not a plain version", newest)
	}
	if slices.Compare(got, want) < 0 {
		t.Errorf("plugin version %s is behind tag %s: bump "+
			"plugins/autogit/.claude-plugin/plugin.json in the commit that ships under the next tag",
			p.Version, newest)
	}
}

func newestTag(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// semver splits a plain major.minor.patch. A prerelease or build suffix has no
// obvious answer here — the marketplace would serve it to everyone — so it is
// rejected rather than guessed at.
func semver(s string) ([]int, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("want major.minor.patch, got %d parts", len(parts))
	}
	out := make([]int, 3)
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%q is not a version number", part)
		}
		out[i] = n
	}
	return out, nil
}

func TestCommandSetMatchesTheHook(t *testing.T) {
	entries, err := os.ReadDir("commands")
	if err != nil {
		t.Fatalf("read commands: %v", err)
	}

	var shipped []hook.Kind
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok {
			t.Errorf("commands/%s is not a flat markdown stub", e.Name())
			continue
		}
		shipped = append(shipped, hook.Kind(name))
	}

	want := hook.Kinds()
	slices.Sort(shipped)
	slices.Sort(want)
	if diff := cmp.Diff(want, shipped); diff != "" {
		t.Errorf("shipped commands differ from the kinds the hook accepts (-want +got):\n%s", diff)
	}
}

func TestEachStubIsHookOnly(t *testing.T) {
	for _, kind := range hook.Kinds() {
		body, err := os.ReadFile(filepath.Join("commands", string(kind)+".md"))
		if err != nil {
			t.Fatalf("read stub for %s: %v", kind, err)
		}
		cmd, ok := hook.Parse("/autogit:" + string(kind))
		if !ok || cmd.Kind != kind {
			t.Errorf("hook does not answer /autogit:%s", kind)
		}
		if !strings.Contains(string(body), "disable-model-invocation: true") {
			t.Errorf("commands/%s.md lets the model answer the command itself instead of the hook", kind)
		}
		if !strings.Contains(string(body), "did not run") {
			t.Errorf("commands/%s.md drops the explanation the model reads when the hook is absent", kind)
		}
	}
}

func TestHooksRunTheBinaryFromPath(t *testing.T) {
	h := readJSON[hookFile](t, filepath.Join("hooks", "hooks.json"))

	groups := h.Hooks["UserPromptSubmit"]
	if len(groups) != 1 || len(groups[0].Hooks) != 1 {
		t.Fatalf("UserPromptSubmit declares %d groups, want exactly one entry", len(groups))
	}
	entry := groups[0].Hooks[0]
	if entry.Type != "command" {
		t.Errorf("UserPromptSubmit type = %q, want %q", entry.Type, "command")
	}
	if entry.Command != "autogit hook" {
		t.Errorf("UserPromptSubmit command = %q, want %q: the binary comes from PATH", entry.Command, "autogit hook")
	}
	if entry.Timeout == 0 {
		t.Error("UserPromptSubmit has no timeout; a generation outlives the default")
	}

	start := h.Hooks["SessionStart"]
	if len(start) != 1 || len(start[0].Hooks) != 1 {
		t.Fatalf("SessionStart declares %d groups, want exactly one entry", len(start))
	}
	if start[0].Matcher != "startup" {
		t.Errorf("SessionStart matcher = %q, want %q: resume and clear would repeat the warning",
			start[0].Matcher, "startup")
	}
	check := start[0].Hooks[0].Command
	if !strings.Contains(check, "command -v autogit") {
		t.Errorf("SessionStart does not probe PATH for autogit: %q", check)
	}
	if !strings.Contains(check, "brew install Conte777/tap/autogit") {
		t.Errorf("SessionStart does not name the install command: %q", check)
	}
	if !strings.Contains(check, `"hookEventName":"SessionStart"`) {
		t.Errorf("SessionStart payload is not tagged with its event, so Claude Code reads it as raw context: %q", check)
	}
}

func TestNoLauncherScript(t *testing.T) {
	if _, err := os.Stat("bin"); err == nil {
		t.Error("plugins/autogit/bin exists; the plugin ships no executables")
	}
	data, err := os.ReadFile(filepath.Join("hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	if strings.Contains(string(data), "CLAUDE_PLUGIN_ROOT") {
		t.Error("hooks.json reaches into the plugin root; autogit is found on PATH")
	}
}

func TestMCPServerRunsTheBinaryFromPath(t *testing.T) {
	m := readJSON[mcpFile](t, ".mcp.json")

	if len(m.Servers) != 1 {
		t.Fatalf(".mcp.json declares %d servers, want 1", len(m.Servers))
	}
	server, ok := m.Servers["autogit"]
	if !ok {
		t.Fatalf(".mcp.json declares %v, want a server named %q", slices.Collect(maps.Keys(m.Servers)), "autogit")
	}
	if server.Type != "stdio" {
		t.Errorf("server type = %q, want %q", server.Type, "stdio")
	}
	if server.Command != "autogit" {
		t.Errorf("server command = %q, want %q: the binary comes from PATH", server.Command, "autogit")
	}
	if diff := cmp.Diff([]string{"mcp"}, server.Args); diff != "" {
		t.Errorf("server args differ (-want +got):\n%s", diff)
	}
	if len(server.Env) != 0 {
		t.Errorf("server carries env %v; API keys reach autogit from the user's own environment", server.Env)
	}

	data, err := os.ReadFile(".mcp.json")
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	if strings.Contains(string(data), "CLAUDE_PLUGIN_ROOT") {
		t.Error(".mcp.json reaches into the plugin root; autogit is found on PATH")
	}
}
