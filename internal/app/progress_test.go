package app_test

import (
	"context"
	"testing"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/ui"
)

const (
	commitLabel = "Generating commit message…"
	branchLabel = "Generating branch name…"
)

func TestCommitReportsThatItIsGenerating(t *testing.T) {
	e := newEnv(t, "feat: add the greeting file")
	rec := &reports{}
	e.progress = rec
	e.write("hello.txt", "hi\n")
	e.git("add", ".")

	if _, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged}); err != nil {
		t.Fatal(err)
	}
	if got := rec.labels(); len(got) != 1 || got[0] != commitLabel {
		t.Errorf("labels = %v, want exactly %q", got, commitLabel)
	}
	if rec.live() != 0 {
		t.Error("the report outlived the run")
	}
}

// The passthrough path never reaches the model, so a phrase about generating
// one would be a lie.
func TestPassthroughReportsNothing(t *testing.T) {
	e := newEnv(t)
	rec := &reports{}
	e.progress = rec
	e.diverge()
	e.git("merge", "--squash", "extra")

	if _, err := e.app().Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged}); err != nil {
		t.Fatal(err)
	}
	if got := rec.labels(); len(got) != 0 {
		t.Errorf("labels = %v, want none", got)
	}
}

// The reason messageFor is its own method: a report still running under
// confirmCommit would have the question drawn over a live spinner.
func TestTheReportIsDownBeforeTheQuestion(t *testing.T) {
	e := newEnv(t, "feat: add the greeting file")
	rec := &reports{}
	e.progress = rec
	e.cfg.Confirm = true
	e.write("hello.txt", "hi\n")
	e.git("add", ".")

	asked := false
	a := e.answering(watcher{rec: rec, t: t, asked: &asked})
	if _, err := a.Commit(context.Background(), app.CommitRequest{Stage: app.StageStaged}); err != nil {
		t.Fatal(err)
	}
	if !asked {
		t.Fatal("the confirmation never ran, so the test proved nothing")
	}
}

// watcher fails the moment a question is asked while a report is still up.
type watcher struct {
	rec   *reports
	t     *testing.T
	asked *bool
}

func (w watcher) Confirm(string, bool) (bool, error) {
	w.t.Helper()
	*w.asked = true
	if w.rec.live() != 0 {
		w.t.Error("the question was asked over a live report")
	}
	return true, nil
}

func (w watcher) Choose(string, []ui.Option) (string, error) { return "", ui.ErrNoInput }
func (watcher) Interactive() bool                            { return true }

// A branch run with nothing to describe never reaches the model, so a static
// line claiming otherwise would be the only thing the run ever said.
func TestBranchWithNothingToDescribeReportsNothing(t *testing.T) {
	e := newEnv(t)
	rec := &reports{}
	e.progress = rec
	e.commitFile("a.txt", "one\n", "init")

	if _, err := e.app().Branch(context.Background(), app.BranchRequest{}); err == nil {
		t.Fatal("a clean worktree produced a branch name")
	}
	if got := rec.labels(); len(got) != 0 {
		t.Errorf("labels = %v, want none", got)
	}
}

func TestBranchReportsOnBothGeneratingPaths(t *testing.T) {
	tests := []struct {
		name string
		req  app.BranchRequest
		want []string
	}{
		{
			name: "from the diff",
			req:  app.BranchRequest{},
			want: []string{branchLabel},
		},
		{
			name: "from a description",
			req:  app.BranchRequest{Description: "fix the login redirect"},
			want: []string{branchLabel},
		},
		{
			name: "from a description and a ticket",
			req:  app.BranchRequest{Ticket: "CUS-1234", Description: "fix the login redirect"},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, "feat add-user-auth")
			e.cfg.Preset = "ticket"
			rec := &reports{}
			e.progress = rec
			e.commitFile("a.txt", "one\n", "init")
			e.write("auth.go", "package auth\n")
			e.git("add", ".")

			if _, err := e.app().Branch(context.Background(), tt.req); err != nil {
				t.Fatal(err)
			}
			got := rec.labels()
			if len(got) != len(tt.want) {
				t.Fatalf("labels = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("labels[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
			if rec.live() != 0 {
				t.Error("the report outlived the run")
			}
		})
	}
}
