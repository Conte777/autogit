package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/preset"
	"github.com/Conte777/autogit/internal/ui"
)

const ejectDir = ".autogit/prompts"

func presetCmd(g *globals, out *ui.UI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preset",
		Short: "Inspect and eject the built-in presets",
	}
	cmd.AddCommand(presetListCmd(out), presetEjectCmd(g, out))
	return cmd
}

func presetListCmd(out *ui.UI) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the built-in presets",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			for _, name := range preset.Names() {
				out.Print("%s", name)
			}
			return nil
		},
	}
}

func presetEjectCmd(g *globals, out *ui.UI) *cobra.Command {
	var write bool

	cmd := &cobra.Command{
		Use:   "eject NAME",
		Short: "Copy a preset's prompts into the repository so they can be edited",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if _, ok := preset.Builtin(name); !ok {
				return &usageError{fmt.Errorf("unknown preset %q; built in: %v", name, preset.Names())}
			}
			repo, err := openRepo(cmd.Context(), g.repo, prompterFor(g, out))
			if err != nil {
				return err
			}

			target := filepath.Join(repo.Root(), ejectDir)
			if mkErr := os.MkdirAll(target, 0o750); mkErr != nil {
				return mkErr
			}
			paths := map[string]string{}
			for _, op := range []string{"commit", "branch"} {
				src, embedErr := preset.EmbeddedPrompt(name, op)
				if embedErr != nil {
					return embedErr
				}
				file := filepath.Join(target, op+".md")
				if _, statErr := os.Stat(file); statErr == nil {
					return fmt.Errorf("%s already exists; delete it first", rel(repo.Root(), file))
				} else if !errors.Is(statErr, os.ErrNotExist) {
					return statErr
				}
				if writeErr := os.WriteFile(file, []byte(src), 0o600); writeErr != nil {
					return writeErr
				}
				paths[op] = filepath.ToSlash(filepath.Join(ejectDir, op+".md"))
				out.Warn("wrote %s", rel(repo.Root(), file))
			}

			fragment, err := ejectFragment(name, paths)
			if err != nil {
				return err
			}
			if !write {
				out.Print("%s", fragment)
				out.Warn("add the fragment above to %s, or re-run with --write", config.FileName)
				return nil
			}
			return mergeRepoConfig(repo.Root(), name, paths, out)
		},
	}

	cmd.Flags().BoolVar(&write, "write", false, "add the prompt paths to "+config.FileName)
	return cmd
}

func ejectFragment(name string, paths map[string]string) (string, error) {
	fragment := map[string]any{
		"preset": name,
		"presets": map[string]any{
			name: map[string]any{
				"commit": map[string]any{"prompt": paths["commit"]},
				"branch": map[string]any{"prompt": paths["branch"]},
			},
		},
	}
	data, err := json.MarshalIndent(fragment, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// mergeRepoConfig edits .autogit.json in place, keeping every key it does not
// own.
func mergeRepoConfig(root, name string, paths map[string]string, out *ui.UI) error {
	path := filepath.Join(root, config.FileName)

	doc := map[string]any{}
	data, err := os.ReadFile(path) //nolint:gosec // inside the repository the user asked us to edit
	switch {
	case err == nil:
		if parseErr := json.Unmarshal(data, &doc); parseErr != nil {
			return fmt.Errorf("%s: %w", path, parseErr)
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	default:
		doc["$schema"] = config.SchemaURL
	}

	doc["preset"] = name
	presets, _ := doc["presets"].(map[string]any)
	if presets == nil {
		presets = map[string]any{}
	}
	entry, _ := presets[name].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
	}
	for _, op := range []string{"commit", "branch"} {
		section, _ := entry[op].(map[string]any)
		if section == nil {
			section = map[string]any{}
		}
		section["prompt"] = paths[op]
		entry[op] = section
	}
	presets[name] = entry
	doc["presets"] = presets

	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	out.Warn("updated %s", rel(root, path))
	return nil
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return path
}
