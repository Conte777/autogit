package app_test

import (
	"testing"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/preset"
)

func branchFormat(t *testing.T, name string) preset.BranchFormat {
	t.Helper()
	p, ok := preset.Builtin(name)
	if !ok {
		t.Fatalf("no builtin preset %q", name)
	}
	return p.Branch
}

func TestParseBranchArgs(t *testing.T) {
	conventional := branchFormat(t, "conventional")
	ticket := branchFormat(t, "ticket")

	tests := []struct {
		name   string
		args   []string
		format preset.BranchFormat
		want   app.BranchRequest
	}{
		{
			name:   "description shaped like a ticket stays a description",
			args:   []string{"a-1", "fix", "login"},
			format: conventional,
			want:   app.BranchRequest{Description: "a-1 fix login"},
		},
		{
			name:   "ticket matching the preset becomes the prefix",
			args:   []string{"AG-12", "fix", "login"},
			format: conventional,
			want:   app.BranchRequest{Ticket: "AG-12", Description: "fix login"},
		},
		{
			name:   "a lowercase ticket is uppercased",
			args:   []string{"cus-9"},
			format: ticket,
			want:   app.BranchRequest{Ticket: "CUS-9"},
		},
		{
			name:   "a ticket from another preset is description text",
			args:   []string{"AG-12", "fix", "login"},
			format: ticket,
			want:   app.BranchRequest{Description: "AG-12 fix login"},
		},
		{
			name:   "without a pattern any ticket-shaped word is a ticket",
			args:   []string{"a-1", "fix", "login"},
			format: preset.BranchFormat{},
			want:   app.BranchRequest{Ticket: "A-1", Description: "fix login"},
		},
		{
			name:   "a ticket only in part of the word is a description",
			args:   []string{"AG-12-and-more", "fix"},
			format: conventional,
			want:   app.BranchRequest{Description: "AG-12-and-more fix"},
		},
		{
			name:   "no args at all",
			args:   nil,
			format: conventional,
			want:   app.BranchRequest{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := app.ParseBranchArgs(tt.args, tt.format)
			if got != tt.want {
				t.Errorf("ParseBranchArgs(%q) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestStageModeFor(t *testing.T) {
	tests := []struct {
		all, tracked bool
		want         app.StageMode
	}{
		{want: app.StageStaged},
		{all: true, want: app.StageAll},
		{tracked: true, want: app.StageTracked},
		{all: true, tracked: true, want: app.StageAll},
	}
	for _, tt := range tests {
		if got := app.StageModeFor(tt.all, tt.tracked); got != tt.want {
			t.Errorf("StageModeFor(%v, %v) = %q, want %q", tt.all, tt.tracked, got, tt.want)
		}
	}
}

func TestParseStageMode(t *testing.T) {
	tests := map[string]app.StageMode{
		"":         app.StageStaged,
		"staged":   app.StageStaged,
		"all":      app.StageAll,
		"tracked":  app.StageTracked,
		"nonsense": app.StageStaged,
	}
	for in, want := range tests {
		if got := app.ParseStageMode(in); got != want {
			t.Errorf("ParseStageMode(%q) = %q, want %q", in, got, want)
		}
	}
}
