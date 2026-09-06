# A protected branch over MCP takes the user's consent, never the model's word

The MCP `commit` tool used to refuse a protected branch outright. The refusal
was right about the threat and wrong about the remedy: it left the user with no
way to say yes without leaving the agent, and an agent that has to stop and ask
for a command typed elsewhere is an agent that stops.

The threat has not changed. A tool parameter — `allowProtectedBranch: true`,
however sternly documented — is a switch the model itself sets, and a model
under pressure to finish will set it and report that it asked. So the tool's
argument object still carries nothing about protection, and the server decides
on its own to ask.

**Consent** is the answer to that question when it comes back over a channel the
model neither sees nor can forge: MCP elicitation. The server returns the
question as an `InputRequests` map (SEP-2322); the client puts it to the user,
and calls `commit` again carrying the answer. `--force` on the command line and
Consent over MCP are the two forms of the same thing — what lifts a branch's
protection — and both are a human, never a model.

## What was decided, and against what

**Elicitation over `_meta: anthropic/requiresUserInteraction`.** That flag makes
Claude Code confirm every call of the tool, including in `bypassPermissions`,
which is stronger. But it is a property of the tool, not of the call: it cannot
be raised only for a protected branch. Confirming every ordinary commit in a
feature branch is the thing autogit exists to avoid, and a confirmation asked
twenty times a day stops being read.

**`InputRequests` over a direct `ServerSession.Elicit`.** Protocol revision
2026-07-28 forbids a server-initiated request while a tool call is in flight,
and the Go SDK enforces it. Returning the question as `InputRequests` is the one
form that works on both sides of that line: newer clients fulfil it and retry,
and for an older client the SDK's own server middleware sends
`elicitation/create` and re-invokes the handler with the answer. Writing the
direct call instead would have worked with today's Claude Code and silently
degraded to a refusal the day it negotiates the newer revision.

**Asked before the message is generated.** `checkProtected` keeps its place
ahead of staging and ahead of the provider, so a refusal costs no tokens and
leaves the index untouched. The user is therefore asked about the branch, not
about the contents of the commit — which is what the question is: permission to
write to `main` at all.

**Off unless the global config turns it on**, as `mcp.allowProtectedBranch`. A
repository `.autogit.json` cannot see the key, for the reason in ADR
_A repository config file is untrusted input_: a cloned repository able to set
it would be granting an agent the right to commit to the very branches the same
file declares protected. With the key off the server passes no consent channel
and the behaviour is what it always was.

**Consent lasts until the work leaves the branch.** It is held per repository in
the running server, keyed by the branch it was given for, and a commit landing
anywhere else drops it. Asking again for every commit in one editing session
teaches the user to click yes without reading; remembering the answer for the
whole session turns "the user allowed this commit" into "the user once allowed a
mode". Expiring on a branch change keeps it to a single episode of work.

A refusal is never remembered. The two are not symmetric: a stuck yes quietly
lets commits into `main`, a stuck no only inconveniences.

## Consequences

The call no longer waits for the user, so nothing is held while the question is
open: the per-repository lock is taken, released with the first result, and
taken again by the retry. Between the two, another call could commit — but
consent names the branch it was granted for and the retry re-reads the current
branch, so a consent cannot be spent on a branch it was not given for.

The `commit` handler is now re-entrant by design. Its first pass must reach the
protected-branch check without side effects, which the existing order already
guaranteed and which the tests now hold it to.

Three failures stay worded apart, because they ask different things of the
reader: the key being off points at the human paths, a client that cannot
elicit says so rather than pretending the user declined, and a declined or
dismissed question is reported as the user's own answer, with an instruction not
to retry it and not to reach around autogit.

A server serving more than one session at once would have to key consent by
session rather than by repository root. `autogit mcp` is stdio — one process,
one session — so the two are the same set today, and this is the thing to change
first if that ever stops being true.
