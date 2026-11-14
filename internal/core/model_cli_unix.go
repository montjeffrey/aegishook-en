//go:build !windows

package core

import (
	"os/exec"
	"syscall"
)

// cliProcessTree puts the CLI in its own process group and ends the whole
// group on timeout, so helper processes do not outlive the review.
func cliProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
