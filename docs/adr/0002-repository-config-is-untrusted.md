# A repository config file is untrusted input

`.autogit.json` arrives with a clone, so autogit treats it as input from a
stranger: it may set only `preset`, `presets.*`, `protectedBranches`, `confirm`
and `diff.*`. `provider` and `providers.*` stay global-only because
`providers.claude-cli.binary` from someone else's repository would be arbitrary
code execution on the first commit, and `baseUrl` would quietly redirect the
prompt — and the API key — to a collector. Unknown keys are a startup error at
every layer rather than a warning, because a typo in `protectedBranches` that is
merely ignored switches branch protection off in silence.

The same reading applies to the values of the keys it may set, not only to the
keys themselves: a prompt path inside `presets.*` names a file autogit reads and
sends to the model, so a repository layer resolves it inside the worktree and
rejects an absolute path, a `~`, an escape through `..` and a symlink leaving
the worktree, at resolution time.
The global config is the user's own file and keeps the run of the disk.

## Consequences

The repository layer decodes into its own whitelist type, not into `Config`.
A new field is reachable from a repository file only when it is added there
explicitly, which is the intended default: opt in per field, after asking what
a hostile value of it would do.

The cost is that a team cannot standardise its model or endpoint through the
repository — that belongs in each developer's global config. Accepted: the
alternative trades a supply-chain hole for a convenience.
