# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
go test ./...                  # CI runs it with -race
golangci-lint run              # v2 config, gofumpt as the formatter
go run ./cmd/autogit schema | diff -u schema/config.schema.json -
```

`lefthook install` wires the local gates: gofumpt + `go vet` + `golangci-lint --fast-only` + gitleaks on commit, `go test ./...` and a `go mod tidy` cleanliness check on push. Requires `brew install lefthook gitleaks`.

## Invariants

- `schema/config.schema.json` is generated from the Go types in `internal/config`. Touch a config struct → regenerate it (`go run ./cmd/autogit schema > schema/config.schema.json`), or CI fails on the diff.
- No `fmt.Print*` outside `internal/ui` and `cmd/` — forbidigo enforces it. A stray write to stdout lands in the MCP server's JSON-RPC stream and kills the session.
- API keys come from the environment only (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `AUTOGIT_API_KEY`). A key in a config file is a startup error, not a warning.
- A repository `.autogit.json` is untrusted input: `provider` and `providers.*` are global-only, unknown keys are errors everywhere. Do not relax either without a reason — the first is arbitrary code execution, the second silently disables branch protection on a typo.
- The exit codes in `internal/cli/exit.go` are a public contract; scripts and the Claude Code hook branch on them.

## Layering

`internal/gen` is the generation core and knows nothing about git, transports or message formats — those arrive as a `Provider` and a `Validator`. `internal/app` is the only place where git, a provider and a prompt meet. Provider adapters live in `internal/provider/<name>/` and are wired in `registry.go`.

## Repo etiquette

Changes reach `main` through a pull request; `main` is protected. Commit messages follow Conventional Commits (the project's own default preset).
