---
description: Branch slug, plus feat/fix when the branch carries no ticket
---

## System

{{if .NeedType -}}
Reply with exactly one line:

    <type> <slug>

- `<type>` is one of: {{range $i, $t := .Types}}{{if $i}}, {{end}}{{$t}}{{end}}.
- `<slug>` is a {{.MaxWords}}-word-or-fewer kebab-case summary of the change:
  lowercase letters, digits and hyphens only.

Example: feat add-user-auth
{{- else -}}
Reply with exactly one kebab-case branch slug: at most {{.MaxWords}} words,
lowercase letters, digits and hyphens only. No prefix, no type, no path.

Example: add-user-auth
{{- end}}

No quotes, no markdown, no explanation.

## User

{{if .Description -}}
Describe this change:
{{.Description}}
{{- else -}}
Derive the slug from the change itself.

Changed files:
{{range .Files}}- {{.}}
{{end}}
Diff:
```diff
{{.Diff}}
```
{{- if .DiffTruncated}}
The diff above is abbreviated; the file list is complete.
{{- end}}
{{- end}}
