package hook_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/Conte777/autogit/internal/hook"
)

func TestParseBothGrammars(t *testing.T) {
	tests := []struct {
		prompt string
		want   hook.Command
		ok     bool
	}{
		{prompt: "/commit", want: hook.Command{Kind: hook.KindCommit}, ok: true},
		{prompt: "  /commit  ", want: hook.Command{Kind: hook.KindCommit}, ok: true},
		{
			prompt: "/commit all force",
			want:   hook.Command{Kind: hook.KindCommit, All: true, Force: true},
			ok:     true,
		},
		{
			prompt: "/commit --all --force",
			want:   hook.Command{Kind: hook.KindCommit, All: true, Force: true},
			ok:     true,
		},
		{
			prompt: "/commit tracked --dry-run",
			want:   hook.Command{Kind: hook.KindCommit, Tracked: true, DryRun: true},
			ok:     true,
		},
		{prompt: "/commit-msg", want: hook.Command{Kind: hook.KindCommitMsg}, ok: true},
		{
			prompt: "/branch CUS-1234 add user auth",
			want:   hook.Command{Kind: hook.KindBranch, Args: []string{"CUS-1234", "add", "user", "auth"}},
			ok:     true,
		},
		{
			prompt: "/branch add user auth",
			want:   hook.Command{Kind: hook.KindBranch, Args: []string{"add", "user", "auth"}},
			ok:     true,
		},
		{
			prompt: "/branch cus-9",
			want:   hook.Command{Kind: hook.KindBranch, Args: []string{"cus-9"}},
			ok:     true,
		},
		{prompt: "/autogit:commit", want: hook.Command{Kind: hook.KindCommit}, ok: true},
		{prompt: "  /autogit:commit  ", want: hook.Command{Kind: hook.KindCommit}, ok: true},
		{
			prompt: "/autogit:commit all force",
			want:   hook.Command{Kind: hook.KindCommit, All: true, Force: true},
			ok:     true,
		},
		{
			prompt: "/autogit:commit --all --force",
			want:   hook.Command{Kind: hook.KindCommit, All: true, Force: true},
			ok:     true,
		},
		{
			prompt: "/autogit:commit tracked --dry-run",
			want:   hook.Command{Kind: hook.KindCommit, Tracked: true, DryRun: true},
			ok:     true,
		},
		{prompt: "/autogit:commit-msg", want: hook.Command{Kind: hook.KindCommitMsg}, ok: true},
		{
			prompt: "/autogit:branch CUS-1234 add user auth",
			want:   hook.Command{Kind: hook.KindBranch, Args: []string{"CUS-1234", "add", "user", "auth"}},
			ok:     true,
		},
		{
			prompt: "/autogit:branch cus-9",
			want:   hook.Command{Kind: hook.KindBranch, Args: []string{"cus-9"}},
			ok:     true,
		},
		{prompt: "/other:commit"},
		{prompt: "/other:branch add auth"},
		{prompt: "/autogit:commitment issues"},
		{prompt: "/autogit:"},
		{prompt: "/autogit:autogit:commit"},
		{prompt: "please /autogit:commit for me"},
		{prompt: "/commitment issues"},
		{prompt: "please /commit for me"},
		{prompt: "how do I commit?"},
		{prompt: ""},
	}

	for _, tt := range tests {
		t.Run(tt.prompt, func(t *testing.T) {
			got, ok := hook.Parse(tt.prompt)
			if ok != tt.ok {
				t.Fatalf("Parse(%q) matched = %v, want %v", tt.prompt, ok, tt.ok)
			}
			if !ok {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Parse(%q) mismatch (-want +got):\n%s", tt.prompt, diff)
			}
		})
	}
}

// commitMsgBeatsCommit guards the ordering: /commit is a prefix of /commit-msg.
func TestCommitMsgIsNotParsedAsCommit(t *testing.T) {
	for _, prompt := range []string{"/commit-msg", "/autogit:commit-msg"} {
		got, ok := hook.Parse(prompt)
		if !ok || got.Kind != hook.KindCommitMsg {
			t.Fatalf("Parse(%s) = %+v, %v", prompt, got, ok)
		}
	}
}

func envOf(kv map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

func TestRunBlocksOnMatch(t *testing.T) {
	var out strings.Builder
	called := false

	err := hook.Run(context.Background(),
		strings.NewReader(`{"prompt":"/commit all"}`), &out, envOf(nil),
		func(_ context.Context, c hook.Command) (string, error) {
			called = true
			if !c.All {
				t.Errorf("command = %+v, want All", c)
			}
			return "committed abc1234: feat: add thing", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("the runner was never invoked")
	}

	var decision struct {
		Decision      string `json:"decision"`
		Reason        string `json:"reason"`
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal([]byte(out.String()), &decision); err != nil {
		t.Fatalf("output is not the decision JSON: %v\n%s", err, out.String())
	}
	if decision.Decision != "block" {
		t.Errorf("decision = %q, want block", decision.Decision)
	}
	if !strings.Contains(decision.Reason, "committed abc1234") {
		t.Errorf("reason = %q", decision.Reason)
	}
	if decision.SystemMessage == "" {
		t.Error("systemMessage is empty; the user would see nothing")
	}
}

func TestRunReportsFailuresToTheUser(t *testing.T) {
	var out strings.Builder
	err := hook.Run(context.Background(),
		strings.NewReader(`{"prompt":"/commit"}`), &out, envOf(nil),
		func(context.Context, hook.Command) (string, error) {
			return "", errors.New("nothing staged")
		})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing staged") {
		t.Errorf("the failure never reached the user:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "block") {
		t.Errorf("a failed run must still block the prompt:\n%s", out.String())
	}
}

func TestRunIgnoresNonTriggerPromptsWithoutTouchingAnything(t *testing.T) {
	var out strings.Builder
	err := hook.Run(context.Background(),
		strings.NewReader(`{"prompt":"what does this repo do?"}`), &out, envOf(nil),
		func(context.Context, hook.Command) (string, error) {
			t.Fatal("the runner ran for a prompt that is not a slash command")
			return "", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "" {
		t.Errorf("a non-trigger prompt produced output:\n%s", out.String())
	}
}

func TestRunExitsImmediatelyWhenAlreadyActive(t *testing.T) {
	var out strings.Builder
	err := hook.Run(context.Background(),
		strings.NewReader(`{"prompt":"/commit"}`), &out,
		envOf(map[string]string{hook.ActiveEnv: "1"}),
		func(context.Context, hook.Command) (string, error) {
			t.Fatal("the hook re-entered itself")
			return "", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "" {
		t.Errorf("output while guarded:\n%s", out.String())
	}
}

func TestRunSurvivesGarbageOnStdin(t *testing.T) {
	var out strings.Builder
	if err := hook.Run(context.Background(), strings.NewReader("not json"), &out, envOf(nil),
		func(context.Context, hook.Command) (string, error) {
			t.Fatal("the runner ran on unparsable input")
			return "", nil
		}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "" {
		t.Errorf("output on garbage input:\n%s", out.String())
	}
}

func TestRunAppliesItsOwnBudget(t *testing.T) {
	var out strings.Builder
	err := hook.Run(context.Background(),
		strings.NewReader(`{"prompt":"/commit"}`), &out, envOf(nil),
		func(ctx context.Context, _ hook.Command) (string, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("no deadline; a killed hook shows the user nothing at all")
			}
			if remaining := time.Until(deadline); remaining > hook.Budget {
				t.Errorf("deadline is %s away, want at most %s", remaining, hook.Budget)
			}
			return "ok", nil
		})
	if err != nil {
		t.Fatal(err)
	}
}
