//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// setProcGroup sets the process to start in its own process group.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the entire process group.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Kill the entire process group (negative PID)
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
