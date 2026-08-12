package git

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestStagedDiffFull(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	write(t, dir, "sub/b.txt", "two\n")
	runGit(t, dir, "add", ".")

	d, err := open(t, dir).StagedDiff(ctx, DiffOptions{MaxBytes: 40000, Context: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 2 {
		t.Fatalf("Files = %v, want 2", d.Files)
	}
	if d.Truncated {
		t.Error("Truncated = true for a tiny diff")
	}
	if !strings.Contains(d.Text, "+one") || !strings.Contains(d.Text, "+two") {
		t.Errorf("diff text missing content:\n%s", d.Text)
	}
}

func TestStagedDiffOnEmptyRepoWorks(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")

	d, err := open(t, dir).StagedDiff(ctx, DiffOptions{MaxBytes: 40000})
	if err != nil {
		t.Fatalf("StagedDiff in a repo with no commits: %v", err)
	}
	if len(d.Files) != 1 || d.Text == "" {
		t.Fatalf("StagedDiff = %+v, want one file with a body", d)
	}
}

func TestWorktreeDiffOnEmptyRepoUsesSentinel(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", ".")

	d, err := open(t, dir).WorktreeDiff(ctx, DiffOptions{MaxBytes: 40000})
	if err != nil {
		t.Fatalf("WorktreeDiff in a repo with no commits: %v", err)
	}
	if len(d.Files) != 1 {
		t.Fatalf("Files = %v, want a.txt", d.Files)
	}
}

func TestStagedDiffExcludePathspecsKeepFileList(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "a.txt", "code\n")
	write(t, dir, "go.sum", "lockfile noise\n")
	runGit(t, dir, "add", ".")

	d, err := open(t, dir).StagedDiff(ctx, DiffOptions{
		MaxBytes:         40000,
		ExcludePathspecs: []string{":(exclude)go.sum"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 2 {
		t.Errorf("Files = %v, want the excluded file still listed", d.Files)
	}
	if strings.Contains(d.Text, "lockfile noise") {
		t.Error("excluded pathspec leaked into the diff body")
	}
}

func TestStagedDiffLadderKeepsFullFileListAndWholeHunks(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	const files = 200
	for i := range files {
		write(t, dir, fmt.Sprintf("f%03d.txt", i), strings.Repeat(fmt.Sprintf("line %d\n", i), 60))
	}
	runGit(t, dir, "add", ".")

	const maxBytes = 40000
	d, err := open(t, dir).StagedDiff(ctx, DiffOptions{MaxBytes: maxBytes, Context: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != files {
		t.Fatalf("Files = %d entries, want the complete list of %d", len(d.Files), files)
	}
	if !d.Truncated {
		t.Fatal("Truncated = false for a diff far over the budget")
	}
	if len(d.Text) > maxBytes {
		t.Errorf("diff text is %d bytes, over the %d budget", len(d.Text), maxBytes)
	}
	if !strings.Contains(d.Text, "f000.txt |") {
		t.Error("--stat block missing: the model would not see which files changed")
	}
	assertWholeSections(t, d.Text)
}

// assertWholeSections checks the tail is not a hunk cut in half: every `diff
// --git` section that made it in must carry its complete header.
func assertWholeSections(t *testing.T, text string) {
	t.Helper()
	body := text
	if i := strings.Index(body, "diff --git "); i >= 0 {
		body = body[i:]
	} else {
		return
	}
	for _, section := range splitDiffSections(body) {
		lines := strings.Split(section, "\n")
		if len(lines) < 4 {
			t.Errorf("section cut short:\n%s", section)
			continue
		}
		if !strings.HasPrefix(lines[0], "diff --git ") {
			t.Errorf("section does not start with its header:\n%s", lines[0])
		}
		if !strings.Contains(section, "@@") {
			t.Errorf("section has no hunk header:\n%s", section)
		}
	}
	if strings.HasSuffix(body, "@@") {
		t.Error("diff ends on a bare hunk header")
	}
}

func TestSplitDiffSections(t *testing.T) {
	body := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+diff --git a/fake b/fake\n" +
		"diff --git a/y b/y\n--- a/y\n+++ b/y\n@@ -1 +1 @@\n-b\n+c\n"

	got := splitDiffSections(body)
	if len(got) != 2 {
		t.Fatalf("splitDiffSections = %d sections, want 2 (a `+diff --git` line is content)", len(got))
	}
	if !strings.HasPrefix(got[1], "diff --git a/y b/y") {
		t.Errorf("second section = %q", got[1])
	}
}

func TestShrinkFallsBackToStatOnly(t *testing.T) {
	stat := " a.txt | 400 ++++\n 1 file changed\n"
	body := "diff --git a/a.txt b/a.txt\n" + strings.Repeat("+x\n", 5000)

	got := shrink(body, stat, 200)
	if strings.Contains(got, "diff --git") {
		t.Errorf("shrink kept a file body that does not fit:\n%s", got)
	}
	if !strings.Contains(got, "a.txt") {
		t.Errorf("shrink dropped the --stat block:\n%s", got)
	}
}
