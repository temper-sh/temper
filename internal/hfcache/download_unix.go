//go:build darwin || linux

package hfcache

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func runDownload(command *exec.Cmd) error {
	// uv may spawn Python. Cancellation owns that whole process group so a
	// canceled preparation cannot leave its download running in the background.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	command.WaitDelay = 2 * time.Second
	return command.Run()
}
