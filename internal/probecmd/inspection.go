package probecmd

import "strings"

func inspectionExecutable(path string) bool {
	return path == "/usr/sbin/system_profiler" || path == "/usr/sbin/sysctl" || path == "/usr/sbin/ioreg"
}

// These are the read-only macOS commands used by llama-swap's hardware and
// performance observation. Executable identity comes from the kernel, not argv[0].
func inspectionCommand(row processRow) bool {
	argv := strings.Split(row.arguments, "\x00")
	if len(argv) < 2 {
		return false
	}
	args := strings.Join(argv[1:], "\x00")
	switch row.executable {
	case "/usr/sbin/system_profiler":
		return args == "-json\x00SPHardwareDataType\x00SPDisplaysDataType" ||
			args == "-nospawn\x00-xml\x00SPHardwareDataType\x00-detailLevel\x00full" ||
			args == "-nospawn\x00-xml\x00SPDisplaysDataType\x00-detailLevel\x00full"
	case "/usr/sbin/sysctl":
		return args == "-n\x00kern.hv_vmm_present"
	case "/usr/sbin/ioreg":
		return args == "-r\x00-c\x00IOGPU\x00-d\x001\x00-f"
	}
	return false
}

func routerInspection(row processRow, rows []processRow, group int, invocation Invocation, known map[int]processRow) bool {
	if !inspectionCommand(row) {
		return false
	}
	if prior, ok := known[row.pid]; ok && inspectionCommand(prior) {
		// The caller has already rechecked PID/start/group/executable/argv.
		// A bound helper may be reparented while the router shuts down.
		return true
	}
	parent := row.ppid
	for range len(rows) {
		var found *processRow
		for i := range rows {
			if rows[i].pid == parent {
				found = &rows[i]
				break
			}
		}
		if found == nil {
			return false
		}
		if parent == group {
			return found.executable == invocation.Path && found.pgid == group
		}
		if row.executable != "/usr/sbin/system_profiler" || found.executable != "/usr/sbin/system_profiler" || !inspectionCommand(*found) {
			return false
		}
		parent = found.ppid
	}
	return false
}

// vLLM inspects a model type it has no cached information for in one
// short-lived child of its frontend, then caches the result in the engine's
// cache folder, so only a first start runs it. It is not a measured role; its
// exact identity is retained for reaping and owned shutdown.
func vllmModelInspection(row processRow, rows []processRow, invocation Invocation, known map[int]processRow) bool {
	command := []string{"-m", "vllm.model_executor.models.registry"}
	if !exactArguments(row.arguments, command) {
		return false
	}
	if prior, ok := known[row.pid]; ok && exactArguments(prior.arguments, command) {
		// The caller has already rechecked PID/start/group/executable/argv.
		return true
	}
	for _, parent := range rows {
		if parent.pid == row.ppid && parent.executable == invocation.FrontendPath && exactArguments(parent.arguments, invocation.FrontendArguments) {
			return true
		}
	}
	return false
}
