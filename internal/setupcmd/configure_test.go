package setupcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func configureRun(c Command, args ...string) (int, string, string) {
	var out, err bytes.Buffer
	code := c.Configure(context.Background(), args, strings.NewReader(""), &out, &err)
	return code, out.String(), err.String()
}

func TestConfigureSavesPresetsOnceAndEditsLayoutsOffline(t *testing.T) {
	c := fixtureCommand(t)
	root := filepath.Join(t.TempDir(), "root")
	args := []string{"--root", root, "--preset", compactLayout, "--context", compactLayout + "=16384", "--json"}
	if code, _, err := configureRun(c, append(args, "--dry-run")...); code != 0 {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("dry run created root")
	}
	if code, _, err := configureRun(c, args...); code != 0 {
		t.Fatal(err)
	}
	saved, rev, err := setup.ReadConfiguration(root)
	if err != nil || len(saved.Presets) != 1 || len(saved.Layouts) != 0 {
		t.Fatal("script selected unrequested layouts", err)
	}
	saved.Layouts["review"] = setup.Layout{Name: "Review", Presets: []string{compactLayout}, Startup: []string{}, IdleSeconds: 30}
	raw, err := saved.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "edit.json")
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) {
		t.Fatal("offline layout edit read moving catalog")
		return catalog.Document{}, "", errors.New("offline")
	}
	if code, _, err := configureRun(c, "--root", root, "--file", file, "--revision", rev, "--json"); code != 0 {
		t.Fatal(err)
	}
	if code, _, err := configureRun(c, "--root", root, "--resume", "--json"); code != 0 {
		t.Fatal(err)
	}
	if code, _, _ := configureRun(c, "--root", root, "--file", file, "--revision", rev); code == 0 {
		t.Fatal("stale editor overwrote layout")
	}
	saved, revision, err := setup.ReadConfiguration(root)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, err := configureRun(c, "--root", root, "--remove-preset", compactLayout, "--revision", revision); code == 0 || !strings.Contains(err, "review") {
		t.Fatal("referenced removal did not identify layout", err)
	}
	delete(saved.Layouts, "review")
	raw, _ = json.Marshal(saved)
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, err := configureRun(c, "--root", root, "--file", file, "--revision", revision); code != 0 {
		t.Fatal(err)
	}
	got, _, err := setup.ReadConfiguration(root)
	if err != nil || len(got.Presets) != 1 {
		t.Fatal("layout deletion discarded preset", err)
	}
}
