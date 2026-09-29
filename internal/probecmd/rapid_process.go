package probecmd

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
)

// Rapid MLX 0.15.2's _signal_observability._CRASH_TEE_SCRIPT. Retain its
// identity rather than vendoring third-party code. A changed helper needs review.
const rapidCrashLogSHA256 = "8b69ee2f2cebdf3943155e195d5f31a06dbc5b14818e7b40aee9ba90206e3435"

func pythonCrashLogCommand(arguments, digest string) bool {
	args := strings.Split(arguments, "\x00")
	if digest == "" || len(args) != 4 || args[1] != "-c" {
		return false
	}
	fd, err := strconv.Atoi(args[3])
	return err == nil && fd >= 3 && strconv.Itoa(fd) == args[3] &&
		fmt.Sprintf("%x", sha256.Sum256([]byte(args[2]))) == digest
}

// Only the selected engine may introduce this helper. Once observed, the
// ordinary PID/start/argv/group checks retain it through engine shutdown.
func pythonCrashLogParent(row processRow, rows []processRow, invocation Invocation) bool {
	for _, parent := range rows {
		if parent.pid == row.ppid && !parent.exited() && parent.executable == invocation.EnginePath &&
			exactArguments(parent.arguments, invocation.EngineArguments) {
			return true
		}
	}
	return false
}
