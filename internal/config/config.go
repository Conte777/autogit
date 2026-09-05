// Package config layers the settings: built-in defaults, the global file, the
// repository file, environment, then flags. The repository file is untrusted —
// anyone can clone a repo carrying one — so it may only touch formatting.
package config

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Conte777/autogit/internal/preset"
)

// SchemaURL is the $schema value autogit writes into generated config files.
const SchemaURL = "https://raw.githubusercontent.com/Conte777/autogit/main/schema/config.schema.json"

// Config is the whole configuration surface, and the source of the JSON schema.
type Config struct {
	Schema            string                    `json:"$schema,omitempty" jsonschema:"URL of the JSON schema for this file"`
	Provider          string                    `json:"provider,omitempty" jsonschema:"which provider answers; global config only"`
	Preset            string                    `json:"preset,omitempty" jsonschema:"name of the commit/branch format"`
	Confirm           bool                      `json:"confirm" jsonschema:"ask before committing; honoured only on an interactive terminal"`
	PreparedMessage   bool                      `json:"preparedMessage" jsonschema:"commit the message git prepared for a merge, squash merge, cherry-pick or revert instead of generating one"`
	Attempts          int                       `json:"attempts,omitempty" jsonschema:"how many times the model may be asked to fix its output"`
	Timeout           Duration                  `json:"timeout,omitempty" jsonschema:"budget for one generation, e.g. 90s"`
	ProtectedBranches []string                  `json:"protectedBranches,omitempty" jsonschema:"branch name globs that require --force"`
	Diff              Diff                      `json:"diff"`
	Providers         Providers                 `json:"providers"`
	Presets           map[string]PresetOverride `json:"presets,omitempty" jsonschema:"per-preset overrides, merged over the built-in of the same name"`

	// presetLayers keeps overrides in the order they were declared, together
	// with the directory each came from, because prompt paths resolve against
	// the file that declared them.
	presetLayers []presetLayer
	// sources lists the config files that were actually read, for `doctor`.
	sources []string
	// env is the environment Load read, kept so that a `~` in a prompt path
	// expands against the same one rather than the process's.
	env func(string) (string, bool)
}

// Diff controls how much of the change the model gets to see.
type Diff struct {
	MaxBytes         int      `json:"maxBytes,omitempty" jsonschema:"truncation budget for the diff text"`
	Context          int      `json:"context,omitempty" jsonschema:"lines of context per hunk"`
	IgnoreSubmodules bool     `json:"ignoreSubmodules"`
	ExcludePathspecs []string `json:"excludePathspecs,omitempty" jsonschema:"git pathspecs kept out of the diff body; the file list stays complete"`
}

// Providers is the per-provider settings. API keys are deliberately absent:
// they come from the environment only.
type Providers struct {
	Anthropic HTTPProvider `json:"anthropic"`
	ClaudeCLI ClaudeCLI    `json:"claude-cli"`
	OpenAI    HTTPProvider `json:"openai"`
	Gemini    HTTPProvider `json:"gemini"`
}

// HTTPProvider configures one of the API-key providers.
type HTTPProvider struct {
	Model     string `json:"model,omitempty"`
	BaseURL   string `json:"baseUrl,omitempty" jsonschema:"override for self-hosted or compatible endpoints"`
	MaxTokens int    `json:"maxTokens,omitempty"`
}

// ClaudeCLI configures the subscription path through the user's own binary.
type ClaudeCLI struct {
	Binary    string   `json:"binary,omitempty"`
	Model     string   `json:"model,omitempty"`
	ExtraArgs []string `json:"extraArgs,omitempty"`
}

// PresetOverride is a partial preset kept as raw JSON so it can be decoded on
// top of the built-in it names.
type PresetOverride struct{ raw json.RawMessage }

func (p PresetOverride) MarshalJSON() ([]byte, error) {
	if len(p.raw) == 0 {
		return []byte("{}"), nil
	}
	return p.raw, nil
}

func (p *PresetOverride) UnmarshalJSON(b []byte) error {
	p.raw = append(json.RawMessage(nil), b...)
	return nil
}

type presetLayer struct {
	dir  string
	defs map[string]PresetOverride
}

// Duration is a time.Duration written as a Go duration string.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("timeout must be a duration string like \"90s\"")
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Duration converts back to the stdlib type.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Default is the configuration autogit uses with no files at all.
func Default() Config {
	return Config{
		Provider:          "claude-cli",
		Preset:            "conventional",
		Confirm:           false,
		PreparedMessage:   true,
		Attempts:          3,
		Timeout:           Duration(90 * time.Second),
		ProtectedBranches: []string{"main", "master", "develop", "stage", "staging", "release/*"},
		Diff: Diff{
			MaxBytes:         40000,
			Context:          3,
			IgnoreSubmodules: true,
			ExcludePathspecs: []string{
				":(exclude)*.lock",
				":(exclude)go.sum",
				":(exclude)package-lock.json",
				":(exclude)pnpm-lock.yaml",
				":(exclude)yarn.lock",
			},
		},
		Providers: defaultProviders(),
	}
}

// Sources lists the config files that were read, nearest last.
func (c *Config) Sources() []string { return c.sources }

// Preset resolves the effective preset: the built-in of that name with every
// declared override decoded on top, prompt paths already made absolute.
func (c *Config) ResolvePreset() (preset.Preset, error) {
	p, ok := preset.Builtin(c.Preset)
	if !ok {
		// A preset unknown to the binary is still usable if a layer defines it
		// in full, but it needs somewhere to start.
		if !c.definedInLayers(c.Preset) {
			return preset.Preset{}, &Error{
				fmt.Errorf("unknown preset %q; built in: %v", c.Preset, preset.Names()),
			}
		}
		p = preset.Empty(c.Preset)
	}

	for _, layer := range c.presetLayers {
		override, ok := layer.defs[c.Preset]
		if !ok || len(override.raw) == 0 {
			continue
		}
		if err := decodeStrict(override.raw, &p); err != nil {
			return preset.Preset{}, &Error{fmt.Errorf("presets.%s: %w", c.Preset, err)}
		}
		resolvePromptPaths(&p, layer.dir, c.env)
	}

	if err := p.Validate(); err != nil {
		return preset.Preset{}, &Error{fmt.Errorf("presets.%s: %w", c.Preset, err)}
	}
	return p, nil
}

func (c *Config) definedInLayers(name string) bool {
	for _, layer := range c.presetLayers {
		if _, ok := layer.defs[name]; ok {
			return true
		}
	}
	return false
}

func resolvePromptPaths(p *preset.Preset, dir string, env func(string) (string, bool)) {
	p.Commit.Prompt = resolvePath(p.Commit.Prompt, dir, env)
	p.Branch.Prompt = resolvePath(p.Branch.Prompt, dir, env)
}
