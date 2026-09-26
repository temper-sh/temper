package probecmd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
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

func processArguments(pid int) (string, error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	return parseProcessArguments(raw)
}

// KERN_PROCARGS2 carries argc, the executable path, padding, then exactly argc
// NUL-terminated arguments. Environment strings are never retained.
func parseProcessArguments(raw []byte) (string, error) {
	if len(raw) < 5 {
		return "", errors.New("incomplete kernel process arguments")
	}
	argc := int(binary.NativeEndian.Uint32(raw[:4]))
	if argc < 1 || argc > 256 {
		return "", errors.New("invalid kernel argument count")
	}
	raw = raw[4:]
	end := bytes.IndexByte(raw, 0)
	if end < 0 {
		return "", errors.New("missing kernel executable terminator")
	}
	raw = bytes.TrimLeft(raw[end:], "\x00")
	args := make([]string, 0, argc)
	for range argc {
		end := bytes.IndexByte(raw, 0)
		if end < 0 {
			return "", errors.New("incomplete kernel argument vector")
		}
		args = append(args, string(raw[:end]))
		raw = raw[end+1:]
	}
	return strings.Join(args, "\x00"), nil
}
