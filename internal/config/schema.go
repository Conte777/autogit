package config

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/Conte777/autogit/internal/preset"
)

// Schema returns the JSON schema of Config, pretty-printed and newline
// terminated so it can be diffed against the committed copy.
func Schema() ([]byte, error) {
	s, err := documentSchema[Config]()
	if err != nil {
		return nil, err
	}
	s.Schema = "https://json-schema.org/draft/2020-12/schema"
	s.Title = "autogit configuration"

	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// documentSchema builds the schema of a config document. T is Config for the
// global file and repoConfig for the repository one, which is why the provider
// enum is applied only where a `provider` property exists.
func documentSchema[T any]() (*jsonschema.Schema, error) {
	workspace, err := workspaceSchema()
	if err != nil {
		return nil, err
	}
	types, err := typeSchemas(workspace)
	if err != nil {
		return nil, err
	}
	s, err := jsonschema.For[T](&jsonschema.ForOptions{TypeSchemas: types})
	if err != nil {
		return nil, err
	}
	applyProviderEnum(s)
	clearRequired(s)
	return s, nil
}

// workspaceSchema is the Config schema with `path` added and `workspaces`
// removed: a rule carries every key of the global file, and does not nest.
func workspaceSchema() (*jsonschema.Schema, error) {
	types, err := typeSchemas(&jsonschema.Schema{Type: "object"})
	if err != nil {
		return nil, err
	}
	s, err := jsonschema.For[Config](&jsonschema.ForOptions{TypeSchemas: types})
	if err != nil {
		return nil, err
	}
	delete(s.Properties, "workspaces")
	s.Properties["path"] = &jsonschema.Schema{
		Type:        "string",
		Description: "directory whose repositories the rule applies to",
	}
	applyProviderEnum(s)
	return s, nil
}

func typeSchemas(workspace *jsonschema.Schema) (map[reflect.Type]*jsonschema.Schema, error) {
	presetSchema, err := jsonschema.For[preset.Preset](nil)
	if err != nil {
		return nil, err
	}
	repoDiffSchema, err := jsonschema.For[repoDiff](nil)
	if err != nil {
		return nil, err
	}
	return map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[Duration](): {
			Type:        "string",
			Description: "Go duration string, e.g. 90s, 2m or 1m30s",
			Pattern:     `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`,
		},
		// PresetOverride is raw JSON at runtime; the schema shows the shape
		// a user actually writes. The only raw section left is the
		// repository file's diff, which carries its own narrower whitelist.
		reflect.TypeFor[PresetOverride]():  presetSchema,
		reflect.TypeFor[json.RawMessage](): repoDiffSchema,
		reflect.TypeFor[Workspace]():       workspace,
	}, nil
}

// applyProviderEnum takes the list of providers from the table, not a struct
// tag: a name spelled in two places is the drift this schema exists to catch.
func applyProviderEnum(s *jsonschema.Schema) {
	p, ok := s.Properties["provider"]
	if !ok {
		return
	}
	for _, name := range ProviderNames() {
		p.Enum = append(p.Enum, name)
	}
}

// clearRequired walks the schema dropping every "required" list. A config file
// is a partial document merged over the defaults, so the fields the generator
// takes for required — the ones whose Go tag carries no omitempty — are
// optional in anything a user actually writes.
func clearRequired(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	s.Required = nil
	for _, sub := range s.Properties {
		clearRequired(sub)
	}
	for _, sub := range s.Defs {
		clearRequired(sub)
	}
	clearRequired(s.Items)
	clearRequired(s.AdditionalProperties)
}

// ValidateDocument checks raw config bytes against the generated schema. It
// exists so a wrong type reports the offending path instead of a decoder
// message about an interface conversion, and so a name outside an enum — which
// decodes cleanly and only fails much later — is reported at all.
func ValidateDocument(data []byte) error { return validateAgainst[Config](data) }

// validateRepoDocument checks a repository file against the whitelist that
// will decode it, not against Config: `provider` and `providers.*` are
// global-only, and a report that accepted them here would contradict Load.
func validateRepoDocument(data []byte) error { return validateAgainst[repoConfig](data) }

func validateAgainst[T any](data []byte) error {
	s, err := documentSchema[T]()
	if err != nil {
		return err
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return resolved.Validate(doc)
}
