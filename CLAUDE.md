# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
go test ./...                  # CI runs it with -race
golangci-lint run              # v2 config, gofumpt as the formatter
go run ./cmd/autogit schema | diff -u schema/config.schema.json -
claude plugin validate ./plugins/autogit && claude plugin validate .   # plugin + marketplace manifests
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

**Code never lands on `main` as a direct commit.** A task that edits code runs in its own worktree and finishes as a pull request that has been reviewed on both axes, fixed, and merged — the sequence is in `docs/agents/code-change-workflow.md`, and it is binding, not a suggestion.

## Agent skills

### Issue tracker

Issues live in GitHub Issues for `Conte777/autogit`, driven by the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Code changes

The worktree, the gates, the PR, the two reviews, the merge. See `docs/agents/code-change-workflow.md`.

### Triage labels

The five canonical triage roles, each label string equal to its name. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
