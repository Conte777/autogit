package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/provider"
	"github.com/Conte777/autogit/internal/ui"
)

// globals are the flags every operation shares, plus the environment they are
// read against — the seam that lets a test run `build` without handing it the
// process's own variables.
type globals struct {
	repo     string
	provider string
	preset   string
	model    string
	attempts int
	timeout  time.Duration
	confPath string
	noInput  bool
	env      func(string) (string, bool)
}

// Root builds the whole command tree. v is stamped in at build time.
func Root(v Version) *cobra.Command {
	version = v.Version
	g := &globals{env: os.LookupEnv}
	out := ui.Std()

	root := &cobra.Command{
		Use:   "autogit",
		Short: "Generate git commit messages and branch names with an LLM",
		Long: "autogit stages, asks a model for a message, checks the answer against the\n" +
			"preset's rules, and commits what actually passed.",
		Version:       v.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	f := root.PersistentFlags()
	f.StringVarP(&g.repo, "repo", "C", "", "repository path (default: current directory)")
	f.StringVar(&g.provider, "provider", "", "override the configured provider")
	f.StringVar(&g.preset, "preset", "", "override the configured preset")
	f.StringVar(&g.model, "model", "", "override the model of the selected provider")
	f.IntVar(&g.attempts, "attempts", 0, "how many times the model may fix its output")
	f.DurationVar(&g.timeout, "timeout", 0, "budget for one generation")
	f.StringVar(&g.confPath, "config", "", "path to the global config file")
	f.BoolVar(&g.noInput, "no-input", false, "never ask questions; fail with a hint instead")

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return &usageError{err} })

	root.AddCommand(
		commitCmd(g, out),
		commitMsgCmd(g, out),
		branchCmd(g, out),
		schemaCmd(out),
		presetCmd(g, out),
		doctorCmd(g, out),
		hookCmd(g),
		mcpCmd(g),
	)
	// Cobra grows `help` and `completion` on its way into Execute, which is
	// after the walk below; `completion` is a parent command of exactly the
	// shape the walk exists to fix.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	classifyUsageErrors(root)
	return root
}

// classifyUsageErrors makes every node of the tree answer a bad invocation with
// exit 2. Cobra's own answer is a plain error — a bug in autogit, by the exit
// codes — and under a command it cannot run, no error at all: RunE is what
// carries a mistyped subcommand as far as Args.
func classifyUsageErrors(cmd *cobra.Command) {
	if cmd.HasSubCommands() {
		// SuggestionsFor reads the distance raw; only cobra's own path fills
		// the zero value in.
		cmd.SuggestionsMinimumDistance = 2
		if !cmd.Runnable() {
			cmd.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }
		}
	}
	switch args := cmd.Args; {
	case args != nil:
		cmd.Args = func(c *cobra.Command, a []string) error {
			if err := args(c, a); err != nil {
				return &usageError{err}
			}
			return nil
		}
	case cmd.HasSubCommands():
		cmd.Args = unknownCommandIsUsage
	}
	for _, sub := range cmd.Commands() {
		classifyUsageErrors(sub)
	}
}

// Execute runs the tree and returns the process exit code.
func Execute(ctx context.Context, v Version) int {
	// cobra hands the context to each RunE through cmd.Context(); Root itself
	// only builds the tree.
	root := Root(v) //nolint:contextcheck
	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	code := ExitCode(err)
	fmt.Fprintf(os.Stderr, "autogit: %v\n", err)
	if detail := Detail(err); detail != "" {
		fmt.Fprintf(os.Stderr, "         %s\n", detail)
	}
	return code
}

// build assembles everything an operation needs from flags, config and the
// repository. It is the single place that decides what the run looks like.
func build(ctx context.Context, g *globals, prompter ui.Prompter) (*app.App, error) {
	repo, err := openRepo(ctx, g.repo, prompter)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(config.Options{
		RepoRoot:   repo.Root(),
		GlobalPath: g.confPath,
		Env:        g.env,
	})
	if err != nil {
		return nil, err
	}
	applyFlags(cfg, g)

	prov, err := provider.Build(cfg, g.env, nil)
	if err != nil {
		return nil, &config.Error{Err: err}
	}

	return app.New(repo, cfg, prov, prompter)
}

// prompterFor picks who answers a question on the CLI. `--no-input` hands back
// the same silent prompter mcp and hook use.
func prompterFor(g *globals, out *ui.UI) ui.Prompter {
	if g.noInput {
		return ui.Noop{}
	}
	return out
}

// applyFlags is the last layer: flags beat environment beats files.
func applyFlags(cfg *config.Config, g *globals) {
	if g.provider != "" {
		cfg.Provider = g.provider
	}
	if g.preset != "" {
		cfg.Preset = g.preset
	}
	if g.attempts > 0 {
		cfg.Attempts = g.attempts
	}
	if g.timeout > 0 {
		cfg.Timeout = config.Duration(g.timeout)
	}
	// Applied after the provider is settled, so it lands on the right one.
	if g.model != "" {
		cfg.SetModel(g.model)
	}
}

func unknownCommandIsUsage(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(suggestions, "\n\t")
	}
	return &usageError{errors.New(msg)}
}
