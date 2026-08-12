package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/install"
	"github.com/Conte777/autogit/internal/ui"
)

func installCmd(out *ui.UI) *cobra.Command {
	return agentCmd("install", "Wire autogit into an agent's configuration", out, install.PlanInstall)
}

func uninstallCmd(out *ui.UI) *cobra.Command {
	return agentCmd("uninstall", "Remove what `install` added, and nothing else", out, install.PlanUninstall)
}

func agentCmd(verb, short string, out *ui.UI, plan func(install.Options) (*install.Plan, error)) *cobra.Command {
	var (
		write bool
		dir   string
	)

	cmd := &cobra.Command{
		Use:   verb + " claude-code",
		Short: short,
		Long: "Nothing is written without --write: the default is a dry run that prints\n" +
			"exactly what would change.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if args[0] != "claude-code" {
				return &usageError{fmt.Errorf("unknown agent %q; supported: claude-code", args[0])}
			}
			target, err := claudeDir(dir)
			if err != nil {
				return err
			}
			p, err := plan(install.Options{Dir: target, Binary: binaryPath()})
			if err != nil {
				return err
			}
			if p.Empty() {
				out.Print("nothing to do: %s is already up to date", target)
				return nil
			}
			for _, change := range p.Changes {
				out.Print("%s", change)
			}
			if !write {
				out.Warn("dry run; re-run with --write to apply")
				return nil
			}
			if err := p.Apply(); err != nil {
				return err
			}
			out.Print("applied to %s", target)
			return nil
		},
	}

	cmd.Flags().BoolVar(&write, "write", false, "actually apply the changes")
	cmd.Flags().StringVar(&dir, "claude-dir", "", "path to the .claude directory (default: ~/.claude)")
	return cmd
}

func claudeDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// binaryPath prefers the absolute path of the running binary, so the hook keeps
// working when the agent runs with a different PATH.
func binaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "autogit"
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "autogit"
		}
		return exe
	}
	return resolved
}
