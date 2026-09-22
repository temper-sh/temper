package probecmd

import (
	"bytes"
	"errors"
	"path/filepath"
	"syscall"
	"unsafe"
)

// processExecutable reads the kernel's executable path, independently of argv[0].
// PROC_INFO_CALL_PIDINFO / PROC_PIDPATHINFO is the query used by proc_pidpath;
// using the syscall keeps the release free of a C runtime build dependency.
func processExecutable(pid int) (string, error) {
	buffer := make([]byte, 4096) // PROC_PIDPATHINFO_MAXSIZE
	_, _, errno := syscall.Syscall6(syscall.SYS_PROC_INFO, 2, uintptr(pid), 11, 0,
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if errno != 0 {
		return "", errno
	}
	end := bytes.IndexByte(buffer, 0)
	if end <= 0 || !filepath.IsAbs(string(buffer[:end])) {
		return "", errors.New("kernel returned no absolute executable path")
	}
	return string(buffer[:end]), nil
}
