package userpreset_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/userpreset"
)

// Cancellation is triggered by an observable prepared file, between attachment
// publication and the preset commit. No timing or goroutine race is required.
type cancelAfterTemplate struct {
	context.Context
	path string
}

func (c cancelAfterTemplate) Err() error {
	if _, err := os.Stat(c.path); err == nil {
		return context.Canceled
	}
	return nil
}

func TestInterruptedTemplateSaveRemovesOnlyItsOwnPreparedCopy(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(userpreset.Dir(root), 0700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(userpreset.Dir(root), "z-existing.jinja")
	if err := os.WriteFile(keep, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	u, err := catalog.ParseUserPreset("mine", templatePreset(t, "a-new.jinja"))
	if err != nil {
		t.Fatal(err)
	}
	u.Templates["keep"] = catalog.Template{CompatibleArtifacts: []string{"weights"}, Sources: []catalog.Source{{Hub: catalog.HubLocal}}, Files: []catalog.File{{Path: "z-existing.jinja"}}, License: "unknown"}
	raw, err := catalog.MarshalUserPreset(u)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"a-new.jinja": []byte("new"), "z-existing.jinja": []byte("existing")}
	newPath := filepath.Join(userpreset.Dir(root), "a-new.jinja")
	ctx := cancelAfterTemplate{Context: context.Background(), path: newPath}
	changed, err := userpreset.SaveWithTemplates(ctx, root, "mine", nil, raw, files, false)
	if changed || !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupt: %v %v", changed, err)
	}
	if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed save retained its own attachment")
	}
	if _, err := userpreset.Read(root, "mine"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed save published preset")
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != "existing" {
		t.Fatal("cleanup changed a preexisting template")
	}
	// A crash may leave a prepared unreferenced copy. Retrying safely reuses it.
	if err := os.WriteFile(newPath, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(newPath)
	if changed, err := userpreset.SaveWithTemplates(context.Background(), root, "mine", nil, raw, files, false); err != nil || !changed {
		t.Fatalf("retry: %v %v", changed, err)
	}
	after, _ := os.Stat(newPath)
	if !os.SameFile(info, after) {
		t.Fatal("retry replaced prepared identical copy")
	}
}

func templatePreset(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := catalog.MarshalUserPreset(catalog.UserPreset{
		Schema: catalog.UserPresetSchema, Origin: "original",
		Changes:   map[string]any{"template": "chat"},
		Templates: map[string]catalog.Template{"chat": {CompatibleArtifacts: []string{"weights"}, Sources: []catalog.Source{{Hub: catalog.HubLocal}}, Files: []catalog.File{{Path: name}}, License: "unknown"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTemplateImportSnapshotsBytesAndPublishesOnlyOnSave(t *testing.T) {
	source := filepath.Join(t.TempDir(), "My chat.jinja")
	content := []byte("{{ messages }}")
	if err := os.WriteFile(source, content, 0600); err != nil {
		t.Fatal(err)
	}
	selected, err := userpreset.ReadTemplateFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("changed later"), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "absent")
	raw := templatePreset(t, selected.ManagedName)
	files := map[string][]byte{selected.ManagedName: selected.Content}
	ctx := context.Background()
	if _, err := userpreset.SaveWithTemplates(ctx, root, "mine", nil, raw, files, true); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := userpreset.SaveWithTemplates(cancelled, root, "mine", nil, raw, files, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("selection/dry run/cancel created root: %v", err)
	}
	if changed, err := userpreset.SaveWithTemplates(ctx, root, "mine", nil, raw, files, false); err != nil || !changed {
		t.Fatalf("save: %v %v", changed, err)
	}
	destination := filepath.Join(userpreset.Dir(root), selected.ManagedName)
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(content) {
		t.Fatalf("saved template is not selected snapshot: %q %v", got, err)
	}
	before, _ := os.Stat(destination)
	if changed, err := userpreset.SaveWithTemplates(ctx, root, "mine", raw, raw, files, false); err != nil || changed {
		t.Fatalf("repeat: %v %v", changed, err)
	}
	after, _ := os.Stat(destination)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("repeat rewrote template")
	}
	presetBefore, err := os.Stat(userpreset.Path(root, "mine"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if changed, err := userpreset.SaveWithTemplates(ctx, root, "mine", raw, raw, files, true); err != nil || !changed {
		t.Fatalf("dry restore: %v %v", changed, err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dry restore wrote template")
	}
	if changed, err := userpreset.SaveWithTemplates(ctx, root, "mine", raw, raw, files, false); err != nil || !changed {
		t.Fatalf("restore: %v %v", changed, err)
	}
	if restored, err := os.ReadFile(destination); err != nil || string(restored) != string(content) {
		t.Fatalf("restore lost selected bytes: %q %v", restored, err)
	}
	presetAfter, err := os.Stat(userpreset.Path(root, "mine"))
	if err != nil || !os.SameFile(presetBefore, presetAfter) || !presetBefore.ModTime().Equal(presetAfter.ModTime()) {
		t.Fatal("restoring the template rewrote the unchanged preset")
	}
	if _, err := userpreset.SaveWithTemplates(ctx, root, "second", nil, raw, files, false); err != nil {
		t.Fatal(err)
	}
	if _, err := userpreset.Delete(ctx, root, "mine", raw, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal("delete removed a shared template")
	}
}

func TestTemplatePublicationRefusesCollisionsSymlinksAndUnreferencedFiles(t *testing.T) {
	for _, kind := range []string{"different", "symlink", "unreferenced", "concurrent"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(userpreset.Dir(root), 0700); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(userpreset.Dir(root), "chat.jinja")
			raw := templatePreset(t, "chat.jinja")
			files := map[string][]byte{"chat.jinja": []byte("selected")}
			switch kind {
			case "different":
				if err := os.WriteFile(destination, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("selected"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, destination); err != nil {
					t.Fatal(err)
				}
			case "unreferenced":
				files["extra.jinja"] = []byte("extra")
			case "concurrent":
				if err := userpreset.Create(root, "mine", []byte("concurrent edit")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := userpreset.SaveWithTemplates(context.Background(), root, "mine", nil, raw, files, false); err == nil {
				t.Fatal("unsafe import succeeded")
			}
			if kind != "concurrent" {
				if _, err := userpreset.Read(root, "mine"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("refused import published preset")
				}
			}
			if kind == "different" {
				got, _ := os.ReadFile(destination)
				if string(got) != "keep" {
					t.Fatal("collision replaced existing template")
				}
			}
			matches, _ := filepath.Glob(filepath.Join(userpreset.Dir(root), ".temper-preset-*"))
			if len(matches) != 0 {
				t.Fatalf("staged residue: %v", matches)
			}
		})
	}
}
