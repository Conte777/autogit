// Package assets embeds the built-in preset prompts. It lives at the repo root
// because go:embed cannot reach outside its own package directory, and the
// prompts are meant to be browsable, not buried under internal/.
package assets

import "embed"

// Presets holds `presets/<name>/<op>.md`.
//
//go:embed all:presets
var Presets embed.FS
