# Code-Change Workflow

Binding for any agent session that changes code. Code never lands on `main` as a direct commit: it arrives as a reviewed, merged pull request. `main` is protected.

Code means anything under `cmd/`, `internal/`, `assets/`, `schema/`, or the build and CI files that gate them (`.golangci.yml`, `.goreleaser.yaml`, `lefthook.yml`, `.github/workflows/`). A change touching only prose — README, `CONTEXT.md`, `docs/`, ADRs, issue bodies — is out of scope here and commits directly; it rejoins this workflow the moment it ships alongside code.

## 1. Claim the ticket, then work in a worktree

A change that answers a ticket starts by claiming it — `gh issue edit <n> --add-assignee @me`, per _Taking an issue into work_ in `issue-tracker.md`. The tracker shows who the work belongs to from the moment it starts, not from the moment the PR appears.

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

## 5. Merge, clean up, close the ticket

This step belongs to the task, not to the user. A green PR left unmerged, or a merged PR whose ticket is still open, is unfinished work — do not stop here to hand it over or to ask for permission. Ask only when a command is actually refused, and say which one.

Merge once CI is green:

```sh
gh pr merge <n> --merge --delete-branch
git worktree remove .claude/worktrees/<slug>
```

A merge commit is what this repo's history uses — `Merge pull request #10 from Conte777/feat/centralize-provider-config`. The branch's own commits stay in the history, so each one has to read as a commit, not as a checkpoint. `gh pr merge` fails its local cleanup when `main` is checked out in another worktree; the merge itself still happened, so confirm with `gh pr view <n> --json state` and finish the pruning by hand rather than retrying the merge.

Only then resolve the ticket per the Resolve step in `issue-tracker.md`: the answer comment first, then `gh issue close`. Both, in that order, every time — an auto-close from the PR body is what `Refs #<n>` exists to prevent.
