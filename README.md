# autogit

Generate git commit messages and branch names with an LLM — then check the
answer against rules you control, and commit only what passed.

```
$ autogit commit --all
committed 4f2a1c9: feat(diff): drop file bodies instead of cutting bytes
```

The generation core knows nothing about transports or message formats. Providers
and presets plug in from the outside, so switching from a Claude subscription to
your own API key is a one-line config change, not a rewrite.

- **Four providers**: `claude-cli` (your existing Claude subscription, via the
  `claude` binary), `anthropic`, `openai` (also Ollama and LM Studio through
  `baseUrl`), `gemini`.
- **Four surfaces**: the CLI, an MCP server for agents, a Claude Code
  `UserPromptSubmit` hook, and an installer that wires the two together.
- **Two built-in presets**: full Conventional Commits, and a single-line
  `TICKET: description` dialect. Eject either one and edit the prompts.

---

## Install

```sh
brew install Conte777/tap/autogit
# or
go install github.com/Conte777/autogit/cmd/autogit@latest
```

Check that it can reach a model:

```sh
autogit doctor
```

## Use

```sh
autogit commit                # commit what is staged
autogit commit --all          # stage everything first, including untracked
autogit commit --tracked      # stage tracked files first
autogit commit --force        # allow a protected branch
autogit commit --dry-run      # print the message, commit nothing

autogit commit-msg            # the same code path, stopped before the commit

autogit branch CUS-1234 add user auth
autogit branch                # infer both the type and the slug from the diff
```

On a terminal autogit asks before doing anything surprising: committing on a
protected branch, or picking what to stage when the index is empty and the tree
is not. `--no-input` turns every question into an error that names the flag you
should have passed.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | success, including `--dry-run` |
| 1 | internal error |
| 2 | usage error |
| 3 | not a git repository, or a state that blocks committing |
| 4 | nothing to commit |
| 5 | protected branch without `--force` |
| 6 | provider failure (missing binary, 401, timeout, network) |
| 7 | validation failed after every attempt |
| 8 | configuration error |
| 130 | cancelled |

## Configure

Two layers, deep-merged, the repository file on top of the global one. Per key:
CLI flags → `AUTOGIT_*` → `<repo>/.autogit.json` → `$AUTOGIT_CONFIG` or
`~/.config/autogit/config.json` → built-in defaults.

`~/.config/autogit/config.json`:

```json
{
  "$schema": "https://raw.githubusercontent.com/Conte777/autogit/main/schema/config.schema.json",
  "provider": "claude-cli",
  "preset": "conventional",
  "confirm": false,
  "attempts": 3,
  "timeout": "90s",
  "protectedBranches": ["main", "master", "develop", "stage", "staging", "release/*"],
  "diff": {
    "maxBytes": 40000,
    "context": 3,
    "ignoreSubmodules": true,
    "excludePathspecs": [
      ":(exclude)*.lock",
      ":(exclude)go.sum",
      ":(exclude)package-lock.json",
      ":(exclude)pnpm-lock.yaml"
    ]
  },
  "providers": {
    "anthropic":  { "model": "claude-haiku-4-5", "maxTokens": 1024 },
    "claude-cli": { "binary": "claude", "model": "haiku" },
    "openai":     { "model": "gpt-4.1-mini", "baseUrl": "https://api.openai.com/v1" },
    "gemini":     { "model": "gemini-2.5-flash" }
  }
}
```

Run `autogit schema` to print the schema the config validates against.

### API keys never live in the config file

They come from the environment only: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`GEMINI_API_KEY`, or `AUTOGIT_API_KEY` as a fallback. A key found in a config
file is a startup error, not a warning.

### The repository file is untrusted input

A `.autogit.json` you cloned along with someone else's repository may set only
`preset`, `presets.*`, `protectedBranches`, `confirm` and `diff.*`. `provider`
and `providers.*` are global-only: otherwise `providers.claude-cli.binary` from
a stranger's repo would be arbitrary code execution, and `baseUrl` would be a
key collector. Unknown keys are errors everywhere — a typo in
`protectedBranches` must not silently switch branch protection off.

### Environment

| Variable | Effect |
|---|---|
| `AUTOGIT_CONFIG` | path to the global config file |
| `AUTOGIT_PROVIDER` | override the provider |
| `AUTOGIT_PRESET` | override the preset |
| `AUTOGIT_MODEL` | override the model **of the selected provider** |
| `AUTOGIT_ATTEMPTS` | how many times the model may fix its output |
| `AUTOGIT_TIMEOUT` | budget for one generation |
| `AUTOGIT_CONFIRM` | ask before committing |

## Presets

A preset is a pair of prompts plus the rules that check what comes back.

- **`conventional`** (default) — subject, optional scope, body and footers.
  Subject limit 72. Scopes are mined from the repository's own history
  (`git log -n 500`, top 20 by frequency) and offered to the model as a
  vocabulary; below 10 conventional commits nothing is offered at all, because
  two examples teach worse than none. A body is requested only above a size
  threshold. `Refs:` appears only when the branch name carries a ticket.
- **`ticket`** — `CUS-1234|feat|fix: description`, 50 characters, one line,
  with an abbreviation glossary.

Override any field of either:

```json
{
  "preset": "conventional",
  "presets": {
    "conventional": {
      "commit": {
        "maxSubject": 60,
        "scope": { "mode": "whitelist", "values": ["api", "cli", "db"] },
        "body": { "mode": "off" }
      }
    }
  }
}
```

To edit the prompts themselves:

```sh
autogit preset eject conventional --write
```

That writes `.autogit/prompts/{commit,branch}.md` and points the repository
config at them. Prompt paths always resolve against the config file that
declared them.

A prompt file is frontmatter, then `## System` and `## User`, rendered with
`text/template`. Referencing a field the data type does not have is an error at
load time, not halfway through a commit.

### What the validator does and does not enforce

It enforces what a diff-blind checker can prove: the subject shape, the length
limit, lowercase, no trailing period, no body when the format is single-line, a
description that is not simply the branch name. It does **not** enforce "there
should be a body here" or "this change is breaking" — those are not decidable
from the message alone, and enforcing them burns every retry and then rejects a
perfectly good commit.

## Agents

### MCP

```sh
claude mcp add --scope user autogit -- autogit mcp
```

Exposes `commit` and `branch`. The tool generates and validates the message
itself; the agent never writes or passes one. There is no
`allowProtectedBranch` parameter, so a model cannot talk itself into committing
on `main` — that takes a human at a terminal with `--force`.

### Claude Code hook

```sh
autogit install claude-code            # dry run, prints what would change
autogit install claude-code --write
```

This registers `autogit hook` on `UserPromptSubmit`, allows the two MCP tools in
`permissions`, and writes `/commit`, `/commit-msg` and `/branch` slash-command
stubs. The hook blocks the prompt and does the work itself, so the model never
wakes up for a commit. `autogit uninstall claude-code` removes exactly that and
nothing else.

`settings.json` is backed up with a timestamped name before each change and
written through symlinks, so a config linked into a dotfiles repository stays
linked.

## Two ways to reach Claude

`claude-cli` drives the `claude` binary you already have installed and logged
in, over `stream-json` on a long-lived process. This is the only legitimate
route to a Claude subscription: Anthropic
[does not permit](https://code.claude.com/docs/en/legal-and-compliance)
third-party tools to hold Free, Pro or Max credentials.

`anthropic` goes straight to the Messages API with your own key. It is the
escape hatch, and it is tested rather than theoretical — `claude -p` has drifted
away from subscription usage twice already, and if it goes for good, autogit
survives it by changing one line of config.

## Development

```sh
brew install lefthook gitleaks
lefthook install
go test ./...
golangci-lint run
```

`schema/config.schema.json` is generated from the Go types. CI fails if
`autogit schema` no longer matches it.

## License

MIT
