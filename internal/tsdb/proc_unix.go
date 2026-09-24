//go:build !windows

package tsdb

import (
	"os"
	"os/exec"
	"syscall"
)

// TSDB запускается в своей группе процессов, чтобы при зависании убить её целиком.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// В контейнере TSDB завершается вместе с главным процессом, отдельная привязка не нужна.
func bindToParent(*os.Process) {}

func stopProcess(p *os.Process) {
	_ = p.Signal(syscall.SIGTERM)
}

func killProcessGroup(p *os.Process) {
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
}
