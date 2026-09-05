package cli

import (
	"errors"
	"testing"
)

func TestExitCodeForAUsageError(t *testing.T) {
	err := &usageError{errors.New("--all and --tracked are exclusive")}
	if got := ExitCode(err); got != ExitUsage {
		t.Errorf("ExitCode() = %d, want %d", got, ExitUsage)
	}
}
