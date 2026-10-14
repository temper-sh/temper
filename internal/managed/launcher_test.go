package managed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// A layout launches engines through a copy inside its root, so replacing the
// installed Temper binary cannot break it.
func TestPublishCopiesTheLauncherIntoTheRootOnce(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "temper")
	binary := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(source, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(binary)
	digest := hex.EncodeToString(sum[:])
	rendered := Rendered{Job: Job{Launcher: launcherPath(root, digest), LauncherSHA256: digest}, LauncherSource: source}

	if err := publishLauncher(context.Background(), rendered); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(rendered.Job.Launcher)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("launcher copy: %v %v", info, err)
	}
	// The original may disappear (brew cleanup); the copy stays usable.
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := publishLauncher(context.Background(), rendered); err != nil {
		t.Fatalf("second publish needs no source: %v", err)
	}
	if err := os.WriteFile(rendered.Job.Launcher, []byte("edited"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := publishLauncher(context.Background(), rendered); err == nil {
		t.Fatal("an edited launcher copy was accepted")
	}
}

func TestPublishRefusesASourceThatChangedSinceRendering(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "temper")
	if err := os.WriteFile(source, []byte("new binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	digest := hex.EncodeToString(make([]byte, 32))
	rendered := Rendered{Job: Job{Launcher: launcherPath(root, digest), LauncherSHA256: digest}, LauncherSource: source}
	if err := publishLauncher(context.Background(), rendered); err == nil {
		t.Fatal("a changed source was copied")
	}
	if _, err := os.Stat(rendered.Job.Launcher); !os.IsNotExist(err) {
		t.Fatalf("refused copy left a file: %v", err)
	}
}

func TestPruneLaunchersKeepsOnlyTheCurrentCopy(t *testing.T) {
	root := t.TempDir()
	keep, old := hex.EncodeToString(make([]byte, 32)), hex.EncodeToString(sha256.New().Sum(nil))
	for _, digest := range []string{keep, old} {
		path := launcherPath(root, digest)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(digest), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneLaunchers(root, keep); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(launcherPath(root, keep)); err != nil {
		t.Fatalf("current copy removed: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(launcherPath(root, old))); !os.IsNotExist(err) {
		t.Fatalf("old copy kept: %v", err)
	}
	if err := PruneLaunchers(t.TempDir(), keep); err != nil {
		t.Fatalf("a root without launchers: %v", err)
	}
}
