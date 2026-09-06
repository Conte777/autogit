//go:build !unix

package proc

import (
	"os"
	"os/exec"
)

// Isolate is a no-op where process groups are not a thing.
func Isolate(*exec.Cmd) {}

// Kill terminates the child alone: there is no group to take with it.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}
