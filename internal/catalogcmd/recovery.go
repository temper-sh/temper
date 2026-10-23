package catalogcmd

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/temper-sh/temper/internal/clihelp"
	"github.com/temper-sh/temper/internal/probecmd"
)

func verifyStopped(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("temper execution verify-stopped", flag.ContinueOnError)
	f.SetOutput(stderr)
	status := f.String("status-file", "", "original stopped probe status")
	jsonOutput := f.Bool("json", false, "print the fresh shutdown observation")
	f.Usage = clihelp.Usage(stderr, clihelp.Help{Summary: "Verify a stopped probe's processes and ports are gone, without changing anything.",
		Usage: []string{"temper execution verify-stopped --status-file FILE [--json]"}}, f)
	if err := f.Parse(args); err != nil {
		return parseFailure(err)
	}
	if *status == "" || f.NArg() != 0 {
		return failed(stderr, errors.New("--status-file is required"))
	}
	result, err := probecmd.VerifyStopped(*status)
	if err != nil {
		return failed(stderr, err)
	}
	if *jsonOutput {
		return encode(stdout, stderr, result)
	}
	fmt.Fprintf(stdout, "Verified stopped installation %s. Original status unchanged.\n", result.Installation)
	return 0
}
