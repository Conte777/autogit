# Context

Glossary for autogit. Use these words in issues, commits and code; the
alternatives listed under each are the ones to avoid.

## Prepared message

The commit message **git itself wrote to disk** before autogit was invoked, and
which git's own `commit` would use if the user typed it. It lives in
`$GIT_DIR/MERGE_MSG` for a merge, a cherry-pick and a revert, and in
`$GIT_DIR/SQUASH_MSG` for `git merge --squash`. `Merge branch 'x'` is the
canonical example.

A prepared message is not a draft and not a suggestion: `git log --merges` and
changelog tooling read its shape, and a cherry-pick's message belongs to the
original author. Nothing rewrites it.

The file is the authority, not the ref beside it. A clean `cherry-pick -n`
writes `MERGE_MSG` and leaves no `CHERRY_PICK_HEAD`, so a message with no ref
of its own is still a prepared message — the `prepared` operation.

Not "the default message" — that is `commit.template`, which is a blank form
the user fills in, and which autogit does not treat as prepared.

## Passthrough

Committing a prepared message **verbatim**: no provider call, no preset
validation, no attempt counter. The word names the whole path through
`App.Commit`, and `CommitResult.Prepared` names the operation that triggered
it. Governed by the `preparedMessage` config key, on by default.

Passthrough is a substitute for generation, never a fallback from it — a
generation that failed is an error, not a reason to reach for git's message.

## In-progress state

A multi-step git operation the working tree is in the middle of, read from the
git directory by `Repo.State` and typed as a `git.Operation`: merge, squash
merge, cherry-pick, revert, rebase, bisect, and `prepared` for a message whose
operation left no ref to name it.

The state itself carries no policy. Two questions are asked of it separately:
`Blocked()` — may autogit commit here at all? — and `HasPreparedMessage()` —
has git already written what this commit should say? A rebase blocks because
only `git rebase --continue` can advance the sequencer's todo list. A squash
merge blocks nothing: it leaves no ref behind and git considers the tree
ordinary.
