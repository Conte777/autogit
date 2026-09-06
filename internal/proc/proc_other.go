//go:build !unix

package proc

import (
	"os"
	"os/exec"
)

// Isolate is a no-op where process groups are not a thing.
func Isolate(*exec.Cmd) {}

// Kill terminates the child. It reports os.ErrProcessDone when there is
// nothing left to signal, which is what exec.Cmd.Cancel expects.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}
