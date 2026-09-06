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
	return connectWith(t, build, nil)
}

func connectWith(t *testing.T, build mcpsrv.Builder, opts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "autogit", Version: "test"}, nil)
	mcpsrv.New(build).Register(server)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, opts)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func protectedMain(t *testing.T, prov *mock.Provider) (string, mcpsrv.Builder) {
	t.Helper()
	dir := repo(t)
	run(t, dir, "branch", "-m", "main")
	return dir, builder(t, prov, func(c *config.Config) {
		c.ProtectedBranches = []string{"main"}
		c.MCP.AllowProtectedBranch = true
	})
}

// asks answers every elicitation with one action and records the questions.
type asks struct {
	action   string
	mu       sync.Mutex
	messages []string
}

func (a *asks) options() *mcp.ClientOptions {
	return &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			a.mu.Lock()
			a.messages = append(a.messages, req.Params.Message)
			a.mu.Unlock()
			return &mcp.ElicitResult{Action: a.action}, nil
		},
	}
}

func (a *asks) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.messages)
}

func stage(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
}

func TestCommitToolAsksTheUserOnAProtectedBranch(t *testing.T) {
	prov := &mock.Provider{Replies: []string{"feat: add the greeting file"}}
	dir, build := protectedMain(t, prov)
	answers := &asks{action: "accept"}
	s := connectWith(t, build, answers.options())

	result := call(t, s, "commit", map[string]any{"repoPath": dir})
	if result.IsError {
		t.Fatalf("consent did not let the commit through: %s", text(t, result))
	}
	if answers.count() != 1 {
		t.Fatalf("the user was asked %d times, want 1", answers.count())
	}
	if !strings.Contains(answers.messages[0], "main") {
		t.Errorf("question = %q, want the branch named", answers.messages[0])
	}
	if got := strings.TrimSpace(run(t, dir, "log", "-1", "--format=%s")); got != "feat: add the greeting file" {
		t.Errorf("git log says %q", got)
	}
}

func TestCommitToolRefusalCommitsNothing(t *testing.T) {
	prov := &mock.Provider{Replies: []string{"feat: add the greeting file"}}
	dir, build := protectedMain(t, prov)
	s := connectWith(t, build, (&asks{action: "decline"}).options())

	result := call(t, s, "commit", map[string]any{"repoPath": dir})
	if !result.IsError {
		t.Fatal("a refusal still committed")
	}
	if !strings.Contains(text(t, result), "did not consent") {
		t.Errorf("result = %q", text(t, result))
	}
	if cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD"); cmd.Run() == nil {
		t.Error("a refusal still created a commit")
	}
}

func TestCommitToolDismissalCommitsNothing(t *testing.T) {
	prov := &mock.Provider{Replies: []string{"feat: add the greeting file"}}
	dir, build := protectedMain(t, prov)
	s := connectWith(t, build, (&asks{action: "cancel"}).options())

	result := call(t, s, "commit", map[string]any{"repoPath": dir})
	if !result.IsError {
		t.Fatal("a dismissed question still committed")
	}
	if !strings.Contains(text(t, result), "did not consent") {
		t.Errorf("result = %q", text(t, result))
	}
}

// TestConsentAnsweredAboutAnotherBranchIsNotSpent moves HEAD to a second
// protected branch while the question about the first is open. The lock is not
// held across the wait, so this is reachable, and the answer must not carry.
func TestConsentAnsweredAboutAnotherBranchIsNotSpent(t *testing.T) {
	dir := repo(t)
	run(t, dir, "branch", "-m", "main")
	prov := &mock.Provider{Replies: []string{"feat: add the greeting file"}}
	build := builder(t, prov, func(c *config.Config) {
		c.ProtectedBranches = []string{"main", "release/*"}
		c.MCP.AllowProtectedBranch = true
	})

	var asked []string
	s := connectWith(t, build, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			asked = append(asked, req.Params.Message)
			if len(asked) == 1 {
				run(t, dir, "switch", "-c", "release/1.2")
			}
			return &mcp.ElicitResult{Action: "accept"}, nil
		},
	})

	result := call(t, s, "commit", map[string]any{"repoPath": dir})
	if result.IsError {
		t.Fatalf("commit failed: %s", text(t, result))
	}
	if len(asked) != 2 {
		t.Fatalf("the user was asked %d times, want 2: an answer about main cannot pay for release/1.2\n%v",
			len(asked), asked)
	}
	if !strings.Contains(asked[0], "main") || !strings.Contains(asked[1], "release/1.2") {
		t.Errorf("questions = %v, want one per branch", asked)
	}
}

func TestConsentIsNotAskedTwiceOnTheSameBranch(t *testing.T) {
	prov := &mock.Provider{Replies: []string{"feat: add the first file", "feat: add the second file"}}
	dir, build := protectedMain(t, prov)
	answers := &asks{action: "accept"}
	s := connectWith(t, build, answers.options())

	if result := call(t, s, "commit", map[string]any{"repoPath": dir}); result.IsError {
		t.Fatalf("first commit failed: %s", text(t, result))
	}
	stage(t, dir, "b.txt", "two\n")
	if result := call(t, s, "commit", map[string]any{"repoPath": dir}); result.IsError {
		t.Fatalf("second commit failed: %s", text(t, result))
	}
	if answers.count() != 1 {
		t.Errorf("the user was asked %d times, want 1 for one episode on main", answers.count())
	}
}

func TestConsentExpiresWhenTheWorkMovesToAnotherBranch(t *testing.T) {
	prov := &mock.Provider{Replies: []string{
		"feat: add the first file", "feat: add the second file", "feat: add the third file",
	}}
	dir, build := protectedMain(t, prov)
	answers := &asks{action: "accept"}
	s := connectWith(t, build, answers.options())

	if result := call(t, s, "commit", map[string]any{"repoPath": dir}); result.IsError {
		t.Fatalf("commit on main failed: %s", text(t, result))
	}

	run(t, dir, "switch", "-c", "feature")
	stage(t, dir, "b.txt", "two\n")
	if result := call(t, s, "commit", map[string]any{"repoPath": dir}); result.IsError {
		t.Fatalf("commit on feature failed: %s", text(t, result))
	}

	run(t, dir, "switch", "main")
	stage(t, dir, "c.txt", "three\n")
	if result := call(t, s, "commit", map[string]any{"repoPath": dir}); result.IsError {
		t.Fatalf("second commit on main failed: %s", text(t, result))
	}

	if answers.count() != 2 {
		t.Errorf("the user was asked %d times, want 2: leaving main expires the consent", answers.count())
	}
}

func TestConsentIsNeverAskedForWhileTheConfigForbidsIt(t *testing.T) {
	dir := repo(t)
	run(t, dir, "branch", "-m", "main")
	prov := &mock.Provider{Replies: []string{"feat: add the greeting file"}}
	answers := &asks{action: "accept"}
	s := connectWith(t, builder(t, prov, func(c *config.Config) {
		c.ProtectedBranches = []string{"main"}
	}), answers.options())

	result := call(t, s, "commit", map[string]any{"repoPath": dir})
	if !result.IsError {
		t.Fatal("the model committed on main while mcp.allowProtectedBranch was off")
	}
	if answers.count() != 0 {
		t.Errorf("the user was asked anyway: %v", answers.messages)
	}
}

func TestCommitToolSaysWhoCanAllowItWhenTheClientCannotAsk(t *testing.T) {
	prov := &mock.Provider{Replies: []string{"feat: add the greeting file"}}
	dir, build := protectedMain(t, prov)
	s := connect(t, build)

	result := call(t, s, "commit", map[string]any{"repoPath": dir})
	if !result.IsError {
		t.Fatal("a client that cannot ask the user still committed on main")
	}
	if !strings.Contains(text(t, result), "--force") {
		t.Errorf("result = %q, want the human path spelled out", text(t, result))
	}
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
		return app.New(r, &cfg, prov, ui.Noop{}, ui.Noop{})
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
	testSerialisation(t, dir, dir)
}

func TestConcurrentCommitsSpellingTheRepoDifferentlyAreSerialised(t *testing.T) {
	dir := repo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	testSerialisation(t, dir, sub)
}

func testSerialisation(t *testing.T, dir, secondSpelling string) {
	t.Helper()

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
	for _, path := range []string{dir, secondSpelling} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call(t, s, "commit", map[string]any{"repoPath": path})
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
