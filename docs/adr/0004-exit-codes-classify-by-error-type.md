# Exit codes classify by error type, and cancellation is answered first

`ExitCode` maps an error onto a public exit code. It used to do that partly by
type and partly by matching stdlib sentinels through whichever wrapper the lower
packages happened to build, and one whole type — `git.ExecError` — was not
matched at all, so a pre-commit hook that refused the work exited 1, "autogit is
broken", rather than 3, "the repository refused".

Three decisions replace that.

**A failed git command is a repository failure (3).** A hook that declined, a
failed switch, an unwritable index: git ran and said no. Code 3 already means
"not a repository, or a state that blocks committing", and a refusing git
belongs to the same sentence. Code 1 goes back to meaning a bug in autogit.

**A git timeout is also 3, not 6.** The hanging case is a signing setup waiting
on pinentry, which is local git, not the model. It exited 6 only because
`git.run` wraps `ctx.Err()` and `ExitCode` reached through that wrapper for
`context.DeadlineExceeded`. Nothing else produced a bare deadline: every
provider failure, timeout included, already arrives as a `gen.ProviderError`
(`gen.Generate` wraps both `start` and `send`), so code 6 still covers a model
that timed out, and the sentinel branch was dead once the types were matched.

**Cancellation outranks classification.** Ctrl-C is answered before any layer is
identified, so it is 130 wherever it lands — inside a git call, inside a request
to the model, or at a prompt. Previously `ProviderError` was matched first and
an interrupted request exited 6, while an interrupted git call exited 130; the
class of the operation the signal happened to interrupt is not information the
caller asked for.

`context.Canceled` therefore stays matched through wrappers, deliberately, and
it is the only sentinel that is. It answers "was this interrupted", not "which
layer failed", and every layer propagates it by the language's own convention.
Marking cancellation on `git.ExecError` and on `gen.ProviderError` instead would
be two fields in two packages restating what the stdlib already guarantees.

An unbuildable provider — unknown name, missing API key — is a `config.Error`
now and exits 8. It carried a `cli`-local wrapper type whose comment promised
exit 8, but no branch matched that type, so it exited 1.

## Consequences

The exit code changes for four cases that scripts can branch on: a failing git
command 1 → 3, a git timeout 6 → 3, an interrupted provider call 6 → 130, an
unbuildable provider 1 → 8. The table in `README.md` is the contract and was
updated with them.

Every code in that table is reachable from a test that builds the error the way
its producing package builds it — `internal/cli/exit_test.go` runs a real
failing command for the `ExecError` case rather than inventing a plausible
inner error. A new error type added anywhere below `cli` must be classified in
`ExitCode` or it silently becomes an internal error.
