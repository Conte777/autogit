package app

import (
	"testing"

	"github.com/Conte777/autogit/internal/git"
)

func TestCommitResultSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result CommitResult
		style  SummaryStyle
		want   string
	}{
		{
			name:   "human keeps the subject only",
			result: CommitResult{Message: "feat: add thing\n\nbody line", ShortHash: "abc1234"},
			style:  SummaryHuman,
			want:   "committed abc1234: feat: add thing",
		},
		{
			name:   "agent carries the whole message",
			result: CommitResult{Message: "feat: add thing\n\nbody line", ShortHash: "abc1234"},
			style:  SummaryAgent,
			want:   "committed abc1234\n\nfeat: add thing\n\nbody line",
		},
		{
			name:   "human preview is the bare message",
			result: CommitResult{Message: "feat: add thing", Preview: true, Prepared: git.OpMerge},
			style:  SummaryHuman,
			want:   "feat: add thing",
		},
		{
			name:   "agent preview labels a prepared message",
			result: CommitResult{Message: "Merge branch 'x'", Preview: true, Prepared: git.OpMerge},
			style:  SummaryAgent,
			want:   "Merge branch 'x'\n\n(git's own merge message; it would be used verbatim, not generated)",
		},
		{
			name:   "human notes a prepared message inline",
			result: CommitResult{Message: "Merge branch 'x'", ShortHash: "abc1234", Prepared: git.OpMerge},
			style:  SummaryHuman,
			want:   "committed abc1234: Merge branch 'x' (git's own merge message)",
		},
		{
			name:   "agent explains that nothing was generated",
			result: CommitResult{Message: "Merge branch 'x'", ShortHash: "abc1234", Prepared: git.OpMerge},
			style:  SummaryAgent,
			want: "committed abc1234\n\nMerge branch 'x'\n\n" +
				"(git's own merge message, committed verbatim: no message was generated)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.result.Summary(tt.style); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBranchResultSummary(t *testing.T) {
	t.Parallel()

	got := BranchResult{Name: "feat/add-thing"}.Summary()
	if want := "switched to new branch feat/add-thing"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}
