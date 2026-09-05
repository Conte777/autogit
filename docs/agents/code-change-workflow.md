# Code-Change Workflow

Binding for any agent session that changes code. Code never lands on `main` as a direct commit: it arrives as a reviewed, squash-merged pull request. `main` is protected.

Code means anything under `cmd/`, `internal/`, `assets/`, `schema/`, or the build and CI files that gate them (`.golangci.yml`, `.goreleaser.yaml`, `lefthook.yml`, `.github/workflows/`). A change touching only prose — README, `CONTEXT.md`, `docs/`, ADRs, issue bodies — is out of scope here and commits directly; it rejoins this workflow the moment it ships alongside code.

## 1. Work in a worktree

Check out a fresh worktree before the first edit — never edit code in the primary clone. Concurrent sessions would otherwise fight over one working tree.

```sh
git worktree add .claude/worktrees/<slug> -b <type>/<slug>
```

`<slug>` is the task in three or four words (`untrusted-repo-config`); `<type>` is the Conventional Commits type the work will carry (`feat`, `fix`, `test`, `build`). `/.claude/worktrees/` is gitignored. `EnterWorktree` places worktrees here too, so either door is fine.

## 2. Green every gate before the PR

Run every job of `.github/workflows/ci.yml`, not just what the pre-commit hook covers:

```sh
go build ./...
go test -race ./...
go mod tidy && git diff --exit-code -- go.mod go.sum
go run ./cmd/autogit schema | diff -u schema/config.schema.json -
golangci-lint run
gitleaks detect --redact --no-banner
```

The lefthook gates run a subset — gofumpt, `go vet`, `golangci-lint --fast-only`, gitleaks on commit; `go test` without `-race` and the tidy check on push — so a clean commit says nothing about the pipeline.

## 3. Open the PR

```sh
gh pr create --title "<type>: <what changed>" --body "..."
```

Reference a ticket as `Refs #<n>` — **not** `Closes #<n>`. The ticket is closed by the resolution step in `issue-tracker.md`, which posts the answer comment first; a GitHub auto-close on merge would skip that comment.

## 4. Run both reviews, then fix once

Launch both in the same turn, in report mode:

- `/code-review <PR#>` — correctness bugs, reuse and simplification, against the PR diff. Pass no `--fix` here.
- `/mattpocock-skills:code-review main` — the Standards and Spec axes; Spec reads the ticket the PR references.

Wait for both reports before touching the tree. Applying fixes while the other review is still reading the diff makes its findings stale — that is why the built-in review runs without `--fix` in this flow.

Then fix in one pass: commit on the same branch, push, re-run the gates from step 2. A finding you disagree with gets a PR comment saying why, not silence.

## 5. Merge and clean up

Merge once CI is green:

```sh
gh pr merge <n> --squash --delete-branch
git worktree remove .claude/worktrees/<slug>
```

Squash is what this repo's history uses — `feat: add post-install hook to remove gatekeeper quarantine (#1)`.

Only then resolve the ticket per the Resolve step in `issue-tracker.md`.
