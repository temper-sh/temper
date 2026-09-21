package probecmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"
)

// ProcessRunner owns exactly one foreground process group. Cancellation asks
// the router and any child engine to terminate, then os/exec enforces a bound.
type ProcessRunner struct{}

func (ProcessRunner) Run(ctx context.Context, invocation Invocation, stdout, stderr io.Writer) error {
	if invocation.Supervision != nil {
		return runSupervised(ctx, invocation, stdout, stderr)
	}
	command := exec.CommandContext(ctx, invocation.Path, invocation.Arguments...)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Stdin = bytes.NewReader(invocation.Input)
	command.Env = append([]string(nil), invocation.Environment...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	command.WaitDelay = 15 * time.Second
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("probe process exited: %w", err)
	}
	return nil
}
