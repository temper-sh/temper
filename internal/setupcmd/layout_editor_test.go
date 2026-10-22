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
	"github.com/temper-sh/temper/internal/harness"
	"github.com/temper-sh/temper/internal/setup"
)

func layoutFixture(t *testing.T) setup.Configuration {
	t.Helper()
	doc := guidedCatalog(t)
	c := setup.EmptyConfiguration()
	for id, origin := range map[string]string{"chat": compactLayout, "extract": extraction} {
		lock, err := catalog.CompilePreset(doc, origin, 0, 0, doc.Runtime.Router.Target)
		if err != nil {
			t.Fatal(err)
		}
		c.Presets[id] = setup.Preset{Name: id, Lock: lock}
	}
	c.Layouts = setup.StarterLayouts([]string{"chat"})
	c.Integrations = []harness.Selection{{ID: "pi", ModelsFile: filepath.Join(t.TempDir(), "models.json")}}
	return c
}

func layoutPress(m tea.Model, code rune) tea.Cmd {
	key := tea.KeyPressMsg{Code: code}
	if code >= 32 && code < 127 {
		key.Text = string(code)
	}
	_, cmd := m.Update(key)
	return cmd
}

func layoutControl(m tea.Model, code rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl})
	return cmd
}

func TestLayoutEditorComposesSelectedPresetsAndPreservesExactInputs(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	before := layoutFixture(t)
	if _, _, err := setup.SaveConfiguration(ctx, root, before, "", false); err != nil {
		t.Fatal(err)
	}
	runningJob(t, root)
	statePath := filepath.Join(root, "managed", "activation.json")
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "presets.d"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "presets.d", "unselected.json"), []byte("not a preset"), 0600); err != nil {
		t.Fatal(err)
	}
	c := fixtureCommand(t)
	c.Catalog = func(context.Context, string, string, bool) (catalog.View, error) {
		t.Fatal("layout composition must not read the catalog")
		return catalog.View{}, errors.New("offline")
	}
	m, err := c.NewLayoutEditor(ctx, LayoutEditorOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	layoutPress(m, 'n')
	m.Update(tea.PasteMsg{Content: "Writing"})
	layoutPress(m, tea.KeyEnter)
	layoutPress(m, ' ')
	layoutPress(m, 'p')
	layoutPress(m, 'd')
	layoutPress(m, tea.KeyRight)
	layoutPress(m, tea.KeyEnter)
	layoutControl(m, 'u')
	m.Update(tea.PasteMsg{Content: "Writing desk"})
	layoutPress(m, tea.KeyEnter)
	layoutPress(m, tea.KeyDown)
	layoutPress(m, tea.KeyEnter)
	layoutControl(m, 'u')
	m.Update(tea.PasteMsg{Content: "300"})
	layoutPress(m, tea.KeyEnter)
	review := layoutControl(m, 's')
	if review == nil {
		t.Fatal("Review did not request memory assessment")
	}
	m.Update(review())
	if got, _, err := setup.ReadConfiguration(root); err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal("draft/review changed saved configuration", err)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "chat · preload · default") || !strings.Contains(view, "300 seconds") || strings.Contains(view, "unselected") {
		t.Fatalf("review lost chosen behavior or included an authoring file:\n%s", view)
	}
	save := layoutControl(m, 's')
	if save == nil {
		t.Fatal("Save was not reachable")
	}
	m.Update(save())
	got, _, err := setup.ReadConfiguration(root)
	want := setup.Layout{Name: "Writing desk", Presets: []string{"chat"}, Startup: []string{"chat"}, Default: "chat", IdleSeconds: 300}
	if err != nil || !reflect.DeepEqual(got.Layouts["writing"], want) {
		t.Fatalf("wrong saved layout: %+v, %v", got.Layouts["writing"], err)
	}
	if !reflect.DeepEqual(got.Presets, before.Presets) || !reflect.DeepEqual(got.Integrations, before.Integrations) || got.Software != before.Software || !reflect.DeepEqual(got.Layouts["default"], before.Layouts["default"]) {
		t.Fatal("layout save changed exact presets, other layouts or root settings")
	}
	stateAfter, _ := os.ReadFile(statePath)
	if !bytes.Equal(stateBefore, stateAfter) {
		t.Fatal("saving a layout changed the running generation")
	}
	path := filepath.Join(root, setup.ConfigurationFile)
	first, _ := os.Stat(path)
	m.Update(layoutControl(m, 's')())
	m.Update(layoutControl(m, 's')())
	second, _ := os.Stat(path)
	if !first.ModTime().Equal(second.ModTime()) {
		t.Fatal("unchanged Save rewrote configuration")
	}
}

func TestLayoutSaveConflictRetainsDraftAndOtherWritersChanges(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	before := layoutFixture(t)
	rev, _, err := setup.SaveConfiguration(ctx, root, before, "", false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := fixtureCommand(t).NewLayoutEditor(ctx, LayoutEditorOptions{Root: root, ID: "default"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	layoutPress(m, 'p')
	m.Update(layoutControl(m, 's')())
	before.Layouts["other"] = setup.Layout{Name: "Another writer", IdleSeconds: 60}
	if _, _, err := setup.SaveConfiguration(ctx, root, before, rev, false); err != nil {
		t.Fatal(err)
	}
	m.Update(layoutControl(m, 's')())
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "configuration changed") || !strings.Contains(view, "chat · preload") {
		t.Fatalf("conflict hid the reason or lost the draft:\n%s", view)
	}
	got, _, err := setup.ReadConfiguration(root)
	if err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal("conflict overwrote another writer", err)
	}
}

func TestLayoutDeletionPreservesPresetsAndRunningGeneration(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	before := layoutFixture(t)
	if _, _, err := setup.SaveConfiguration(ctx, root, before, "", false); err != nil {
		t.Fatal(err)
	}
	runningJob(t, root)
	path := filepath.Join(root, "managed", "activation.json")
	active, _ := os.ReadFile(path)
	m, err := fixtureCommand(t).NewLayoutEditor(ctx, LayoutEditorOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	layoutPress(m, tea.KeyDelete)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Delete layout “Default”?") {
		t.Fatalf("deletion did not name its target:\n%s", view)
	}
	layoutPress(m, tea.KeyEnter) // Keep layout is the initial choice.
	if got, _, _ := setup.ReadConfiguration(root); len(got.Layouts) != 1 {
		t.Fatal("deletion's default action removed a layout")
	}
	layoutPress(m, tea.KeyDelete)
	layoutPress(m, tea.KeyRight)
	cmd := layoutPress(m, tea.KeyEnter)
	if cmd == nil {
		t.Fatal("confirmed deletion did not save")
	}
	m.Update(cmd())
	got, _, err := setup.ReadConfiguration(root)
	if err != nil || len(got.Layouts) != 0 || !reflect.DeepEqual(got.Presets, before.Presets) || !reflect.DeepEqual(got.Integrations, before.Integrations) {
		t.Fatal("deletion changed shared configuration", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(active, after) {
		t.Fatal("deletion changed the running generation")
	}
}

func TestLayoutDryRunAndCancelledSaveLeaveAbsentRootAbsent(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "dry run"}[dry], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			root := filepath.Join(t.TempDir(), "absent")
			m, err := fixtureCommand(t).NewLayoutEditor(ctx, LayoutEditorOptions{Root: root, DryRun: dry})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			layoutPress(m, 'n')
			m.Update(tea.PasteMsg{Content: "Empty"})
			layoutPress(m, tea.KeyEnter)
			m.Update(layoutControl(m, 's')())
			save := layoutControl(m, 's')
			if !dry {
				cancel()
			}
			m.Update(save())
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("read, preview or refused save created root", err)
			}
			if view := ansi.Strip(m.View().Content); dry && !strings.Contains(view, "nothing saved") || !dry && !strings.Contains(view, "context canceled") {
				t.Fatalf("save result was not visible:\n%s", view)
			}
			layoutPress(m, tea.KeyEsc)
			layoutPress(m, tea.KeyEsc)
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "unsaved layout changes") {
				t.Fatalf("unpublished draft was marked saved:\n%s", view)
			}
		})
	}
}

func TestLayoutEditorTerminalAndArgumentChecksDoNotCreateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	c := fixtureCommand(t)
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"--root", root}, 2},
		{[]string{"edit", "default", "--root", root}, 2},
		{[]string{"edit"}, 2},
		{[]string{"edit", "--help"}, 0},
		{[]string{"edit", "default", "extra"}, 2},
		{[]string{"--json"}, 2},
	} {
		var out, diagnostics bytes.Buffer
		if code := c.Layout(context.Background(), tc.args, &out, &diagnostics); code != tc.code {
			t.Fatalf("arguments %v returned %d, want %d: %s", tc.args, code, tc.code, diagnostics.String())
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("argument/terminal check created root")
		}
	}
}
