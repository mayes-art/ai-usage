//go:build !windows

package desktop

import "os/exec"

func ConfigureBackgroundProcess(cmd *exec.Cmd) {}
