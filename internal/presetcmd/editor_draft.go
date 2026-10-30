package presetcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/userpreset"
)

// A draft owns authoring bytes, never a selected execution lock. original is
// the optimistic write guard; initial is the revocable editor baseline.
type presetDraft struct {
	id                string
	file              catalog.UserPreset
	original, initial []byte
	templates         map[string][]byte
}

func draftFrom(id string, u catalog.UserPreset, original []byte) *presetDraft {
	raw, _ := catalog.MarshalUserPreset(u)
	return &presetDraft{id: id, file: u, original: bytes.Clone(original), initial: raw}
}

func (d *presetDraft) dirty() bool {
	raw, err := catalog.MarshalUserPreset(d.file)
	return d.original == nil || err != nil || !bytes.Equal(raw, d.initial)
}

func (d *presetDraft) compile(root string, view catalog.View, memory int) (catalog.Lock, []string, error) {
	raw, err := catalog.MarshalUserPreset(d.file)
	if err != nil {
		return catalog.Lock{}, nil, err
	}
	loaded, err := userpreset.ResolveWithTemplates(root, d.id, raw, view, d.templates)
	if err != nil {
		return catalog.Lock{}, nil, err
	}
	lock, err := catalog.CompilePreset(loaded.Document, d.id, memory, 0, target)
	return lock, loaded.Warnings, err
}

type editField struct {
	catalog.Field
	current, origin any
	override        bool
}

func (d *presetDraft) base(view catalog.View, memory int) catalog.Preset {
	p := view.Document.Presets[d.file.Origin]
	if d.file.Preset != nil {
		p = *d.file.Preset
	}
	p.Profiles = []catalog.Profile{p.SelectProfile(memory)}
	return p
}

func (d *presetDraft) displayName(view catalog.View, memory int) string {
	p := d.base(view, memory)
	name := p.Presentation.DisplayName
	if d.file.Fork() {
		name = d.file.Name
		if name == "" {
			name = p.Presentation.DisplayName + " (yours)"
		}
	}
	return name
}

func (d *presetDraft) fields(view catalog.View, memory int) []editField {
	p := d.base(view, memory)
	values := catalog.Values(p)
	result := []editField{{Field: catalog.Field{Path: "name", Type: catalog.FieldText, Help: "Display name; the preset ID stays unchanged."}, current: d.displayName(view, memory)}}
	if !d.file.Fork() {
		result = append(result, editField{Field: catalog.Field{Path: "context_limit_tokens", Type: catalog.FieldInteger, Help: "Supported context ceiling in tokens; 0 leaves it unspecified."}, current: p.ContextLimitTokens})
	}
	for _, f := range catalog.Fields(p.Config().Kind) {
		entry := editField{Field: f, current: values[f.Path], origin: values[f.Path]}
		if d.file.Fork() {
			if v, ok := nestedValue(d.file.Changes, f.Path); ok {
				entry.current, entry.override = v, true
			}
		}
		result = append(result, entry)
	}
	return result
}

func parseField(f catalog.Field, text string) (any, error) {
	if text == "" && f.Optional() {
		return nil, nil
	}
	switch f.Type {
	case catalog.FieldText:
		if strings.ContainsAny(text, "\x00\x1b\r\n") {
			return nil, errors.New("use one line of text")
		}
		return text, nil
	case catalog.FieldChoice:
		if slices.Contains(f.Choices, text) {
			return text, nil
		}
		return nil, fmt.Errorf("choose %s", strings.Join(f.Choices, ", "))
	case catalog.FieldSwitch:
		if text == "true" {
			return true, nil
		}
		if text == "false" {
			return false, nil
		}
		return nil, errors.New("choose true or false")
	case catalog.FieldInteger:
		n, err := strconv.Atoi(text)
		if err != nil {
			return nil, errors.New("enter a whole number")
		}
		return n, nil
	case catalog.FieldNumber:
		n, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, errors.New("enter a finite number")
		}
		return n, nil
	}
	return nil, errors.New("unsupported field type")
}

func (d *presetDraft) set(f catalog.Field, text string, memory int) error {
	v, err := parseField(f, text)
	if err != nil {
		return err
	}
	if f.Path == "name" {
		if strings.TrimSpace(text) == "" {
			return errors.New("enter a display name")
		}
		if d.file.Fork() {
			d.file.Name = text
		} else {
			d.file.Preset.Presentation.DisplayName = text
		}
		return nil
	}
	return d.setValue(f.Path, v, memory)
}

func (d *presetDraft) setValue(path string, v any, memory int) error {
	if d.file.Fork() {
		if d.file.Changes == nil {
			d.file.Changes = map[string]any{}
		}
		writeNested(d.file.Changes, path, v, false)
		return nil
	}
	return d.setCopy(path, v, memory)
}

func (d *presetDraft) setCopy(path string, v any, memory int) error {
	raw, err := json.Marshal(d.file.Preset)
	if err != nil {
		return err
	}
	var record map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&record); err != nil {
		return err
	}
	into := record
	if path == "context_window_tokens" || strings.HasPrefix(path, "engine_config.") {
		profile := d.file.Preset.SelectProfile(memory)
		for i, p := range d.file.Preset.Profiles {
			if p.MinMemoryGiB == profile.MinMemoryGiB {
				into = record["profiles"].([]any)[i].(map[string]any)
				break
			}
		}
	}
	putNested(into, path, v)
	raw, err = json.Marshal(record)
	if err != nil {
		return err
	}
	var preset catalog.Preset
	if err := json.Unmarshal(raw, &preset); err != nil {
		return err
	}
	d.file.Preset = &preset
	return nil
}

func (d *presetDraft) reset(path string, memory int) error {
	if d.file.Fork() {
		if path == "name" {
			d.file.Name = ""
		} else {
			putNested(d.file.Changes, path, nil)
		}
		return nil
	}
	initial, err := catalog.ParseUserPreset(d.id, d.initial)
	if err != nil {
		return err
	}
	// A converted fork has no copy baseline: Undo conversion restores the
	// whole fork instead of inventing a per-field origin for the copy.
	if initial.Preset == nil {
		return errors.New("this copy was converted in this draft; use Undo conversion to restore the fork")
	}
	if path == "name" {
		d.file.Preset.Presentation.DisplayName = initial.Preset.Presentation.DisplayName
		return nil
	}
	p := *initial.Preset
	p.Profiles = []catalog.Profile{p.SelectProfile(memory)}
	return d.setCopy(path, catalog.Values(p)[path], memory)
}

func nestedValue(m map[string]any, path string) (any, bool) {
	parts := strings.SplitN(path, ".", 2)
	v, ok := m[parts[0]]
	if len(parts) == 1 {
		return v, ok
	}
	next, _ := v.(map[string]any)
	return nestedValue(next, parts[1])
}

// nil removes an authored leaf, and empty parents, rather than writing null.
func putNested(m map[string]any, path string, v any) {
	writeNested(m, path, v, v == nil)
}

func writeNested(m map[string]any, path string, v any, remove bool) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) == 1 {
		if remove {
			delete(m, parts[0])
		} else {
			m[parts[0]] = v
		}
		return
	}
	next, _ := m[parts[0]].(map[string]any)
	if next == nil {
		next = map[string]any{}
	}
	writeNested(next, parts[1], v, remove)
	if len(next) == 0 {
		delete(m, parts[0])
	} else {
		m[parts[0]] = next
	}
}

// saveDraft checks the catalog and compilation again at the effect boundary.
// Publication does not own configuration, layouts, or running services.
func (c Command) saveDraft(ctx context.Context, root string, d *presetDraft, reviewed catalog.View, memory int, dry bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	current, err := c.view(root)
	if err != nil {
		return false, err
	}
	a, _ := json.Marshal(reviewed)
	b, _ := json.Marshal(current)
	if !bytes.Equal(a, b) {
		return false, errors.New("the catalog changed; Ctrl-R reloads it and reviews your draft before saving")
	}
	if d.original == nil {
		if err := newID(root, d.id, current); err != nil {
			return false, err
		}
	}
	if _, _, err := d.compile(root, current, memory); err != nil {
		return false, err
	}
	raw, err := catalog.MarshalUserPreset(d.file)
	if err != nil {
		return false, err
	}
	if !d.dirty() {
		raw = d.original
	}
	if !dry {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := datadir.Ensure(root); err != nil {
			return false, err
		}
	}
	templates := map[string][]byte{}
	for _, name := range d.file.LocalTemplates() {
		if content, ok := d.templates[name]; ok {
			templates[name] = content
		}
	}
	changed, err := userpreset.SaveWithTemplates(ctx, root, d.id, d.original, raw, templates, dry)
	if !dry && (err == nil || changed) {
		d.original, d.initial = raw, mustDraftBytes(d.file)
	}
	return changed, err
}

func mustDraftBytes(u catalog.UserPreset) []byte {
	raw, _ := catalog.MarshalUserPreset(u) // Contains only parsed finite values.
	return raw
}
