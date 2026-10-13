package managed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/temper-sh/temper/internal/probecmd"
)

// launchSpec is a frozen engine invocation. The short-lived Temper launcher
// records its kernel lifetime and execs the selected engine in the same PID.
type launchSpec struct {
	Role, Path, LauncherSHA256 string
	Arguments                  []string
}

func ReadLauncher() (Launcher, error) {
	path, err := os.Executable()
	if err != nil {
		return Launcher{}, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return Launcher{}, err
	}
	digest, err := executableDigest(path)
	return Launcher{Path: path, SHA256: digest}, err
}

func executableDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func launchBytes(p Process, launcherSHA string) ([]byte, string) {
	raw, _ := json.Marshal(launchSpec{p.Role, p.Path, launcherSHA, p.Arguments})
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func readIdentity(path string) (probecmd.ManagedIdentity, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return probecmd.ManagedIdentity{}, false, nil
	}
	if err != nil {
		return probecmd.ManagedIdentity{}, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return probecmd.ManagedIdentity{}, false, errors.New("unsafe engine launch record")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return probecmd.ManagedIdentity{}, false, err
	}
	var identity probecmd.ManagedIdentity
	err = json.Unmarshal(raw, &identity)
	return identity, true, err
}

func (j Job) commands() []probecmd.ManagedCommand {
	commands := []probecmd.ManagedCommand{{ID: "router", Path: j.Router, Arguments: j.Arguments()}}
	for _, p := range j.Processes {
		commands = append(commands, probecmd.ManagedCommand{ID: p.Role, Path: p.Path, Arguments: p.Arguments})
	}
	return commands
}

func (j Job) loaded() ([]probecmd.ManagedIdentity, error) {
	var roots []probecmd.ManagedIdentity
	for _, p := range j.Processes {
		if p.Record == "" {
			continue
		}
		identity, exists, err := readIdentity(p.Record)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		if identity.ID != p.Role || identity.PID != identity.PGID || !probecmd.ManagedIdentityAllowed(identity, j.commands()) {
			return nil, errors.New("engine launch record differs from selected command")
		}
		roots = append(roots, identity)
	}
	return probecmd.ObserveManagedLoads(roots, j.commands())
}

// ExecLaunch is internal to generated commands, never a second supervisor. The
// durable record precedes exec, so interruption cannot create an unrecorded
// engine. Recovery still requires the actual kernel executable and full argv.
func ExecLaunch(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return errors.New("managed launch requires a frozen command and assigned port")
	}
	path := args[0]
	n, err := strconv.Atoi(args[1])
	if err != nil || n < 1024 || n > 65535 {
		return errors.New("invalid managed engine port")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return errors.New("unsafe managed launch command")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var spec launchSpec
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&spec); err != nil {
		return err
	}
	canonical, key := launchBytes(Process{Role: spec.Role, Path: spec.Path, Arguments: spec.Arguments}, spec.LauncherSHA256)
	if !bytes.Equal(raw, canonical) || filepath.Base(filepath.Dir(path)) != key || filepath.Base(path) != "launch.json" || !filepath.IsAbs(spec.Path) {
		return errors.New("managed launch command identity changed")
	}
	launcher, err := os.Executable()
	if err != nil {
		return err
	}
	digest, err := executableDigest(launcher)
	if err != nil {
		return err
	}
	if digest != spec.LauncherSHA256 {
		return errors.New("Temper executable changed; explicitly reactivate the layout")
	}
	record := filepath.Join(filepath.Dir(path), "process.json")
	old, exists, err := readIdentity(record)
	if err != nil {
		return err
	}
	if exists {
		allowed := []probecmd.ManagedCommand{{ID: spec.Role, Path: spec.Path, Arguments: spec.Arguments}}
		if !probecmd.ManagedIdentityAllowed(old, allowed) {
			return errors.New("previous engine launch identity changed")
		}
		remaining, err := probecmd.ReapManaged([]probecmd.ManagedIdentity{old}, false)
		if err != nil {
			return err
		}
		if remaining {
			return errors.New("previous engine lifetime has not exited")
		}
	}
	argv := append([]string(nil), spec.Arguments...)
	for i, a := range argv {
		if a == "${PORT}" {
			argv[i] = args[1]
		}
	}
	identity, err := probecmd.CaptureManagedLaunch(spec.Role, spec.Path, argv)
	if err != nil {
		return err
	}
	raw, err = json.Marshal(identity)
	if err != nil {
		return err
	}
	if err = writeAtomic(ctx, record, raw); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return syscall.Exec(spec.Path, append([]string{spec.Path}, argv...), os.Environ())
}

func launchCommand(launcher, record string) string {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	return fmt.Sprintf("%s internal-managed-exec %s ${PORT}", quote(launcher), quote(filepath.Join(filepath.Dir(record), "launch.json")))
}
