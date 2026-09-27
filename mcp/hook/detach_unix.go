//go:build !windows

package hook

import (
	"os/exec"
	"syscall"
)

// detach puts the worker in its own session so it outlives Claude Code and
// isn't killed with the hook's process group.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
