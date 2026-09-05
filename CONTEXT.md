# autogit

autogit asks a model for a commit message or a branch name, checks the answer
against rules the user controls, and only then touches the repository. This
glossary fixes the words for the format being enforced, the loop that produces
an answer, and the ways a request reaches that loop.

## Format

**Preset**:
A named pair of formats — one for commits, one for branches — that the user
selects by name and may override field by field.
_Avoid_: profile, style, template

**Format**:
The configurable description of one kind of output: which prefixes are allowed,
how long the subject may be, whether a body is wanted.
_Avoid_: config, schema, spec

**Rules**:
What a format becomes once the repository is known — the format plus the current
branch and the scopes mined from history. Rules are what an answer is checked
against; a format alone cannot check anything.
_Avoid_: constraints, checks, policy

**Prefix**:
Whatever stands before `:` in a subject, or before `/` in a branch name. A Type
and a Ticket are the two kinds of prefix.
_Avoid_: kind, tag, label

**Type**:
A prefix drawn from a fixed vocabulary, such as `feat` or `fix`.
_Avoid_: category, kind

**Ticket**:
A prefix that identifies an issue in a tracker, recognised by a pattern rather
than by a list.
_Avoid_: issue, task id, story

**Scope**:
The optional area named in parentheses after the prefix.
_Avoid_: area, module, component

**Subject**:
The first line of a commit message.
_Avoid_: title, headline, summary

**Description**:
The human-readable phrase naming what changed — the text after `": "` in a
subject, and the same phrase when a user hands it to `autogit branch`.
_Avoid_: summary, message text

**Slug**:
A description reduced to the hyphenated form a branch name can carry.
_Avoid_: kebab, handle

**Body**:
The paragraphs of a commit message below the subject.
_Avoid_: description, details

**Footer**:
A `Key: Value` trailer at the end of a commit message.
_Avoid_: trailer, metadata

## Generation

**Provider**:
An adapter that opens sessions with one model, whether over HTTP or by running
a local binary.
_Avoid_: backend, client, driver

**Session**:
One dialogue with a model. Every correction for a single answer belongs to the
same session.
_Avoid_: conversation, chat, connection

**Candidate**:
Raw model output that has not been checked yet.
_Avoid_: answer, output, response

**Validator**:
What turns a candidate into its canonical form and lists the problems with it.
_Avoid_: checker, linter

**Problem**:
One reason a candidate was rejected, phrased so the model can act on it.
_Avoid_: error, violation, issue

**Attempt**:
One turn sent and one candidate received.
_Avoid_: retry, iteration, round

**Correction**:
The turn sent after a rejected candidate, listing its problems.
_Avoid_: retry, feedback, repair

## Repository

**Diff**:
What the model is shown of a change: a complete list of the changed files, plus
diff text that may be cut short.
_Avoid_: patch, changeset

**Stage mode**:
What the user asked to be put in the index before generating — only what is
already staged, everything, or tracked files.
_Avoid_: add mode, staging strategy

**Protected branch**:
A branch matching a configured glob, where committing requires an explicit
override.
_Avoid_: main branch, locked branch

**Global config**:
The user's own config file. The only place that may choose a provider.
_Avoid_: user config, home config

**Repository config**:
The config file carried by a cloned repository. Untrusted: it may shape the
format and nothing else.
_Avoid_: project config, local config

## Surfaces

**Surface**:
The way a request reaches generation — the CLI, the MCP server, or the Claude
Code hook. It decides whether the user can be asked anything. `install` and
`doctor` are commands, not surfaces.
_Avoid_: entry point, frontend, interface

**Operation**:
Commit or Branch. There are two.
_Avoid_: command, action, mode

**Preview**:
The mode of Commit that stops once the message exists, without creating a
commit. Spelled `commit-msg` and `--dry-run` on the CLI, `dryRun` over MCP.
_Avoid_: dry run, simulation
