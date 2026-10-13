package managed

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/probecmd"
)

func TestManagedLauncherProcess(t *testing.T) {
	path := os.Getenv("TEMPER_TEST_MANAGED_LAUNCH")
	if path == "" {
		return
	}
	if err := ExecLaunch(context.Background(), []string{path, "10001"}); err != nil {
		t.Fatal(err)
	}
}

// No launchd, inference or network. A disposable sleep proves exec preserves
// the recorded lifetime and later demand gets a separately verified lifetime.
func TestManagedLauncherRecordsBeforeExecAndAllowsReload(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("kernel executable/argv observation is macOS-specific")
	}
	launcher, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := executableDigest(launcher)
	if err != nil {
		t.Fatal(err)
	}
	p := Process{Role: "fixture", Path: "/bin/sleep", Arguments: []string{"30"}}
	raw, key := launchBytes(p, digest)
	path := filepath.Join(t.TempDir(), key, "launch.json")
	if err = writeAtomic(context.Background(), path, raw); err != nil {
		t.Fatal(err)
	}
	var prior int
	for range 2 {
		cmd := exec.Command(launcher, "-test.run=^TestManagedLauncherProcess$")
		cmd.Env = append(os.Environ(), "TEMPER_TEST_MANAGED_LAUNCH="+path)
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		var captured probecmd.ManagedIdentity
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			captured, _, err = readIdentity(filepath.Join(filepath.Dir(path), "process.json"))
			if err == nil && captured.PID == cmd.Process.Pid {
				found, e := probecmd.ObserveManagedLoads([]probecmd.ManagedIdentity{captured}, []probecmd.ManagedCommand{{ID: p.Role, Path: p.Path, Arguments: p.Arguments}})
				if e == nil && len(found) == 1 {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		if captured.PID != cmd.Process.Pid || captured.PID == prior {
			t.Fatal("new engine lifetime was not recorded", captured, err)
		}
		remaining, err := probecmd.ReapManaged([]probecmd.ManagedIdentity{captured}, true)
		if err != nil || !remaining {
			t.Fatal("record did not match exec'd engine", remaining, err)
		}
		_ = cmd.Wait()
		prior = captured.PID
	}
}
