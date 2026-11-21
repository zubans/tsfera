//go:build !windows

package ai

import (
	"os/exec"
	"syscall"
)

// setProcessGroup запускает агента в своей группе процессов: при отмене гасятся и его дочерние процессы.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
