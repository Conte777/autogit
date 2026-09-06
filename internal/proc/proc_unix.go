//go:build unix

package proc

import (
	"os"
	"os/exec"
	"syscall"
)

// Isolate puts the child in its own process group. Call it before Start.
func Isolate(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Kill terminates the child's whole process group, falling back to the child
// alone where the group is already gone. It reports os.ErrProcessDone when
// there is nothing left to signal, which is what exec.Cmd.Cancel expects.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	// A reaped pid can already belong to somebody else's group.
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return err
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
