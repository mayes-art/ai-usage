package main

import (
	"os/exec"
	"syscall"
)

// Prevent console allocation for the npm shim and the CLI processes it starts.
// Redirected stdin/stdout still carry the app-server protocol normally.
func configureBackgroundProcess(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
