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
	presetSchema, err := jsonschema.For[preset.Preset](nil)
	if err != nil {
		return nil, err
	}
	s, err := jsonschema.For[Config](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[Duration](): {
				Type:        "string",
				Description: "Go duration string, e.g. 90s, 2m or 1m30s",
				Pattern:     `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`,
			},
			// PresetOverride is raw JSON at runtime; the schema shows the shape
			// a user actually writes.
			reflect.TypeFor[PresetOverride](): presetSchema,
		},
	})
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

// ValidateDocument checks raw config bytes against the generated schema. It
// exists so a wrong type reports the offending path instead of a decoder
// message about an interface conversion.
func ValidateDocument(data []byte) error {
	raw, err := Schema()
	if err != nil {
		return err
	}
	var s jsonschema.Schema
	if unmarshalErr := json.Unmarshal(raw, &s); unmarshalErr != nil {
		return unmarshalErr
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
