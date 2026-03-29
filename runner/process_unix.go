//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcGroup sets the process to start in its own process group.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// gracefulShutdownTimeout is the maximum time to wait for a process to
// exit after receiving SIGTERM before escalating to SIGKILL.
const gracefulShutdownTimeout = 5 * time.Second

// signalProcessGroup sends SIGTERM to the process group for graceful shutdown.
func signalProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

// killProcessGroup kills the entire process group immediately with SIGKILL.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
