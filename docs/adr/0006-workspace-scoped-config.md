# Settings scoped to a directory live in the global file

`workspaces` is a list of rules in `~/.config/autogit/config.json`. Each carries
a `path` and any key the global file itself may carry, and applies to every
repository under that path:

```json
"workspaces": [
  { "path": "~/Work/friday-releases", "preset": "ticket" }
]
```

The need is one machine holding trees with different conventions: everything
under `~/Work/friday-releases` takes `CUS-1234: subject`, everything else takes
full Conventional Commits. A repository file cannot express it — there are 164
separate repositories there, and dropping an `.autogit.json` into each is a
change to 164 histories that belong to other people. An environment variable
could, but it would be invisible: nothing would tell you which convention is in
force until a message came out in the wrong shape.

## The layer is trusted

`0002-repository-config-is-untrusted.md` withholds `provider`, `providers.*`,
`mcp` and two `diff` keys from a repository file, because that file arrives with
a clone. This one does not arrive with anything: it is a list inside the user's
own global config, which nothing but the user writes. A rule therefore decodes
into `Config` itself rather than a whitelist type, prompt paths in
`presets.*` resolve against the global file's directory and are not confined to
it, and `workspaces` is itself global-only — a repository file that could
declare one would have written its way past the whitelist in a single key.

Rules do not nest: a rule inside a rule would be a second, path-dependent
ordering to reason about, and it buys nothing that a second top-level rule
naming the deeper path does not.

## It sits under the repository layer

The order is `Default() → global → workspaces → repository → environment →
flags`. The repository file stays the last file read even though `workspaces` is
the more specific layer, because "later is more specific" is not the only rule
in play: the repository layer is already the narrowest scope there is, is
already whitelisted down to formatting, and a repository that declared its own
convention would be overruled by a directory rule that never heard of it.

## Matching is on the repository root, per segment

The rule is compared against `Options.RepoRoot`, not the working directory. Over
MCP the working directory is wherever the editor was started and does not change
for the session, while the repository arrives with every call — matching on cwd
would pin one convention to a whole session.

Comparison is per path segment, so `~/Work/friday` does not cover
`~/Work/friday-releases`; `strings.HasPrefix` would, and would do it silently.
Both sides are also compared with symlinks resolved, because a rule may name a
directory that does not exist yet while the repository path reaches the same
place through a link — and the depth that orders the matches is then taken from
the form that matched, so a shallow directory reached through a long symlink
does not sort as the more specific rule. On macOS segments compare
case-insensitively, following APFS: a rule written `~/Work` has to cover the
path the shell hands over as `~/work`.

A `path` is expanded (`~`) and made absolute; a relative one resolves against
the directory of the global config file, the same base a prompt path in
`presets.*` already uses.

## Consequences

Every rule is decoded whether it matches or not, so a typo in a rule that is
dormant on this machine is still a startup error — the same reading the rest of
the config gets.

Overlapping rules all apply, sorted by how deep the rule's own path is, so a
directory refines its parent. Depth, not declaration order, decides; ties keep
file order. `autogit doctor` prints the rules that matched, because a setting
that changes the output of every commit must not be one nobody can see.
