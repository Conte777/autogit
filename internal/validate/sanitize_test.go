package validate

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "feat: add thing", "feat: add thing"},
		{"code fence", "```\nfeat: add thing\n```", "feat: add thing"},
		{"tagged fence", "```text\nfeat: add thing\n```", "feat: add thing"},
		{"wrapping double quotes", `  "feat: do x"  `, "feat: do x"},
		{"wrapping single quotes", `'feat: do x'`, "feat: do x"},
		{"apostrophe survives", "fix: don't crash", "fix: don't crash"},
		{"inner quotes survive", `feat: add "x" flag`, `feat: add "x" flag`},
		{"trailing period is kept for the rules to reject", "feat: add thing.", "feat: add thing."},
		{
			"multiline body survives",
			"feat: add notifier\n\nIt posts to the channel.\nAnd retries.\n\nRefs: CUS-42",
			"feat: add notifier\n\nIt posts to the channel.\nAnd retries.\n\nRefs: CUS-42",
		},
		{
			"blank run collapses to one",
			"feat: add notifier\n\n\n\nBody.",
			"feat: add notifier\n\nBody.",
		},
		{
			"missing blank after subject is inserted",
			"feat: add notifier\nBody starts here.",
			"feat: add notifier\n\nBody starts here.",
		},
		{"per-line trailing whitespace", "feat: add   \n\nbody   \nmore\t", "feat: add\n\nbody\nmore"},
		{"crlf", "feat: add thing\r\n\r\nbody\r\n", "feat: add thing\n\nbody"},
		{"empty", "   \n\n  ", ""},
		{"fenced multiline", "```\nfeat: x\n\nbody\n```", "feat: x\n\nbody"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sanitize(tt.in); got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
