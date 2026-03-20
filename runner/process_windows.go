package runner

import (
	"fmt"
	"os/exec"
	"syscall"
)

// setProcGroup sets up the process to be created in a new process group on Windows.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// killProcessGroup kills the entire process tree on Windows using taskkill.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// taskkill /F /T kills the process and all its children
	kill := exec.Command("taskkill", "/F", "/T", "/PID",
		fmt.Sprintf("%d", cmd.Process.Pid))
	return kill.Run()
}
