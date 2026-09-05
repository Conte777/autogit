package cli

import (
	"context"
	"testing"
)

func TestUnknownCommandExitsUsage(t *testing.T) {
	for _, name := range []string{"install", "uninstall", "bogus"} {
		t.Run(name, func(t *testing.T) {
			root := Root(Version{})
			root.SetArgs([]string{name})
			root.SetOut(discard{})
			root.SetErr(discard{})

			err := root.ExecuteContext(context.Background())
			if err == nil {
				t.Fatalf("%q is still a command", name)
			}
			if got := ExitCode(err); got != ExitUsage {
				t.Errorf("ExitCode() = %d, want %d (%v)", got, ExitUsage, err)
			}
		})
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
