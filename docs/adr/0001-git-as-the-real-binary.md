# git runs as the real binary, not behind an interface

autogit shells out to the user's own `git` and deliberately exposes no
interface to swap it for a fake. A commit must come out exactly as the user's
own `git commit` would produce it — GPG and SSH signing, `core.hooksPath`,
credential helpers, `gitattributes`, `commit.template` — and only the real
binary honours all of that; a reimplementation or a go-git backend would drift
from it in ways that surface as a broken signature or a skipped pre-commit hook
long after the change that caused them.

## Consequences

Tests create real repositories in temporary directories instead of injecting a
mock. That is slower and it is the intended price: the thing under test is the
interaction with git, so replacing git with a stub would test nothing.

The absence of the interface is load-bearing. Introducing one to make `internal/git`
"testable" reopens exactly this decision.
