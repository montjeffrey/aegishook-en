//go:build windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// cliProcessTree ends the CLI and its helper processes on timeout. taskkill /T
// walks the tree from the CLI's exact PID; if it cannot run, the CLI alone is
// killed.
func cliProcessTree(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		kill := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe"), "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if kill.Run() != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
