# Providers stays a struct, not a map keyed by provider name

Every provider is declared once in `providerSpecs`, which reaches its settings
through a field accessor rather than a map lookup, and `Providers` keeps one
named field per provider. A `map[string]HTTPProvider` would be shorter and is
the obvious next simplification, but it loses two things the struct pays for:
`schema/config.schema.json` is generated from these types, so named fields
become named properties with `additionalProperties: false`, and
`json.Decoder.DisallowUnknownFields` then rejects a misspelled provider name
instead of accepting it as one more key nobody will ever read.

The shapes are not uniform either: `claude-cli` is configured by `binary` and
`extraArgs`, the HTTP providers by `baseUrl` and `maxTokens`. A single map value
type would have to be their union, and the schema would stop saying which
setting belongs where.

## Consequences

Adding a provider means adding a field to `Providers` as well as an entry to
`providerSpecs`, and regenerating the schema. That is the intended friction: the
generated schema is a public artefact, and a provider that no editor can
autocomplete is worse than a provider that costs two lines to declare.
