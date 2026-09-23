# CLAUDE.md

## Commands

```sh
go test ./...          # CI runs it with -race
golangci-lint run      # v2 config, gofumpt as the formatter
claude plugin validate ./plugins/autogit && claude plugin validate .   # after touching plugins/ or .claude-plugin/
AUTOGIT_EVAL_CASES=cases.tsv go test -tags eval -run Eval -v ./internal/app/   # manual only: live-model eval, cases file stays out of the repo
```

`lefthook install` (needs `brew install lefthook gitleaks`) wires the commit and push hooks. They run a subset of CI — the full pre-PR gate list is step 2 of `docs/agents/code-change-workflow.md`.

## Invariants

- `schema/config.schema.json` is generated from the Go types in `internal/config`. Touch a config struct → `go run ./cmd/autogit schema > schema/config.schema.json`, or CI fails on the diff.
- No `fmt.Print*` outside `internal/ui` and `cmd/` — forbidigo enforces it. A stray write to stdout lands in the MCP server's JSON-RPC stream and kills the session.
- API keys come from the environment only (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `AUTOGIT_API_KEY`). A key in a config file is a startup error, not a warning.
- A repository `.autogit.json` is untrusted input: `provider`, `providers.*`, `diff.excludePathspecs` and `diff.ignoreSubmodules` stay global-only, and unknown keys stay errors everywhere. The provider keys are arbitrary code execution; the diff keys hide the changed code from the model while the file list still looks complete; an ignored unknown key silently disables branch protection on a typo.
- The exit codes in `internal/cli/exit.go` are a public contract; scripts and the Claude Code hook branch on them.

## Layering

`internal/gen` is the generation core and knows nothing about git, transports or message formats — those arrive as a `Provider` and a `Validator`. `internal/app` is the only place where git, a provider and a prompt meet. Provider adapters live in `internal/provider/<name>/` and are wired in `registry.go`.

## Repo etiquette

Every code change ships as a reviewed pull request from its own worktree; `main` is protected. The sequence in `docs/agents/code-change-workflow.md` — worktree, gates, PR, two reviews, fix, merge, ticket — is binding. Commit messages follow Conventional Commits (the project's own default preset).

## Docs for agents

- Issues and specs: `gh` conventions, claiming and resolving a ticket — `docs/agents/issue-tracker.md`
- Triage labels: the five canonical roles — `docs/agents/triage-labels.md`
- Cutting a release: the tag, the plugin version that has to match it, the order — `docs/agents/release.md`
- Exploring the codebase: the `CONTEXT.md` glossary and `docs/adr/` — `docs/agents/domain.md`
