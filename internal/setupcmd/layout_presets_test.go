package setupcmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/presetcmd"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/setupui"
	"github.com/temper-sh/temper/internal/userpreset"
)

func layoutFolderCommand(t *testing.T) Command {
	t.Helper()
	c := fixtureCommand(t)
	doc := guidedCatalog(t)
	c.StoredCatalog = func(string) (catalog.View, error) { return catalog.View{Document: doc}, nil }
	c.Catalog = func(context.Context, string, string, bool) (catalog.View, error) {
		t.Fatal("folder discovery reached a catalog download path")
		return catalog.View{}, errors.New("unexpected catalog download")
	}
	return c
}

func writeLayoutPreset(t *testing.T, root, id string, file catalog.UserPreset) []byte {
	t.Helper()
	raw, err := catalog.MarshalUserPreset(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := userpreset.Create(root, id, raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func newFileLayout(t *testing.T, ctx context.Context, c Command, root string, dry bool) *setupui.LayoutModel {
	t.Helper()
	m, err := c.NewLayoutEditor(ctx, LayoutEditorOptions{Root: root, DryRun: dry})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	layoutPress(m, 'n')
	m.Update(tea.PasteMsg{Content: "Work"})
	layoutPress(m, tea.KeyEnter)
	return m
}

func layoutComplete(t *testing.T, m tea.Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected layout operation")
	}
	m.Update(cmd())
}

func TestPresetEditorSaveAppearsInLayoutWithoutConfigurationSelection(t *testing.T) {
	for _, form := range []rune{'f', 'c'} {
		t.Run(map[rune]string{'f': "fork", 'c': "copy"}[form], func(t *testing.T) {
			ctx := context.Background()
			root := filepath.Join(t.TempDir(), "root")
			c := layoutFolderCommand(t)
			doc := guidedCatalog(t)
			doc.Presets = map[string]catalog.Preset{compactLayout: doc.Presets[compactLayout]}
			author := presetcmd.Command{Facts: c.Detect, Catalog: func(string) (catalog.View, error) {
				return catalog.View{Document: doc}, nil
			}}
			editor, err := author.NewEditor(ctx, presetcmd.EditorOptions{Root: root})
			if err != nil {
				t.Fatal(err)
			}
			defer editor.Close()
			layoutPress(editor, 'n')
			layoutPress(editor, form)
			layoutControl(editor, 'u')
			editor.Update(tea.PasteMsg{Content: "Fresh preset"})
			layoutPress(editor, tea.KeyEnter)
			layoutPress(editor, 's')
			file, err := userpreset.Read(root, "fresh-preset")
			if err != nil {
				t.Fatal("preset editor did not save", err, ansi.Strip(editor.View().Content))
			}
			writeLayoutPreset(t, root, "unused", catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: compactLayout, Name: "Unused preset"})
			for name, text := range map[string]string{"z-broken.json": "invalid json", "disabled.json.off": "ignored", ".hidden.json": "ignored", "template.jinja": "ignored"} {
				if err := os.WriteFile(filepath.Join(userpreset.Dir(root), name), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			m := newFileLayout(t, ctx, c, root, false)
			view := ansi.Strip(m.View().Content)
			for _, want := range []string{"Fresh preset", "Unused preset", "z-broken · unavailable", "untested"} {
				if !strings.Contains(view, want) {
					t.Fatalf("folder entry %q missing:\n%s", want, view)
				}
			}
			if strings.Contains(view, "disabled") || strings.Contains(view, ".hidden") || strings.Contains(view, "template.jinja") {
				t.Fatalf("non-preset files entered membership:\n%s", view)
			}
			layoutPress(m, ' ')
			layoutPress(m, 'p')
			layoutPress(m, 'd')
			layoutPress(m, tea.KeyDown)
			layoutPress(m, tea.KeyDown)
			layoutPress(m, ' ') // Invalid files cannot be included.
			layoutComplete(t, m, layoutControl(m, 's'))
			if _, rev, err := setup.ReadConfiguration(root); err != nil || rev != "" {
				t.Fatal("authoring, opening or reviewing selected presets implicitly", rev, err)
			}
			layoutComplete(t, m, layoutControl(m, 's'))
			saved, _, err := setup.ReadConfiguration(root)
			want := setup.Layout{Name: "Work", Presets: []string{"fresh-preset"}, Startup: []string{"fresh-preset"}, Default: "fresh-preset", IdleSeconds: 1800}
			if err != nil || !reflect.DeepEqual(saved.Layouts["work"], want) || len(saved.Presets) != 1 {
				t.Fatalf("first placement saved wrong composition: %+v, %v", saved, err)
			}
			preset := saved.Presets["fresh-preset"]
			if preset.UserPreset != "fresh-preset" || preset.Name != "Fresh preset" || preset.Lock.Preset != "fresh-preset" {
				t.Fatal("first placement lost file identity or compiled the origin instead", preset)
			}
			after, _ := userpreset.Read(root, "fresh-preset")
			if !bytes.Equal(after, file) {
				t.Fatal("layout save rewrote authoring")
			}
			first, _ := os.Stat(filepath.Join(root, setup.ConfigurationFile))
			layoutComplete(t, m, layoutControl(m, 's'))
			layoutComplete(t, m, layoutControl(m, 's'))
			second, _ := os.Stat(filepath.Join(root, setup.ConfigurationFile))
			if !first.ModTime().Equal(second.ModTime()) {
				t.Fatal("repeated file placement rewrote configuration")
			}
		})
	}
}

func TestFirstLayoutPlacementRefusesChangedFileAndTemplate(t *testing.T) {
	for _, change := range []string{"file", "deleted", "template"} {
		t.Run(change, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			c := layoutFolderCommand(t)
			file := catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: compactLayout, Name: "Mine"}
			if change == "template" {
				file.Changes = map[string]any{"template": "local"}
				file.Templates = map[string]catalog.Template{"local": {
					Sources: []catalog.Source{{Hub: catalog.HubLocal}}, Files: []catalog.File{{Path: "mine.jinja"}},
					CompatibleArtifacts: []string{guidedCatalog(t).Presets[compactLayout].Artifact}, License: "own work",
				}}
			}
			raw := writeLayoutPreset(t, root, "mine", file)
			template := filepath.Join(userpreset.Dir(root), "mine.jinja")
			if change == "template" {
				if err := os.WriteFile(template, []byte("{{ messages }}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			m := newFileLayout(t, context.Background(), c, root, false)
			layoutPress(m, ' ')
			layoutComplete(t, m, layoutControl(m, 's'))
			var err error
			switch change {
			case "file":
				err = os.WriteFile(userpreset.Path(root, "mine"), append(raw, ' '), 0600)
			case "deleted":
				err = os.Remove(userpreset.Path(root, "mine"))
			case "template":
				err = os.WriteFile(template, []byte("{{ messages }} changed"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			layoutComplete(t, m, layoutControl(m, 's'))
			if _, rev, err := setup.ReadConfiguration(root); err != nil || rev != "" {
				t.Fatal("source conflict wrote configuration", rev, err)
			}
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "reopen the layout editor") || !strings.Contains(view, "Mine · untested") {
				t.Fatalf("conflict lost draft or recovery direction:\n%s", view)
			}
		})
	}
}

func TestLayoutFolderDryRunCancellationAndMissingCatalogNeverSave(t *testing.T) {
	for _, mode := range []string{"dry", "cancelled", "missing catalog"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			root := filepath.Join(t.TempDir(), "root")
			writeLayoutPreset(t, root, "mine", catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: compactLayout, Name: "Mine"})
			c := layoutFolderCommand(t)
			if mode == "missing catalog" {
				c.StoredCatalog = readStoredCatalog
			}
			m := newFileLayout(t, ctx, c, root, mode == "dry")
			if mode == "missing catalog" {
				view := ansi.Strip(m.View().Content)
				if !strings.Contains(view, "mine · unavailable") || !strings.Contains(view, "no stored catalog") {
					t.Fatalf("missing catalog hid the file or reason:\n%s", view)
				}
			} else {
				layoutPress(m, ' ')
				layoutComplete(t, m, layoutControl(m, 's'))
				if mode == "cancelled" {
					cancel()
				}
				layoutComplete(t, m, layoutControl(m, 's'))
			}
			if _, rev, err := setup.ReadConfiguration(root); err != nil || rev != "" {
				t.Fatal("dry/refused placement wrote configuration", rev, err)
			}
		})
	}
}

func TestLayoutFilePlacementPreservesSavedLocksAndRunningGeneration(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	before := layoutFixture(t)
	if _, _, err := setup.SaveConfiguration(ctx, root, before, "", false); err != nil {
		t.Fatal(err)
	}
	runningJob(t, root)
	path := filepath.Join(root, "managed", "activation.json")
	active, _ := os.ReadFile(path)
	writeLayoutPreset(t, root, "mine", catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: compactLayout, Name: "Mine"})
	c := layoutFolderCommand(t)
	m := newFileLayout(t, ctx, c, root, false)
	layoutPress(m, tea.KeyDown)
	layoutPress(m, tea.KeyDown)
	layoutPress(m, ' ')
	layoutComplete(t, m, layoutControl(m, 's'))
	layoutComplete(t, m, layoutControl(m, 's'))
	saved, _, err := setup.ReadConfiguration(root)
	if err != nil || !reflect.DeepEqual(saved.Layouts["work"].Presets, []string{"mine"}) {
		t.Fatal("local preset was not placed", saved, err)
	}
	for id, p := range before.Presets {
		if !reflect.DeepEqual(p, saved.Presets[id]) {
			t.Fatal("placement rebuilt existing exact preset", id)
		}
	}
	if !reflect.DeepEqual(before.Layouts["default"], saved.Layouts["default"]) || !reflect.DeepEqual(before.Integrations, saved.Integrations) || before.Software != saved.Software {
		t.Fatal("placement altered unrelated composition or root settings")
	}
	// Removing authoring must not invalidate a layout's saved execution.
	if err := os.Remove(userpreset.Path(root, "mine")); err != nil {
		t.Fatal(err)
	}
	c.StoredCatalog = func(string) (catalog.View, error) {
		t.Fatal("existing exact presets should not need catalog access")
		return catalog.View{}, errors.New("offline")
	}
	m, err = c.NewLayoutEditor(ctx, LayoutEditorOptions{Root: root, ID: "work"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	layoutPress(m, tea.KeyDown)
	layoutPress(m, tea.KeyDown)
	layoutPress(m, 'p')
	layoutComplete(t, m, layoutControl(m, 's'))
	layoutComplete(t, m, layoutControl(m, 's'))
	after, _, err := setup.ReadConfiguration(root)
	if err != nil || !reflect.DeepEqual(saved.Presets, after.Presets) || !reflect.DeepEqual(after.Layouts["work"].Startup, []string{"mine"}) {
		t.Fatal("missing authoring changed the saved execution", err)
	}
	stillActive, _ := os.ReadFile(path)
	if !bytes.Equal(active, stillActive) {
		t.Fatal("preset placement affected the running generation")
	}
}
