//go:build darwin || linux

package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/setup"
)

func TestExistingSpecialSetupLockRefusesWithoutPublishing(t *testing.T) {
	for _, kind := range []string{"fifo", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(root, ".setup.lock")
			switch kind {
			case "fifo":
				if err := syscall.Mkfifo(lockPath, 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(root, "user-file")
				if err := os.WriteFile(target, []byte("preserve\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, lockPath); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(lockPath, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			c := composition(t)
			result := make(chan error, 1)
			go func() {
				_, changed, err := setup.SaveConfiguration(context.Background(), root, c, "", false)
				if changed && err == nil {
					result <- nil
					return
				}
				result <- err
			}()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("special setup lock was accepted")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("save blocked on a special setup lock")
			}
			if _, err := os.Lstat(filepath.Join(root, setup.ConfigurationFile)); !os.IsNotExist(err) {
				t.Fatalf("special lock left configuration: %v", err)
			}
			if kind == "symlink" {
				got, err := os.ReadFile(filepath.Join(root, "user-file"))
				if err != nil || strings.TrimSpace(string(got)) != "preserve" {
					t.Fatalf("symlink target changed: %q, %v", got, err)
				}
			}
		})
	}
}
