package probecmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/temper-sh/temper/internal/datadir"
)

// StoppedObservation is a fresh absence check, bound to the original status
// bytes. It neither replaces that historical status nor grants signaling rights.
type StoppedObservation struct {
	Schema        string   `json:"schema"`
	StatusSHA256  string   `json:"status_sha256"`
	Root          string   `json:"root"`
	Installation  string   `json:"installation"`
	Generation    string   `json:"generation"`
	ProcessIDs    []int    `json:"process_ids"`
	ProcessGroups []int    `json:"process_group_ids"`
	Listeners     []string `json:"listeners"`
	VerifiedAt    string   `json:"verified_at"`
	SafeToCleanup bool     `json:"safe_to_cleanup"`
}

// VerifyStopped reads a stopped probe and the current OS state. A live/reused
// PID, existing group, occupied port or uncertain observation refuses recovery.
// It never signals a process, changes a status file, or touches an installation.
func VerifyStopped(statusPath string) (StoppedObservation, error) {
	if runtime.GOOS != "darwin" {
		return StoppedObservation{}, errors.New("probe shutdown verification currently requires macOS")
	}
	return verifyStopped(statusPath, func(pid int) error { return syscall.Kill(pid, 0) }, listenerClosed)
}

func verifyStopped(statusPath string, exists func(int) error, closed func(string) bool) (StoppedObservation, error) {
	var result StoppedObservation
	info, err := os.Lstat(statusPath)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return result, errors.New("probe status must be a regular file no larger than 16 MiB")
	}
	raw, err := os.ReadFile(statusPath)
	if err != nil {
		return result, err
	}
	var status probeStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return result, err
	}
	if status.Schema != "temper-probe-status/v1" || status.State != "stopped" || status.ListenersVerified ||
		status.TemperPID <= 1 || status.ProcessGroup <= 1 || !filepath.IsAbs(status.Root) ||
		filepath.Clean(status.Root) != status.Root || status.Installation == "" || !generationPattern.MatchString(status.Generation) {
		return result, errors.New("a complete stopped probe status is required; stop its supervisor before verification")
	}
	if err := validateListen(status.Listen); err != nil {
		return result, err
	}
	if _, err := time.Parse(time.RFC3339Nano, status.Updated); err != nil {
		return result, errors.New("probe status has no valid observation time")
	}
	rootInfo, err := os.Lstat(status.Root)
	if err != nil || !rootInfo.IsDir() {
		return result, errors.New("the original probe root must still exist on this machine")
	}
	if err := regularFile(filepath.Join(status.Root, datadir.MarkerFile), false); err != nil {
		return result, err
	}
	if err := datadir.Check(status.Root); err != nil {
		return result, err
	}
	pids, groups := map[int]bool{status.TemperPID: true, status.ProcessGroup: true}, map[int]bool{status.ProcessGroup: true}
	hasRouter := false
	for _, role := range status.Roles {
		if role.ID == "router" && role.PID == status.ProcessGroup && role.PGID == status.ProcessGroup {
			hasRouter = true
		}
	}
	if !hasRouter {
		return result, errors.New("probe status has no bound router identity")
	}
	// Issued 0.3.x statuses contain roles only. New stopped statuses also retain
	// helpers; both are checked, so an incomplete extra list cannot hide a role.
	for _, process := range append(append([]ProcessIdentity{}, status.Roles...), status.Processes...) {
		if process.PID <= 1 || process.PGID <= 1 || process.Started == "" {
			return result, errors.New("probe status contains an incomplete process identity")
		}
		pids[process.PID], groups[process.PGID] = true, true
	}
	ids := func(set map[int]bool) []int {
		values := make([]int, 0, len(set))
		for id := range set {
			values = append(values, id)
		}
		sort.Ints(values)
		return values
	}
	result = StoppedObservation{Schema: "temper-probe-recovery/v1", Root: status.Root, Installation: status.Installation,
		Generation: status.Generation, ProcessIDs: ids(pids), ProcessGroups: ids(groups),
		Listeners: []string{status.Listen, "127.0.0.1:" + strconv.Itoa(datadir.StartPort(status.Root))}}
	checkProcesses := func() error {
		for _, pid := range result.ProcessIDs {
			if err := exists(pid); !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("probe PID %d exists or cannot be inspected: %v", pid, err)
			}
		}
		for _, group := range result.ProcessGroups {
			if err := exists(-group); !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("probe process group %d exists or cannot be inspected: %v", group, err)
			}
		}
		return nil
	}
	if err := checkProcesses(); err != nil {
		return StoppedObservation{}, err
	}
	for _, listen := range result.Listeners {
		if !closed(listen) {
			return StoppedObservation{}, fmt.Errorf("probe listener %s is occupied or cannot be inspected", listen)
		}
	}
	if err := checkProcesses(); err != nil {
		return StoppedObservation{}, err
	}
	after, err := os.ReadFile(statusPath)
	if err != nil || !bytes.Equal(raw, after) {
		return StoppedObservation{}, errors.New("probe status changed during verification")
	}
	digest := sha256.Sum256(raw)
	result.StatusSHA256 = hex.EncodeToString(digest[:])
	result.VerifiedAt, result.SafeToCleanup = time.Now().UTC().Format(time.RFC3339Nano), true
	return result, nil
}
