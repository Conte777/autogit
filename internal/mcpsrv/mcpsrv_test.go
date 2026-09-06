package mcpsrv_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/mcpsrv"
	"github.com/Conte777/autogit/internal/provider/mock"
	"github.com/Conte777/autogit/internal/ui"
)

// connect wires a real client to a real server over in-memory transports, so
// the tests exercise the protocol rather than the handler functions.
func connect(t *testing.T, build mcpsrv.Builder) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "autogit", Version: "test"}, nil)
	mcpsrv.New(build).Register(server)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func call(t *testing.T, s *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: json.RawMessage(raw),
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result
}

func text(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// repo builds a working tree with something staged.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "work"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		run(t, dir, args...)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	return dir
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func builder(t *testing.T, prov *mock.Provider, tweak func(*config.Config)) mcpsrv.Builder {
	t.Helper()
	return func(ctx context.Context, repoPath string) (*app.App, error) {
		r, err := git.Open(ctx, repoPath, git.Options{})
		if err != nil {
			return nil, err
		}
		cfg := config.Default()
		cfg.ProtectedBranches = nil
		if tweak != nil {
			tweak(&cfg)
		}
		return app.New(r, &cfg, prov, ui.Noop{})
	}
}

func TestCommitTool(t *testing.T) {
	dir := repo(t)
	prov := &mock.Provider{Replies: []string{"feat: add the greeting file"}}
	s := connect(t, builder(t, prov, nil))

	result := call(t, s, "commit", map[string]any{"repoPath": dir})
	if result.IsError {
		t.Fatalf("commit failed: %s", text(t, result))
	}
	if !strings.Contains(text(t, result), "feat: add the greeting file") {
		t.Errorf("result = %q", text(t, result))
	}
	if got := strings.TrimSpace(run(t, dir, "log", "-1", "--format=%s")); got != "feat: add the greeting file" {
		t.Errorf("git log says %q", got)
	}
}

func TestCommitToolHasNoProtectedBranchEscape(t *testing.T) {
	dir := repo(t)
	run(t, dir, "branch", "-m", "main")

	prov := &mock.Provider{Replies: []string{"feat: add the greeting file"}}
	s := connect(t, builder(t, prov, func(c *config.Config) {
		c.ProtectedBranches = []string{"main"}
	}))

	result := call(t, s, "commit", map[string]any{"repoPath": dir})
	if !result.IsError {
		t.Fatal("the model committed on a protected branch")
	}
	if !strings.Contains(text(t, result), "protected") {
		t.Errorf("result = %q", text(t, result))
	}

	// The parameter must not exist at all, so the model cannot even try.
	tools, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(schema)), "protected") {
			t.Errorf("tool %s exposes a protected-branch parameter: %s", tool.Name, schema)
		}
	}
}

func TestReplayedCommitDoesNotCommitTwice(t *testing.T) {
	dir := repo(t)
	prov := &mock.Provider{Replies: []string{"feat: add the greeting file", "feat: add it again"}}
	s := connect(t, builder(t, prov, nil))

	if result := call(t, s, "commit", map[string]any{"repoPath": dir}); result.IsError {
		t.Fatalf("first commit failed: %s", text(t, result))
	}
	second := call(t, s, "commit", map[string]any{"repoPath": dir})
	if !second.IsError {
		t.Fatalf("a replayed call created a second commit: %s", text(t, second))
	}
	if got := strings.Count(run(t, dir, "log", "--format=%s"), "\n"); got != 1 {
		t.Errorf("%d commits in the log, want 1", got)
	}
}

func TestConcurrentCommitsOnOneRepoAreSerialised(t *testing.T) {
	dir := repo(t)

	var inFlight, maxInFlight atomic.Int32
	prov := &mock.Provider{
		Replies: []string{"feat: add the greeting file", "feat: add the greeting file"},
		Hook: func(int, string) {
			n := inFlight.Add(1)
			for {
				peak := maxInFlight.Load()
				if n <= peak || maxInFlight.CompareAndSwap(peak, n) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			inFlight.Add(-1)
		},
	}
	s := connect(t, builder(t, prov, nil))

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call(t, s, "commit", map[string]any{"repoPath": dir})
		}()
	}
	wg.Wait()

	if maxInFlight.Load() > 1 {
		t.Errorf("%d handlers ran at once on the same repository", maxInFlight.Load())
	}
	if got := strings.Count(run(t, dir, "log", "--format=%s"), "\n"); got != 1 {
		t.Errorf("%d commits, want 1: the second call should have found an empty index", got)
	}
}

func TestPanicBecomesAToolErrorAndKeepsTheSessionAlive(t *testing.T) {
	dir := repo(t)
	panicked := false
	build := builder(t, &mock.Provider{Replies: []string{"feat: add the greeting file"}}, nil)
	s := connect(t, func(ctx context.Context, repoPath string) (*app.App, error) {
		if !panicked {
			panicked = true
			panic("boom")
		}
		return build(ctx, repoPath)
	})

	result := call(t, s, "commit", map[string]any{"repoPath": dir})
	if !result.IsError {
		t.Fatal("the panic did not become a tool error")
	}
	if !strings.Contains(text(t, result), "internal error") {
		t.Errorf("result = %q", text(t, result))
	}

	// A dead process would fail here; Claude Code never restarts stdio servers.
	if result := call(t, s, "commit", map[string]any{"repoPath": dir}); result.IsError {
		t.Fatalf("the session did not survive the panic: %s", text(t, result))
	}
}

func TestCancellationReachesGit(t *testing.T) {
	dir := repo(t)
	started := make(chan struct{})
	prov := &mock.Provider{
		Replies: []string{"feat: add the greeting file"},
		Hook: func(int, string) {
			close(started)
			time.Sleep(5 * time.Second)
		},
	}
	s := connect(t, builder(t, prov, nil))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.CallTool(ctx, &mcp.CallToolParams{
			Name:      "commit",
			Arguments: json.RawMessage(`{"repoPath":` + quote(dir) + `}`),
		})
	}()

	<-started
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation never reached the handler")
	}
	// The repository has no commits, so a successful commit is the only way
	// HEAD could resolve at all.
	if cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD"); cmd.Run() == nil {
		t.Error("a cancelled call still committed")
	}
	// Cancelling between `git add` and `git commit` leaves the index staged on
	// purpose: it is the same rule as the CLI, nothing rolls the index back.
	if out := run(t, dir, "diff", "--cached", "--name-only"); !strings.Contains(out, "a.txt") {
		t.Error("the staged index was rolled back; it must be left as it was")
	}
}

func TestBranchTool(t *testing.T) {
	dir := repo(t)
	run(t, dir, "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prov := &mock.Provider{Replies: []string{"feat add-user-auth"}}
	s := connect(t, builder(t, prov, nil))

	result := call(t, s, "branch", map[string]any{"repoPath": dir, "description": "add user auth"})
	if result.IsError {
		t.Fatalf("branch failed: %s", text(t, result))
	}
	if got := strings.TrimSpace(run(t, dir, "branch", "--show-current")); got != "feat/add-user-auth" {
		t.Errorf("current branch = %q", got)
	}
}

func TestMissingRepoPathIsAToolError(t *testing.T) {
	s := connect(t, builder(t, &mock.Provider{}, nil))
	result := call(t, s, "commit", map[string]any{})
	if !result.IsError || !strings.Contains(text(t, result), "repoPath") {
		t.Fatalf("result = %+v", result)
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestValidationFailureCarriesTheLastRejectedCandidate(t *testing.T) {
	dir := repo(t)
	prov := &mock.Provider{Replies: []string{"Feat: Add Thing", "Feat: Add Thing", "Feat: Add Thing"}}
	s := connect(t, builder(t, prov, nil))

	result := call(t, s, "commit", map[string]any{"repoPath": dir})
	if !result.IsError {
		t.Fatalf("an invalid message committed: %s", text(t, result))
	}
	if !strings.Contains(text(t, result), "Feat: Add Thing") {
		t.Errorf("result = %q, want the rejected candidate", text(t, result))
	}
}
