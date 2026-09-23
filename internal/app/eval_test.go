//go:build eval

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/provider"
	"github.com/Conte777/autogit/internal/ui"
)

type evalCase struct {
	Line        int    `json:"line"`
	Kind        string `json:"kind"`
	Repo        string `json:"repo"`
	SHA         string `json:"sha"`
	Description string `json:"description,omitempty"`
}

type evalRejection struct {
	Attempt   int      `json:"attempt"`
	Candidate string   `json:"candidate"`
	Problems  []string `json:"problems"`
}

type evalResult struct {
	evalCase
	Provider   string          `json:"provider,omitempty"`
	Branch     string          `json:"branch,omitempty"`
	Attempts   int             `json:"attempts"`
	PassAt1    bool            `json:"passAt1"`
	Pass       bool            `json:"pass"`
	Value      string          `json:"value,omitempty"`
	Rejected   []evalRejection `json:"rejected,omitempty"`
	WallMS     int64           `json:"wallMs"`
	HarnessErr string          `json:"harnessError,omitempty"`
}

type evalSummary struct {
	N          int            `json:"n"`
	PassAt1    float64        `json:"passAt1Pct"`
	PassAtN    float64        `json:"passAtNPct"`
	MedianMS   int64          `json:"medianMs"`
	P90MS      int64          `json:"p90Ms"`
	Problems   map[string]int `json:"problemKinds"`
	HarnessErr int            `json:"harnessErrors"`
}

func TestEvalGeneration(t *testing.T) {
	path := os.Getenv("AUTOGIT_EVAL_CASES")
	if path == "" {
		t.Skip("AUTOGIT_EVAL_CASES is not set")
	}
	cases, err := readEvalCases(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s: no cases", path)
	}
	parallel, err := envInt("AUTOGIT_EVAL_PARALLEL", 4)
	if err != nil || parallel < 1 {
		t.Fatalf("AUTOGIT_EVAL_PARALLEL: want a positive integer (%v)", err)
	}

	results := make([]evalResult, len(cases))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, c := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = runEvalCase(t.Context(), c)
		}()
	}
	wg.Wait()

	for _, r := range results {
		logEvalResult(t, r)
	}
	sum := summarize(results)
	logSummary(t, sum)

	if out := os.Getenv("AUTOGIT_EVAL_OUT"); out != "" {
		data, err := json.MarshalIndent(struct {
			Summary evalSummary  `json:"summary"`
			Cases   []evalResult `json:"cases"`
		}{sum, results}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("results written to %s", out)
	}

	for _, r := range results {
		if r.HarnessErr != "" {
			t.Errorf("%s:%d %s %s: %s", path, r.Line, r.Kind, r.SHA, r.HarnessErr)
		}
	}
	if raw := os.Getenv("AUTOGIT_EVAL_MIN_PASS1"); raw != "" {
		minPass, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			t.Fatalf("AUTOGIT_EVAL_MIN_PASS1: %v", err)
		}
		if sum.PassAt1 < minPass {
			t.Errorf("pass@1 %.1f%% is below AUTOGIT_EVAL_MIN_PASS1 %.1f%%", sum.PassAt1, minPass)
		}
	}
}

func readEvalCases(path string) ([]evalCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var cases []evalCase
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 3 || len(cols) > 4 {
			return nil, fmt.Errorf("%s:%d: want kind<TAB>repo<TAB>sha[<TAB>description], got %d columns", path, n, len(cols))
		}
		c := evalCase{
			Line: n,
			Kind: strings.TrimSpace(cols[0]),
			Repo: strings.TrimSpace(cols[1]),
			SHA:  strings.TrimSpace(cols[2]),
		}
		if len(cols) == 4 {
			c.Description = strings.TrimSpace(cols[3])
		}
		switch {
		case c.Kind != "commit" && c.Kind != "branch":
			return nil, fmt.Errorf("%s:%d: kind %q is neither commit nor branch", path, n, c.Kind)
		case c.Kind == "commit" && c.Description != "":
			return nil, fmt.Errorf("%s:%d: a description belongs to a branch case only", path, n)
		case c.Repo == "" || c.SHA == "":
			return nil, fmt.Errorf("%s:%d: repo and sha are required", path, n)
		}
		c.Repo = expandTilde(c.Repo)
		cases = append(cases, c)
	}
	return cases, sc.Err()
}

func expandTilde(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return home + path[1:]
}

func envInt(name string, def int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return def, nil
	}
	return strconv.Atoi(raw)
}

func runEvalCase(ctx context.Context, c evalCase) evalResult {
	res := evalResult{evalCase: c}
	fail := func(err error) evalResult {
		res.HarnessErr = err.Error()
		return res
	}

	repo, err := git.Open(ctx, c.Repo, git.Options{})
	if err != nil {
		return fail(err)
	}
	sha, err := gitOutput(ctx, repo.Root(), "rev-parse", "--verify", "--quiet", c.SHA+"^{commit}")
	if err != nil {
		return fail(fmt.Errorf("resolve %s: %w", c.SHA, err))
	}
	res.SHA = sha

	cfg, err := config.Load(config.Options{RepoRoot: repo.Root(), Env: os.LookupEnv})
	if err != nil {
		return fail(err)
	}
	prov, err := provider.Build(cfg, os.LookupEnv, nil)
	if err != nil {
		return fail(err)
	}
	res.Provider = prov.Name()
	if model := cfg.Model(); model != "" {
		res.Provider += "/" + model
	}
	a, err := New(repo, cfg, prov, ui.Noop{}, ui.Noop{})
	if err != nil {
		return fail(err)
	}
	attempt := 0
	a.observe = func(candidate string, problems []string) {
		attempt++
		if len(problems) > 0 {
			res.Rejected = append(res.Rejected, evalRejection{Attempt: attempt, Candidate: candidate, Problems: problems})
		}
	}

	changes := func(ctx context.Context, opts git.DiffOptions) (git.Diff, error) {
		return repo.CommitDiff(ctx, sha, opts)
	}

	var value string
	var attempts int
	var genErr error
	start := time.Now()
	switch c.Kind {
	case "commit":
		branch, err := branchAt(ctx, repo, sha)
		if err != nil {
			return fail(err)
		}
		res.Branch = branch.Name
		diff, err := changes(ctx, a.diffOptions())
		if err != nil {
			return fail(err)
		}
		if diff.Empty() {
			return fail(errors.New("the commit changes no files"))
		}
		start = time.Now()
		var out gen.Result
		out, genErr = a.generateMessage(ctx, branch, diff)
		value, attempts = out.Value, out.Attempts
	case "branch":
		var out BranchResult
		out, genErr = a.nameBranch(ctx, a.ParseBranchArgs(strings.Fields(c.Description)), changes)
		value, attempts = out.Name, out.Attempts
	}
	res.WallMS = time.Since(start).Milliseconds()

	var failure *gen.FailureError
	switch {
	case genErr == nil:
		res.Pass, res.Value, res.Attempts = true, value, attempts
		res.PassAt1 = attempts == 1
	case errors.As(genErr, &failure):
		res.Value, res.Attempts = failure.Last, failure.Attempts
	default:
		res.Attempts = attempt
		return fail(genErr)
	}
	return res
}

var mergeSubject = regexp.MustCompile(`^Merge (?:pull request #\d+ from [^/\s]+/(\S+)|(?:remote-tracking )?branch '([^']+)')`)

func branchAt(ctx context.Context, repo *git.Repo, sha string) (git.Branch, error) {
	merges, err := gitOutput(ctx, repo.Root(), "rev-list", "--merges", "--ancestry-path", "--reverse", sha+"..HEAD")
	if err != nil {
		return git.Branch{}, err
	}
	for i, m := range strings.Fields(merges) {
		if i >= 50 {
			break
		}
		if isAncestor(ctx, repo.Root(), sha, m+"^1") {
			continue
		}
		subject, err := gitOutput(ctx, repo.Root(), "log", "-1", "--format=%s", m)
		if err != nil {
			return git.Branch{}, err
		}
		if match := mergeSubject.FindStringSubmatch(subject); match != nil {
			name := match[1] + match[2]
			name = strings.TrimPrefix(name, "origin/")
			return git.Branch{Name: name}, nil
		}
		break
	}
	return repo.Current(ctx)
}

func isAncestor(ctx context.Context, dir, ancestor, rev string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "merge-base", "--is-ancestor", ancestor, rev)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	return cmd.Run() == nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

var (
	quotedText = regexp.MustCompile(`"[^"]*"|` + "`[^`]*`")
	numbers    = regexp.MustCompile(`\d+`)
)

func problemKind(problem string) string {
	if head, _, found := strings.Cut(problem, " one of: "); found {
		problem = head + " one of: …"
	}
	problem = quotedText.ReplaceAllString(problem, "…")
	return numbers.ReplaceAllString(problem, "N")
}

func summarize(results []evalResult) evalSummary {
	sum := evalSummary{Problems: map[string]int{}}
	var times []int64
	var pass1, pass int
	for _, r := range results {
		if r.HarnessErr != "" {
			sum.HarnessErr++
			continue
		}
		sum.N++
		times = append(times, r.WallMS)
		if r.PassAt1 {
			pass1++
		}
		if r.Pass {
			pass++
		}
		for _, rej := range r.Rejected {
			for _, p := range rej.Problems {
				sum.Problems[problemKind(p)]++
			}
		}
	}
	if sum.N > 0 {
		sum.PassAt1 = 100 * float64(pass1) / float64(sum.N)
		sum.PassAtN = 100 * float64(pass) / float64(sum.N)
		slices.Sort(times)
		sum.MedianMS = percentile(times, 50)
		sum.P90MS = percentile(times, 90)
	}
	return sum
}

func percentile(sorted []int64, p int) int64 {
	idx := (p*len(sorted)+99)/100 - 1
	return sorted[max(idx, 0)]
}

func logEvalResult(t *testing.T, r evalResult) {
	t.Helper()
	head := fmt.Sprintf("line %d %s %s", r.Line, r.Kind, shortSHA(r.SHA))
	if r.HarnessErr != "" {
		t.Logf("%s: HARNESS ERROR: %s", head, r.HarnessErr)
		return
	}
	verdict := "FAIL"
	if r.Pass {
		verdict = "pass"
	}
	t.Logf("%s: %s attempts=%d %s %q", head, verdict, r.Attempts, time.Duration(r.WallMS)*time.Millisecond, firstLine(r.Value))
	for _, rej := range r.Rejected {
		t.Logf("    attempt %d rejected %q: %s", rej.Attempt, firstLine(rej.Candidate), strings.Join(rej.Problems, "; "))
	}
}

func logSummary(t *testing.T, sum evalSummary) {
	t.Helper()
	t.Logf("n=%d pass@1=%.1f%% pass@N=%.1f%% median=%s p90=%s harness errors=%d",
		sum.N, sum.PassAt1, sum.PassAtN,
		time.Duration(sum.MedianMS)*time.Millisecond, time.Duration(sum.P90MS)*time.Millisecond, sum.HarnessErr)
	kinds := make([]string, 0, len(sum.Problems))
	for k := range sum.Problems {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		if sum.Problems[kinds[i]] != sum.Problems[kinds[j]] {
			return sum.Problems[kinds[i]] > sum.Problems[kinds[j]]
		}
		return kinds[i] < kinds[j]
	})
	for i, k := range kinds {
		if i == 10 {
			break
		}
		t.Logf("    %4d  %s", sum.Problems[k], k)
	}
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
