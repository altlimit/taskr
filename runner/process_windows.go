package runner

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// setProcGroup sets up the process to be created in a new process group on Windows.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// gracefulShutdownTimeout is the maximum time to wait for a process to
// exit after receiving the stop signal before escalating to force kill.
const gracefulShutdownTimeout = 5 * time.Second

// signalProcessGroup sends CTRL_BREAK_EVENT to the process group for graceful shutdown.
func signalProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// GenerateConsoleCtrlEvent with CTRL_BREAK_EVENT to the process group
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
	proc, err := dll.FindProc("GenerateConsoleCtrlEvent")
	if err != nil {
		return err
	}
	// CTRL_BREAK_EVENT = 1, send to the process group (PID)
	r, _, err := proc.Call(1, uintptr(cmd.Process.Pid))
	if r == 0 {
		return err
	}
	return nil
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
