# The branch slug always comes from the model

`autogit branch` never turns a description into a slug itself. Whatever the user
types is a task handed to the model, and the name that comes back is the branch
name.

Before this, a description took a different route from a diff. `validate.Slugify`
lowercased the text, replaced every non-`[a-z0-9]` run with a hyphen and kept the
first four words, and that was the slug. With a ticket the model was not called
at all; without one it was called for the type only, and its slug was discarded.

## Why the mechanical route had to go

The four-word cut was silent. `autogit branch CUS-1 fix the flaky retry logic in
provider` produced `CUS-1/fix-the-flaky-retry`, and nothing said the rest had
been dropped — the name simply came out shorter than the sentence that asked for
it.

Character classes made it worse for anyone not writing in English. `Slugify`
deletes everything outside `[a-z0-9]`, so a Cyrillic description reduced to the
empty string and the command failed with `no description and no changes to
describe` — a message about missing input, for input that was there.

And the intent was never "type the slug yourself". The description says what the
work is; naming it is the model's job — the same job it already does when there
is no description and only a diff to read. Two routes to one name meant two
behaviours to explain, and the mechanical one was the worse of the pair.

## What was given up

The offline path. There is no longer any way to reach a branch without calling a
provider: a ticket plus a description used to name the branch with no network at
all, and now it does not. That is the trade — the cost of a call, paid every
time, for a name that reads like a name and carries the whole description.

No flag reopens the old route. A flag would restore both behaviours and both
explanations, which is what this decision exists to remove.

## Consequences

`maxWords` is gone from `BranchFormat`. Config keys are strict, so a file still
carrying it is a startup error rather than a key quietly ignored — a breaking
change taken deliberately in the release after `v0.2.0`, because accepting a
word limit that no longer limits anything is the same gap between word and deed
that prompted the change.

An ejected prompt breaks the same way, and less legibly. `autogit preset eject`
copies `branch.md` verbatim into the repository, so a tree that ejected before
this change still holds `{{.MaxWords}}`, and `prompt.Parse` dry-runs every
template against an empty `BranchData` with `missingkey=error`: `autogit branch`
then fails at prompt load with `can't evaluate field MaxWords in type
prompt.BranchData`. The fix is to re-eject, or to edit the two lines by hand.
Only `branch` is affected — the branch template is loaded inside `askBranch`, so
`autogit commit` keeps working.

`maxSlugLen` is the only remaining constraint on a slug, and the branch prompts
now state it: the model is told the character budget it has to fit.

A model that answers with spaces instead of hyphens is joined, not rejected.
`branchValidator.Check` hyphenates the fields before handing them on, the way
its own `<type> <slug>` arm already did; spending another provider round-trip on
whitespace would buy nothing.

The diff is not read when a description is present. `autogit branch` with a
description is typed before the edits exist.
