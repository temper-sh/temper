package probecmd

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// ProcessRunner owns exactly one supervised foreground process group.
// Cancellation asks the router and any child engine to terminate within a bound.
type ProcessRunner struct{}

func (ProcessRunner) Run(ctx context.Context, invocation Invocation, stdout, stderr io.Writer) error {
	if invocation.Supervision == nil {
		return errors.New("a foreground process requires supervision")
	}
	return runSupervised(ctx, invocation, stdout, stderr)
}

// Serve plans one supervised foreground router from exact identities and runs
// it until it stops or ctx ends. Planning refuses before any process effect.
func Serve(ctx context.Context, runner Runner, options Options, supervision Supervision, stdout, stderr io.Writer) error {
	invocation, err := Plan(options)
	if err != nil {
		return err
	}
	invocation.Supervision = &supervision
	fmt.Fprintf(stdout, "RESULT probe-serve starting installation=%s generation=%s listen=%s\n", options.Installation, options.Generation, options.Listen)
	if err := runner.Run(ctx, invocation, stdout, stderr); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "RESULT probe-serve stopped installation=%s generation=%s listen=%s\n", options.Installation, options.Generation, options.Listen)
	return nil
}
