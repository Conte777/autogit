package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FileName is the per-repository config file.
const FileName = ".autogit.json"

// Error marks a configuration problem, which the CLI turns into exit code 8.
type Error struct{ Err error }

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func configErr(format string, a ...any) error { return &Error{fmt.Errorf(format, a...)} }

// Options selects where Load looks.
type Options struct {
	// RepoRoot is the working tree; "" skips the repository layer.
	RepoRoot string
	// GlobalPath overrides $AUTOGIT_CONFIG and the XDG default.
	GlobalPath string
	// Env is the environment to read; nil means the process environment.
	Env func(string) (string, bool)
}

// Load builds the effective configuration.
func Load(opts Options) (*Config, error) {
	if opts.Env == nil {
		opts.Env = os.LookupEnv
	}
	cfg := Default()
	cfg.env = opts.Env

	globalPath := opts.GlobalPath
	if globalPath == "" {
		globalPath = globalConfigPath(opts.Env)
	}
	if data, ok, err := readIfExists(globalPath); err != nil {
		return nil, err
	} else if ok {
		if err := applyGlobal(&cfg, data, globalPath); err != nil {
			return nil, err
		}
	}

	if opts.RepoRoot != "" {
		repoPath := filepath.Join(opts.RepoRoot, FileName)
		if data, ok, err := readIfExists(repoPath); err != nil {
			return nil, err
		} else if ok {
			if err := applyRepo(&cfg, data, repoPath); err != nil {
				return nil, err
			}
		}
	}

	if err := applyEnv(&cfg, opts.Env); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func readIfExists(path string) ([]byte, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is the user's own config location
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, configErr("cannot read %s: %v", path, err)
	}
	return data, true, nil
}

func globalConfigPath(env func(string) (string, bool)) string {
	if p, ok := env("AUTOGIT_CONFIG"); ok && p != "" {
		return expandHome(p, env)
	}
	if p, ok := env("XDG_CONFIG_HOME"); ok && p != "" {
		return filepath.Join(p, "autogit", "config.json")
	}
	home, ok := env("HOME")
	if !ok || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "autogit", "config.json")
}

func applyGlobal(cfg *Config, data []byte, path string) error {
	if err := rejectSecrets(data, path); err != nil {
		return err
	}
	if err := decodeStrict(data, cfg); err != nil {
		return configErr("%s: %v", path, err)
	}
	takePresetLayer(cfg, filepath.Dir(path))
	cfg.sources = append(cfg.sources, path)
	return nil
}

// repoConfig is the whitelist for a repository-level file. `provider` and
// `providers.*` are absent on purpose: a cloned repo could otherwise point
// `providers.claude-cli.binary` at anything, or `baseUrl` at a key collector.
type repoConfig struct {
	Schema            string                    `json:"$schema,omitempty"`
	Preset            string                    `json:"preset,omitempty"`
	Presets           map[string]PresetOverride `json:"presets,omitempty"`
	ProtectedBranches []string                  `json:"protectedBranches,omitempty"`
	Confirm           *bool                     `json:"confirm,omitempty"`
	PreparedMessage   *bool                     `json:"preparedMessage,omitempty"`
	Diff              json.RawMessage           `json:"diff,omitempty"`
}

func applyRepo(cfg *Config, data []byte, path string) error {
	if err := rejectSecrets(data, path); err != nil {
		return err
	}
	var repo repoConfig
	if err := decodeStrict(data, &repo); err != nil {
		return configErr("%s: %v\n"+
			"a repository config may only set preset, presets, protectedBranches, confirm, "+
			"preparedMessage, diff.maxBytes and diff.context; "+
			"provider and provider settings are global-only", path, err)
	}

	if repo.Preset != "" {
		cfg.Preset = repo.Preset
	}
	if repo.ProtectedBranches != nil {
		cfg.ProtectedBranches = repo.ProtectedBranches
	}
	if repo.Confirm != nil {
		cfg.Confirm = *repo.Confirm
	}
	if repo.PreparedMessage != nil {
		cfg.PreparedMessage = *repo.PreparedMessage
	}
	if len(repo.Diff) > 0 {
		if err := applyRepoDiff(&cfg.Diff, repo.Diff, path); err != nil {
			return err
		}
	}
	if len(repo.Presets) > 0 {
		dir := filepath.Dir(path)
		cfg.presetLayers = append(cfg.presetLayers, presetLayer{dir: dir, confinedTo: dir, defs: repo.Presets})
	}
	cfg.sources = append(cfg.sources, path)
	return nil
}

// repoDiff is the diff whitelist for a repository file. The keys left here can
// only shorten the diff in a way the diff itself reports: truncation writes the
// stat and a note into the very text the model reads.
type repoDiff struct {
	MaxBytes *int `json:"maxBytes,omitempty"`
	Context  *int `json:"context,omitempty"`
}

// diffKeysGlobalOnly are the diff keys a repository file may not set. Each drops
// the body of a change while the file list stays complete, so a repository could
// hand the model a diff that describes nothing and says so nowhere.
var diffKeysGlobalOnly = []string{"excludePathspecs", "ignoreSubmodules"}

func applyRepoDiff(diff *Diff, data []byte, path string) error {
	if key, found := findGlobalOnlyDiffKey(data); found {
		return configErr("%s: diff.%s is global-only: it hides the body of a change from the "+
			"model while the file list stays complete, so the generated message would "+
			"describe nothing and say so nowhere", path, key)
	}
	var repo repoDiff
	if err := decodeStrict(data, &repo); err != nil {
		return configErr("%s: diff: %v\n"+
			"a repository config may only set diff.maxBytes and diff.context", path, err)
	}
	if repo.MaxBytes != nil {
		// Only downwards: a budget raised by a cloned repo is a diff read large
		// enough to exhaust memory before anything is generated.
		if *repo.MaxBytes < 1 {
			return configErr("%s: diff.maxBytes must be positive in a repository config; "+
				"0 or less means an unbounded diff read, which is global-only", path)
		}
		if diff.MaxBytes > 0 && *repo.MaxBytes > diff.MaxBytes {
			return configErr("%s: diff.maxBytes must not exceed %d, the budget already in effect; "+
				"a repository may shrink the diff budget, not grow it", path, diff.MaxBytes)
		}
		diff.MaxBytes = *repo.MaxBytes
	}
	if repo.Context != nil {
		diff.Context = *repo.Context
	}
	return nil
}

// findGlobalOnlyDiffKey reports a global-only key present in a repository diff
// object. The match is case-insensitive because the decoder's own field
// matching is, so `ExcludePathspecs` must not slip past with a vaguer error.
func findGlobalOnlyDiffKey(data []byte) (string, bool) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return "", false
	}
	for key := range keys {
		for _, banned := range diffKeysGlobalOnly {
			if strings.EqualFold(key, banned) {
				return key, true
			}
		}
	}
	return "", false
}

// takePresetLayer moves the overrides just decoded into the layer list, so the
// next file's overrides do not silently replace them.
func takePresetLayer(cfg *Config, dir string) {
	if len(cfg.Presets) > 0 {
		cfg.presetLayers = append(cfg.presetLayers, presetLayer{dir: dir, defs: cfg.Presets})
		cfg.Presets = nil
	}
}

func applyEnv(cfg *Config, env func(string) (string, bool)) error {
	if v, ok := env("AUTOGIT_PROVIDER"); ok && v != "" {
		cfg.Provider = v
	}
	if v, ok := env("AUTOGIT_PRESET"); ok && v != "" {
		cfg.Preset = v
	}
	if v, ok := env("AUTOGIT_ATTEMPTS"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return configErr("AUTOGIT_ATTEMPTS must be a positive integer (got %q)", v)
		}
		cfg.Attempts = n
	}
	if v, ok := env("AUTOGIT_TIMEOUT"); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return configErr("AUTOGIT_TIMEOUT must be a duration like 90s (got %q)", v)
		}
		cfg.Timeout = Duration(d)
	}
	if v, ok := env("AUTOGIT_CONFIRM"); ok && v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return configErr("AUTOGIT_CONFIRM must be true or false (got %q)", v)
		}
		cfg.Confirm = b
	}
	if v, ok := env("AUTOGIT_PREPARED_MESSAGE"); ok && v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return configErr("AUTOGIT_PREPARED_MESSAGE must be true or false (got %q)", v)
		}
		cfg.PreparedMessage = b
	}
	// Scoped to the selected provider: a bare model name applied globally would
	// pair an OpenAI model with an Anthropic endpoint.
	if v, ok := env("AUTOGIT_MODEL"); ok && v != "" {
		cfg.SetModel(v)
	}
	return nil
}

// SetModel overrides the model of the currently selected provider.
func (c *Config) SetModel(model string) {
	if spec, ok := LookupProvider(c.Provider); ok {
		*spec.Model(&c.Providers) = model
	}
}

// Model reports the model of the selected provider.
func (c *Config) Model() string {
	if spec, ok := LookupProvider(c.Provider); ok {
		return *spec.Model(&c.Providers)
	}
	return ""
}

func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	// Trailing content means two JSON documents in one file.
	if dec.More() {
		return errors.New("trailing content after the JSON object")
	}
	return nil
}

var secretKeys = []string{"apikey", "api_key", "token", "secret", "password", "authorization"}

// rejectSecrets refuses a config that carries a credential. Keys live in the
// environment: a config file gets committed, shared and backed up.
func rejectSecrets(data []byte, path string) error {
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		return configErr("%s: %v", path, err)
	}
	if key, found := findSecretKey(tree); found {
		return configErr("%s: %q must not be in a config file; "+
			"set ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY or AUTOGIT_API_KEY instead", path, key)
	}
	return nil
}

func findSecretKey(node any) (string, bool) {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			lower := strings.ToLower(strings.ReplaceAll(k, "-", "_"))
			for _, bad := range secretKeys {
				if lower == bad {
					return k, true
				}
			}
			if key, found := findSecretKey(v); found {
				return key, true
			}
		}
	case []any:
		for _, v := range n {
			if key, found := findSecretKey(v); found {
				return key, true
			}
		}
	}
	return "", false
}

// APIKey returns the key for a provider, provider-specific variable first.
func APIKey(provider string, env func(string) (string, bool)) string {
	if env == nil {
		env = os.LookupEnv
	}
	if spec, ok := LookupProvider(provider); ok && spec.EnvKey != "" {
		if v, ok := env(spec.EnvKey); ok && v != "" {
			return v
		}
	}
	if v, ok := env("AUTOGIT_API_KEY"); ok {
		return v
	}
	return ""
}

// resolvePath expands `~` and makes a relative path absolute against base.
func resolvePath(path, base string, env func(string) (string, bool)) string {
	if path == "" {
		return ""
	}
	if env == nil {
		env = os.LookupEnv
	}
	path = expandHome(path, env)
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}

func expandHome(path string, env func(string) (string, bool)) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, ok := env("HOME")
	if !ok || home == "" {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
}
