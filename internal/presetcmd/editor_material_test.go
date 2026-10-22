package presetcmd_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/huggingface"
	"github.com/temper-sh/temper/internal/presetcmd"
	"github.com/temper-sh/temper/internal/userpreset"
)

func primaryAction(m *presetcmd.Editor) tea.Cmd {
	key(m, tea.KeyTab)
	key(m, tea.KeyEnd)
	return key(m, tea.KeyEnter)
}

func completeRead(t *testing.T, m *presetcmd.Editor, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatalf("read did not start:\n%s", ansi.Strip(m.View().Content))
	}
	m.Update(cmd())
}

func TestMaterialFormsShowOnlyTheSelectedSourcesDecisions(t *testing.T) {
	c, root := command(t)
	m := editor(t, singleSource(c), root, "", false)
	key(m, 'n')
	key(m, 'f')
	input(m, "Writing")
	check := func(want, absent []string) {
		t.Helper()
		for _, width := range []int{40, 80, 112} {
			m.Update(tea.WindowSizeMsg{Width: width, Height: 32})
			frame := ansi.Strip(m.View().Content)
			for _, s := range want {
				if !strings.Contains(frame, s) {
					t.Fatalf("missing %q at %d columns:\n%s", s, width, frame)
				}
			}
			for _, s := range append(absent, "License override", "Weights ID", "Template ID", "? keys", "More options", "Advanced", "Transform", "Format:") {
				if strings.Contains(frame, s) {
					t.Fatalf("unnecessary %q at %d columns:\n%s", s, width, frame)
				}
			}
			assertEditorFrame(t, m, width, 32)
		}
	}
	check([]string{"Weights", "Template"}, []string{"Inherited"})
	key(m, tea.KeyDown)
	key(m, tea.KeyEnter) // Weights opens its source chooser, never an ID prompt.
	check([]string{"Repository:", "Revision:"}, []string{"Model relationship:"})
	key(m, tea.KeyEsc)
	key(m, 't')
	check([]string{"From model"}, []string{"Repository:", "Revision:", "File:", "Transform"})
	key(m, tea.KeyEnter) // Local file.
	check([]string{"File:"}, []string{"Repository:", "Revision:", "Transform"})
	key(m, tea.KeyEnter) // Hugging Face.
	check([]string{"Repository:", "Revision:", "File:"}, nil)
}

type hubWithoutLicense struct{ presetcmd.Hub }

func (h hubWithoutLicense) Revision(ctx context.Context, repo, revision string) (huggingface.Repository, error) {
	r, err := h.Hub.Revision(ctx, repo, revision)
	r.License = ""
	return r, err
}

func TestEditorImportsMissingLicenseMetadataAndIgnoresInactiveTemplateSources(t *testing.T) {
	c, root := withHub(t)
	c.Hub = hubWithoutLicense{c.Hub}
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	m := editor(t, c, root, "mine", false)
	key(m, 'w')
	completeRead(t, m, primaryAction(m))
	key(m, ' ')
	completeRead(t, m, key(m, tea.KeyEnter))
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("weights import saved before Save")
	}
	key(m, 's')
	u := read(t, root, "mine")
	a := u.Artifacts[u.Changes["artifact"].(string)]
	if a.License != "Not declared by source" || a.Sources[0].Revision != q6Commit {
		t.Fatalf("missing metadata was fabricated or lost: %+v", a)
	}
	key(m, tea.KeyEnter) // Reopen the saved preset.
	key(m, 't')
	key(m, tea.KeyEnter) // Local.
	key(m, tea.KeyDown)
	key(m, tea.KeyEnter)
	input(m, "does-not-exist.jinja")
	key(m, tea.KeyUp)
	key(m, tea.KeyEnter) // Switch to Hugging Face; the local path must not be used.
	key(m, tea.KeyDown)
	key(m, tea.KeyEnter)
	input(m, "froggeric/Qwen-Fixed-Chat-Templates")
	key(m, tea.KeyDown) // Revision.
	key(m, tea.KeyDown) // File.
	key(m, tea.KeyEnter)
	input(m, "chat_template.jinja")
	completeRead(t, m, primaryAction(m))
	key(m, 's')
	u = read(t, root, "mine")
	template := u.Templates[u.Changes["template"].(string)]
	if template.License != "Not declared by source" || template.Sources[0].Hub != catalog.HubHuggingFace || template.Sources[0].Revision != templateCommit {
		t.Fatalf("wrong source or invented license: %+v", template)
	}
	key(m, tea.KeyEnter)
	key(m, 'd')
	m.Update(tea.WindowSizeMsg{Width: 112, Height: 64})
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"Source details", "License: Not declared by source", q6Commit, templateCommit} {
		if !strings.Contains(frame, want) {
			t.Fatalf("read-only details omit %q:\n%s", want, frame)
		}
	}
}
