package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/provider"
	"github.com/Conte777/autogit/internal/ui"
)

func doctorCmd(g *globals, out *ui.UI) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the configuration and whether the selected provider answers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd.Context(), g, out)
		},
	}
}

func runDoctor(ctx context.Context, g *globals, out *ui.UI) error {
	repoRoot := ""
	if repo, err := openRepo(ctx, g.repo, "cli"); err == nil {
		repoRoot = repo.Root()
		out.Print("repository   %s", repoRoot)
		reportState(ctx, out, repo)
		reportBranch(ctx, out, repo)
	} else {
		out.Print("repository   none (%v)", err)
	}

	cfg, err := config.Load(config.Options{RepoRoot: repoRoot, GlobalPath: g.confPath})
	if err != nil {
		out.Print("config       BROKEN: %v", err)
		return err
	}
	applyFlags(cfg, g)

	sources := cfg.Sources()
	if len(sources) == 0 {
		out.Print("config       built-in defaults only")
	} else {
		out.Print("config       %s", strings.Join(sources, ", "))
	}

	if _, err := cfg.ResolvePreset(); err != nil {
		out.Print("preset       BROKEN: %v", err)
		return err
	}
	out.Print("preset       %s", cfg.Preset)
	out.Print("provider     %s (model %s)", cfg.Provider, cfg.Model())

	return checkProvider(ctx, cfg, out)
}

func reportState(ctx context.Context, out *ui.UI, repo *git.Repo) {
	st, err := repo.State(ctx)
	switch {
	case err != nil:
		out.Print("state        unknown: %v", err)
	case st.Blocked() != nil:
		out.Print("state        BLOCKED: %v", st.Blocked())
	case st.Op == git.OpPrepared:
		out.Print("state        a message is prepared; git's own will be used")
	case st.HasPreparedMessage():
		out.Print("state        %s in progress; git's own message will be used", st.Op)
	default:
		out.Print("state        clean")
	}
}

func reportBranch(ctx context.Context, out *ui.UI, repo *git.Repo) {
	branch, err := repo.Current(ctx)
	if err != nil {
		out.Print("branch       unknown: %v", err)
		return
	}
	if branch.Detached {
		out.Print("branch       detached at %s", branch.Name)
		return
	}
	out.Print("branch       %s", branch.Name)
}

// checkProvider does a real round trip. A provider that cannot answer is the
// single most common failure, and the fix is usually to switch to another one.
func checkProvider(ctx context.Context, cfg *config.Config, out *ui.UI) error {
	p, err := provider.Build(cfg, os.LookupEnv, nil)
	if err != nil {
		out.Print("liveness     UNAVAILABLE: %v", err)
		out.Print("             %s", switchHint(cfg.Provider))
		return &configProviderError{err}
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	start := time.Now()
	_, err = gen.Generate(ctx, p, gen.Request{
		System:    "Reply with exactly the word: ok",
		Prompt:    "ping",
		Attempts:  1,
		Validator: wordValidator{"ok"},
	})
	if err != nil {
		var fail *gen.FailureError
		if errors.As(err, &fail) {
			// It answered, just not with the word we asked for — the transport
			// is fine, which is all doctor is testing.
			out.Print("liveness     ok in %s (answered %q)", time.Since(start).Round(time.Millisecond), fail.Last)
			return nil
		}
		out.Print("liveness     FAILED: %v", err)
		out.Print("             %s", switchHint(cfg.Provider))
		return err
	}
	out.Print("liveness     ok in %s", time.Since(start).Round(time.Millisecond))
	return nil
}

type wordValidator struct{ want string }

func (v wordValidator) Check(raw string) (string, []string) {
	got := strings.ToLower(strings.TrimSpace(raw))
	if got == v.want {
		return got, nil
	}
	return got, []string{fmt.Sprintf("reply with exactly %q", v.want)}
}

func switchHint(current string) string {
	var others []string
	for _, name := range provider.Names() {
		if name != current {
			others = append(others, name)
		}
	}
	return fmt.Sprintf("try another provider: --provider %s, or set \"provider\" in the config file",
		strings.Join(others, " | "))
}
