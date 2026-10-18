package probecmd

import (
	"fmt"
	"strconv"
	"strings"
)

// EnginePortsFree refuses a router start while another process listens on
// one of its engine ports, and names that process. Each root has its own
// block, so this finds another program, or two roots whose blocks collide.
func EnginePortsFree(start, count int) error {
	for port := start; port < start+count; port++ {
		if listenerClosed("127.0.0.1:" + strconv.Itoa(port)) {
			continue
		}
		return fmt.Errorf("engine port %d of this Temper folder is in use by %s; stop it and try again", port, portHolder(port))
	}
	return nil
}

// portHolder names the listening process, or says it could not.
func portHolder(port int) string {
	raw, err := systemRead("/usr/sbin/lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc")
	if err != nil {
		return "another process"
	}
	pid, command := "", ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "p") && pid == "" {
			pid = line[1:]
		}
		if strings.HasPrefix(line, "c") && command == "" {
			command = line[1:]
		}
	}
	switch {
	case pid == "":
		return "another process"
	case command == "":
		return "process " + pid
	}
	return fmt.Sprintf("%s (process %s)", command, pid)
}
