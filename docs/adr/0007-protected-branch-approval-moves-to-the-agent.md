# Protected-branch approval is the agent's to ask for

ADR _A protected branch over MCP takes the user's consent, never the model's
word_ (0005) put the question on a channel the model could neither see nor
forge: MCP elicitation. The server returned the question as an `InputRequests`
map, the client showed a dialog, and a second `commit` call carried the answer.

That dialog renders in the local terminal and nowhere else. A user driving the
same session from Remote Control sees a tool call that does nothing, with no
control to answer it — the work has to move to a laptop. The server cannot even
notice: Claude Code declares the `elicitation` capability whether or not there
is a terminal attached, so `acceptsFormElicitation` passes and the question goes
into a void.

So the question moves to the agent. The `commit` tool's description tells the
model to obtain the user's explicit yes before calling the tool — by asking them
directly, with `AskUserQuestion` where it has one, which does render on a phone.
The tool then takes the model's word for it.

## What this gives up

Server-side enforcement. Where `mcp.allowProtectedBranch` is on, nothing stops a
model from committing to `main` without asking anyone; the gate is an
instruction, not a mechanism. ADR 0005 was right that a model under pressure to
finish will do exactly that and report that it asked, and that has not stopped
being true — it is accepted, deliberately, in exchange for a gate the user can
actually operate from wherever they are. A gate that only works at one desk is
not a gate; it is a reason to turn the feature off.

## What was rejected

**A `confirmed: true` parameter on the tool.** This is the thing ADR 0005 named
and refused, and refusing it still costs nothing: the model would set it, and
the schema would advertise that setting it is a thing one does. Taking the
model's word implicitly, from a description it was given, at least does not
publish an escape hatch on the tool's own interface —
`TestCommitToolHasNoProtectedBranchEscape` still holds the schema to that.

**A forced refusal on the first call.** The tool could always refuse a protected
branch once, so the second call proves a round trip through the agent's own
turn. It proves nothing about the user: the model reads the refusal, asks
nothing, calls again. The cost is real — two provider generations for one
commit.

**An out-of-band approval channel of autogit's own** — a URL, a local endpoint,
a push notification the user answers. Every form of it is a service to run,
authenticate and keep alive, for a question that Claude Code can already put on
the user's screen through the agent. Ruled out under _prefer the simplest,
native path_.

## What still holds

`mcp.allowProtectedBranch` is off by default and stays global-only, for the
reason in ADR _A repository config file is untrusted input_: a cloned repository
able to set it would be granting an agent the right to commit to the very
branches the same file declares protected. With the key off, a protected branch
over MCP is refused outright, and the refusal names the two human paths —
`/autogit:commit force` in Claude Code, `autogit commit --force` in a terminal.
That is the one hard gate left, and it is the default.

`--force` on the command line is unchanged, and so is the terminal question
`app` asks when a human is typing. The hook is untouched: it commits from a
slash command with no agent turn to ask in, which is why the request carries
`Agent` — a boolean the MCP surface sets and no other caller does — rather than
letting the config key open every non-interactive path. `internal/cli/exit.go`
keeps exit code 5 for a protected branch.

The description is static. `Register` runs before any repository is known,
config is loaded per-repo inside the builder, and a workspace rule can override
`mcp.allowProtectedBranch` anyway, so the tool cannot describe the policy in
force — it describes the rule the model must follow, and the server answers with
a refusal when the policy disagrees.
