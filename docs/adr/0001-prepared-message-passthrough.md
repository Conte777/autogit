# 1. Commit git's prepared message verbatim

Date: 2026-09-05

Status: accepted

## Context

Git prepares a commit message itself in four situations: a merge, a
`merge --squash`, a cherry-pick and a revert. In every one of them the right
text is already on disk — `MERGE_MSG` or `SQUASH_MSG` — before autogit runs.

Until now autogit handled none of them well. `Repo.CheckState` refused a merge,
a cherry-pick and a revert outright, so a user who resolved a conflict could not
finish the merge through autogit at all. `merge --squash` writes no ref, so the
check saw nothing and autogit generated a message straight over the one git had
prepared. That is the case where the damage was real rather than merely
inconvenient.

## Decision

When git has prepared a message and the operation is one of those four, commit
that message verbatim. Rebase and bisect keep refusing.

### Verbatim, not rewritten

The message is used exactly as git wrote it, comments stripped. The provider is
never called and the preset validator never runs.

Rewriting was the obvious alternative: `Merge branch 'side'` is not a
conventional commit, and a repository that enforces the `conventional` preset
now contains a subject that does not match it. We rejected it twice over.

`Merge branch 'x'` is a **convention that tooling reads** — `git log --merges`,
release-note generators, and every reviewer who has ever scanned a history for
where a branch landed. A cherry-pick's or revert's message is the original
author's, and rewriting it silently breaks `git log --grep` against the commit
it came from.

Running the validator would be worse than useless: no conventional-commits rule
would ever accept `Merge branch 'side'`, so every attempt would burn and the
command would exit 7 on a merge the user had already resolved correctly. A
validator that can only fail is not a check, it is a wall.

The cost we accept: a history where merge commits do not match the preset. That
is what git's own history looks like, and it is the shape the ecosystem expects.

### autogit reads the file, rather than `git commit --no-edit`

`git commit --no-edit` would pick up `MERGE_MSG` on its own and is one flag.
We read the file ourselves and pass the text to the existing
`Repo.Commit(ctx, msg)` instead.

The reason is `commit-msg`. Its whole contract is that the preview cannot differ
from what would land, because it runs the same code path. With `--no-edit` the
message would materialise inside git, after the point where autogit could show
it, and `autogit commit-msg` mid-merge would have nothing to print. Reading the
file keeps one code path and one set of bytes.

This has a consequence worth naming: the `# Conflicts:` block has to be removed
at read time, so that the preview and the commit see the same text.

Removing it with `git stripspace --strip-comments` is the obvious way and is
wrong. For a merge the message is git's own one-liner and nothing is lost, but
for a cherry-pick or a revert `MERGE_MSG` **is the original author's whole
commit message**, and every body line starting with `#` is content. A commit
whose body reads `Refs:` / `#123 the ticket` / `#456 another` comes out as bare
`Refs:` — both references silently deleted. That is the same mistake
`Repo.Commit` already avoids by passing `--cleanup=whitespace` instead of the
default.

So only git's own conflict listing is cut: everything from the `# Conflicts:`
header to the end, matched loosely enough to survive a non-default
`core.commentChar`. Nothing else in the message is touched.

### The message file is the authority, not the ref

The obvious detection is by ref: `MERGE_HEAD`, `CHERRY_PICK_HEAD`, `REVERT_HEAD`.
It is not enough, and git is not symmetric about it. A clean `revert -n` leaves
`REVERT_HEAD`; a clean `cherry-pick -n` leaves **only** `MERGE_MSG`. Detecting by
ref alone would miss the pick and replace the original author's message with a
generated one — the same failure as the squash case, one branch further along.

So the search ends on a bare `MERGE_MSG`, reported as the `prepared` operation.
That is safe because a commit consumes the file: `git commit` removes both
`MERGE_MSG` and `SQUASH_MSG`, and an ordinary commit never writes either. The
staleness window is the same one already accepted for `SQUASH_MSG`, and the
label in the output is what makes any reuse visible.

`MERGE_MSG` is looked for last, after every real in-progress operation, so it
never masks one.

The bare state is the weakest signal we act on, and it is worth being explicit
about what it costs. After `cherry-pick -n`, a user who keeps working and then
commits gets their unrelated changes under the picked commit's message. We
accept that because it is precisely what `git commit` does in the same
situation — the alternative is autogit second-guessing git about the user's own
`--no-commit`. The label in the output is what makes the reuse visible.

### The empty-index check is waived for a merge only

`git merge -s ours` legitimately produces a commit whose tree equals HEAD's.
Refusing it for an empty diff would strand the user in a merge autogit cannot
finish. Squash, cherry-pick and revert always stage something, so they keep the
check.

### A single pick passes through; a sequence does not

`git cherry-pick A B C` runs through `.git/sequencer`, and only
`cherry-pick --continue` advances its todo list. Committing the conflicted step
with `git commit` lands that one commit and leaves the remaining picks
unapplied, with no ref left to show for it — `State` would report the repository
clean and the user would be told the pick succeeded. This is the rebase argument
exactly, so a sequence blocks for the same reason.

A single-commit pick or revert never creates the sequencer, and stays on the
passthrough path.

### Unresolved conflicts are a hard error, before staging

`autogit commit --all` runs `git add -A`. Against a half-resolved merge that
stages files full of conflict markers. The check runs before the index is
touched, and unconditionally — **not** only on the passthrough path.

Gating it on passthrough would leave a hole: a conflicted `merge --squash` has
no ref, so nothing blocks it, and with `preparedMessage: false` the commit would
sail through with the markers in it. Since `preparedMessage` is settable from a
repository `.autogit.json`, that hole would be reachable from a cloned repo.
A guard against committing garbage must not depend on configuration.

This is the one place the passthrough work changes behaviour with
`preparedMessage: false` — a conflicted squash used to commit and now refuses.
Committing conflict markers was a bug, not behaviour worth preserving.

## Consequences

- Merge, single-commit cherry-pick and revert stop being refused;
  `merge --squash` stops being overwritten. Rebase, bisect and multi-commit
  sequences keep refusing, all for the same reason: only `--continue` can
  advance a todo list, and a commit from autogit would not.
- `Repo.CheckState` is gone, replaced by `Repo.State` returning a typed
  `git.State`. The block-or-pass policy moved into `internal/app`, where the
  configuration that governs it already lives.
- `preparedMessage: false` reproduces the old behaviour, including the old
  refusals, with the single exception noted above. It is settable in a
  repository `.autogit.json`: it changes only which text a commit carries, which
  is the same class of decision as `preset`. Nothing that guards the working
  tree hangs off it.
- A protected branch still needs `--force`. A merge into `main` is exactly the
  case the protection exists for.
