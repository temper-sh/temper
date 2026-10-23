//go:build darwin || linux

package userpreset_test

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/userpreset"
)

func TestTemplateSelectionRefusesPipeWithoutWaitingForAWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.jinja")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := userpreset.ReadTemplateFile(path)
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("accepted a pipe as a template file")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("selection blocked opening a pipe")
	}
}
