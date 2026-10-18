package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	templatepatch "github.com/temper-sh/temper/internal/patch"
)

// UserPresetSchema is the format of one file in a root's presets.d.
const UserPresetSchema = "temper-preset/v1"

// goTypes are the Go type names in decoder errors, which mean nothing to a
// user editing a file.
var goTypes = regexp.MustCompile(` (in type|into) \*?[a-z]+\.[A-Za-z0-9_.]+`)

var unknownField = regexp.MustCompile(`field (\S+) not found`)

// UserPreset is the user's own preset. A fork names a catalog preset as its
// origin and changes settings from the field table; a copy holds a complete
// preset record. Records in the file come before the catalog's.
type UserPreset struct {
	Schema    string                  `yaml:"schema" json:"schema"`
	Origin    string                  `yaml:"origin,omitempty" json:"origin,omitempty"`
	Name      string                  `yaml:"name,omitempty" json:"name,omitempty"`
	Changes   map[string]any          `yaml:"changes,omitempty" json:"changes,omitempty"`
	Preset    *Preset                 `yaml:"preset,omitempty" json:"preset,omitempty"`
	Artifacts map[string]UserArtifact `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
	Templates map[string]Template     `yaml:"templates,omitempty" json:"templates,omitempty"`
}

// UserArtifact is a weights record in a user file: the catalog shape plus
// SameModelAs, the catalog weights it is another build of. Temper cannot
// verify that claim; it lets a fork change weights and a template accept them.
type UserArtifact struct {
	Artifact    `yaml:",inline"`
	SameModelAs string `yaml:"same_model_as,omitempty" json:"same_model_as,omitempty"`
}

// Fork reports whether the file is a fork rather than a copy.
func (u UserPreset) Fork() bool { return u.Origin != "" }

// ValidPresetID reports whether id can name a preset, and so a file.
func ValidPresetID(id string) bool { return idPattern.MatchString(id) }

// ParseUserPreset reads one presets.d file strictly: an unknown field, a
// second form or an invalid record is an error for this preset only.
func ParseUserPreset(id string, data []byte) (UserPreset, error) {
	var u UserPreset
	if !idPattern.MatchString(id) {
		return UserPreset{}, fmt.Errorf("%q is not a preset ID: use lowercase letters, digits, dots and dashes", id)
	}
	if err := decode(data, &u); err != nil {
		return UserPreset{}, errors.New(plainDecodeError(err))
	}
	if u.Schema != UserPresetSchema {
		return UserPreset{}, fmt.Errorf("schema must be %s", UserPresetSchema)
	}
	switch {
	case (u.Origin == "") == (u.Preset == nil):
		return UserPreset{}, errors.New("needs exactly one of origin (a fork) or preset (a copy)")
	case u.Preset != nil && (u.Name != "" || u.Changes != nil):
		return UserPreset{}, errors.New("a copy holds its values in preset; name and changes belong to a fork")
	case u.Origin != "" && !idPattern.MatchString(u.Origin):
		return UserPreset{}, fmt.Errorf("origin %q is not a preset ID", u.Origin)
	case strings.ContainsAny(u.Name, "\r\n\x00"):
		return UserPreset{}, errors.New("name must be one line")
	}
	for _, rid := range sortedIDs(u.Artifacts) {
		a := u.Artifacts[rid]
		if err := validateArtifact(rid, a.Artifact); err != nil {
			return UserPreset{}, fmt.Errorf("artifacts[%q]: %w", rid, err)
		}
		if a.SameModelAs != "" && (!idPattern.MatchString(a.SameModelAs) || a.SameModelAs == rid) {
			return UserPreset{}, fmt.Errorf("artifacts[%q]: same_model_as must name other weights", rid)
		}
	}
	for _, rid := range sortedIDs(u.Templates) {
		t := u.Templates[rid]
		if t.local() {
			// Temper reads a local template's size and hash from its file at
			// each compile, so the file holds neither.
			if len(t.Files) != 1 || t.Files[0].Bytes != 0 || t.Files[0].SHA256 != "" {
				return UserPreset{}, fmt.Errorf("templates[%q]: a local template names one file and no bytes or sha256; Temper reads them from the file", rid)
			}
			if !templatepatch.ValidLocalFile(t.Files[0].Path) {
				return UserPreset{}, fmt.Errorf("templates[%q]: a local template is a plain file name in presets.d, not a .json file", rid)
			}
			t.Files = []File{{Path: t.Files[0].Path, Bytes: 1, SHA256: strings.Repeat("0", 64)}}
		}
		if err := validateTemplate(rid, t); err != nil {
			return UserPreset{}, fmt.Errorf("templates[%q]: %w", rid, err)
		}
	}
	if u.Preset != nil && u.Preset.Speculation == (Speculation{}) {
		u.Preset.Speculation = Speculation{Method: "none", Source: "none"}
	}
	return u, nil
}

// LocalTemplates maps each local template record of the file to its file name.
func (u UserPreset) LocalTemplates() map[string]string {
	files := map[string]string{}
	for id, t := range u.Templates {
		if t.local() {
			files[id] = t.Files[0].Path
		}
	}
	return files
}

// ReadLocalTemplate gives a local template record the size and SHA-256 of
// its file's content as prepared, after its transform.
func (u *UserPreset) ReadLocalTemplate(id string, content []byte) error {
	t := u.Templates[id]
	final, err := templatepatch.Apply(t.Transform, content)
	if err != nil {
		return fmt.Errorf("templates[%q]: %w", id, err)
	}
	sum := sha256.Sum256(final)
	t.Files = []File{{Path: t.Files[0].Path, Bytes: int64(len(final)), SHA256: hex.EncodeToString(sum[:])}}
	u.Templates[id] = t
	return nil
}

// plainDecodeError says what the decoder found wrong in a user's file,
// without the decoder's own vocabulary.
func plainDecodeError(err error) string {
	text := strings.TrimPrefix(err.Error(), "decode catalog contract: ")
	text = strings.TrimPrefix(text, "yaml: unmarshal errors:\n")
	text = strings.TrimPrefix(text, "yaml: ")
	text = goTypes.ReplaceAllString(text, "")
	text = unknownField.ReplaceAllString(text, "unknown field $1")
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return "the file cannot be read: " + strings.Join(lines, "; ")
}

// MarshalUserPreset writes one presets.d file.
func MarshalUserPreset(u UserPreset) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(u); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// CopyOf makes a copy file from a compiled lock: its preset record with the
// one profile the lock selected, and every weights and template record, so the
// copy keeps its meaning when the catalog changes. The engine stays an ID.
func CopyOf(l Lock, name string) UserPreset {
	d := canonicalDocument(l.Records)
	p := d.Presets[l.Preset]
	if name == "" {
		name = displayName(p, l.Preset)
		if !strings.HasSuffix(name, " (yours)") {
			name += " (yours)"
		}
	}
	p.Presentation.DisplayName, p.Presentation.AssessmentURL = name, ""
	u := UserPreset{Schema: UserPresetSchema, Preset: &p, Artifacts: map[string]UserArtifact{}, Templates: d.Templates}
	for id, t := range u.Templates {
		if t.local() {
			// The copy keeps reading the same file at each compile.
			t.Files = []File{{Path: t.Files[0].Path}}
			u.Templates[id] = t
		}
	}
	for id, a := range d.Artifacts {
		u.Artifacts[id] = UserArtifact{Artifact: a}
	}
	if len(u.Templates) == 0 {
		u.Templates = nil
	}
	return u
}

func displayName(p Preset, id string) string {
	if p.Presentation.DisplayName != "" {
		return p.Presentation.DisplayName
	}
	return id
}

// ResolveUserPreset builds the one-preset document that compiles user preset
// id against a catalog view: the preset with the file's records first, then
// the catalog's, and the catalog's engine and router. The warnings name file
// records that shadow a different catalog record.
func ResolveUserPreset(id string, u UserPreset, view View) (Document, []string, error) {
	d := view.Document
	var p Preset
	if u.Fork() {
		origin, ok := d.Presets[u.Origin]
		if !ok {
			return Document{}, nil, fmt.Errorf("origin %q %s", u.Origin, unavailable(view, "preset", u.Origin))
		}
		var err error
		if p, err = applyChanges(origin, u.Changes); err != nil {
			return Document{}, nil, err
		}
		name := u.Name
		if name == "" {
			name = displayName(origin, u.Origin) + " (yours)"
		}
		p.Presentation.DisplayName, p.Presentation.AssessmentURL = name, ""
		if p.Artifact != origin.Artifact && u.Artifacts[p.Artifact].SameModelAs != origin.Artifact {
			return Document{}, nil, fmt.Errorf("a fork keeps its origin's model: weights %q are not marked same_model_as %q; use temper preset copy for another model", p.Artifact, origin.Artifact)
		}
	} else {
		p = *u.Preset
		for _, ref := range []string{p.Artifact, p.ProjectorArtifact, p.Speculation.DraftArtifact} {
			if _, ok := u.Artifacts[ref]; ref != "" && !ok {
				return Document{}, nil, fmt.Errorf("a copy holds its own weights records: add artifacts[%q]", ref)
			}
		}
		if _, ok := u.Templates[p.Template]; p.patched() && !ok {
			return Document{}, nil, fmt.Errorf("a copy holds its own template record: add templates[%q]", p.Template)
		}
		if strings.TrimSpace(p.Presentation.DisplayName) == "" {
			return Document{}, nil, errors.New("a copy needs presentation.display_name")
		}
	}
	e, ok := d.Engines[p.Engine]
	if !ok {
		return Document{}, nil, fmt.Errorf("engine %q %s", p.Engine, unavailable(view, "engine", p.Engine))
	}
	closure := Document{Schema: Schema, Date: d.Date, Platform: d.Platform, Runtime: d.Runtime,
		Artifacts: map[string]Artifact{}, Templates: map[string]Template{}, Engines: map[string]Engine{p.Engine: e},
		Presets: map[string]Preset{id: p}}
	var warnings []string
	// A file record with a catalog record's ID but other material: a copy
	// keeps its own (with a warning); a fork changes records only through
	// changes, so it may not replace one under the same ID.
	var shadowed error
	shadows := func(kind, rid string, file, record any, inCatalog bool) {
		if !inCatalog || runs(file) == runs(record) {
			return
		}
		if u.Fork() {
			shadowed = errors.Join(shadowed, fmt.Errorf("%s[%q] has a catalog record's ID but other files; a fork changes records through changes, so give it its own ID and select it there", kind, rid))
			return
		}
		warnings = append(warnings, fmt.Sprintf("%s[%q] in this file differs from the catalog's %s with that ID; the file's is used", kind, rid, strings.TrimSuffix(kind, "s")))
	}
	for _, ref := range []string{p.Artifact, p.ProjectorArtifact, p.Speculation.DraftArtifact} {
		if ref == "" {
			continue
		}
		if a, ok := u.Artifacts[ref]; ok {
			catalogRecord, inCatalog := d.Artifacts[ref]
			shadows("artifacts", ref, a.Artifact, catalogRecord, inCatalog)
			closure.Artifacts[ref] = a.Artifact
		} else if a, ok := d.Artifacts[ref]; ok {
			closure.Artifacts[ref] = a
		} else {
			return Document{}, nil, fmt.Errorf("weights %q are neither in this file nor %s", ref, unavailable(view, "artifact", ref))
		}
	}
	if p.patched() {
		t, inFile := u.Templates[p.Template]
		if inFile {
			catalogRecord, inCatalog := d.Templates[p.Template]
			shadows("templates", p.Template, t, catalogRecord, inCatalog)
		} else if t, ok = d.Templates[p.Template]; !ok {
			return Document{}, nil, fmt.Errorf("template %q is neither in this file nor %s", p.Template, unavailable(view, "template", p.Template))
		}
		// A template accepts weights it lists, or user weights that are another
		// build of weights it lists. A lock lists the weights it accepted.
		if !slices.Contains(t.CompatibleArtifacts, p.Artifact) {
			same := u.Artifacts[p.Artifact].SameModelAs
			if same == "" || !slices.Contains(t.CompatibleArtifacts, same) {
				return Document{}, nil, fmt.Errorf("template %q does not accept weights %q: they are not in its compatible_artifacts, and neither is their same_model_as", p.Template, p.Artifact)
			}
			t.CompatibleArtifacts = append(slices.Clone(t.CompatibleArtifacts), p.Artifact)
		}
		closure.Templates[p.Template] = t
	}
	if shadowed != nil {
		return Document{}, nil, shadowed
	}
	if err := closure.validateRecords(); err != nil {
		return Document{}, nil, err
	}
	return closure, warnings, nil
}

// unavailable says why a catalog record is missing from the view.
func unavailable(view View, kind, id string) string {
	for _, u := range view.Unavailable {
		if u.Kind == kind && u.ID == id {
			return "is unavailable: " + u.Reason
		}
	}
	return "is not in this catalog"
}

// runs is what of a weights or template record reaches the engine: its
// sources, format or transform and files, in order. License, presentation
// and a template's accepted weights are left out.
func runs(record any) string {
	var value any
	switch r := record.(type) {
	case Artifact:
		value = Artifact{Sources: r.Sources, Format: r.Format, Files: r.Files}
	case Template:
		value = Template{Sources: r.Sources, Transform: r.Transform, Files: r.Files}
	}
	d := Document{Artifacts: map[string]Artifact{}, Templates: map[string]Template{}}
	switch v := value.(type) {
	case Artifact:
		d.Artifacts["record"] = v
	case Template:
		d.Templates["record"] = v
	}
	raw, _ := json.Marshal(canonicalDocument(d))
	return string(raw)
}

// applyChanges merges a fork's changes into its origin. Every change must be
// a field of the origin's engine kind; engine_config and context_window_tokens
// apply to every profile, so the one this Mac selects has them.
func applyChanges(origin Preset, changes map[string]any) (Preset, error) {
	if err := checkChanges(changes, Fields(origin.Config().Kind), ""); err != nil {
		return Preset{}, err
	}
	raw, err := json.Marshal(origin)
	if err != nil {
		return Preset{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var record map[string]any
	if err := decoder.Decode(&record); err != nil {
		return Preset{}, err
	}
	for key, value := range changes {
		switch key {
		case "engine_config", "context_window_tokens":
			for _, profile := range record["profiles"].([]any) {
				profile := profile.(map[string]any)
				profile[key] = merge(profile[key], value)
			}
		default:
			record[key] = merge(record[key], value)
		}
	}
	if raw, err = json.Marshal(record); err != nil {
		return Preset{}, err
	}
	var result Preset
	if err := json.Unmarshal(raw, &result); err != nil {
		return Preset{}, fmt.Errorf("changes: %w", err)
	}
	return result, nil
}

func merge(into, change any) any {
	target, ok := into.(map[string]any)
	values, isMap := change.(map[string]any)
	if !ok || !isMap {
		return change
	}
	for key, value := range values {
		target[key] = merge(target[key], value)
	}
	return target
}

// checkChanges accepts only field-table paths with values of their type.
func checkChanges(changes map[string]any, fields []Field, prefix string) error {
	keys := make([]string, 0, len(changes))
	for key := range changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if strings.Contains(key, ".") {
			return fmt.Errorf("changes.%s: nest the names instead of joining them with dots, for example {\"request_defaults\": {\"sampling\": {\"temperature\": 0.7}}}", path)
		}
		f, ok, group := field(fields, path)
		switch {
		case ok:
			if err := checkValue(f, changes[key]); err != nil {
				return fmt.Errorf("changes.%s: %w", path, err)
			}
		case group:
			nested, isMap := changes[key].(map[string]any)
			if !isMap {
				return fmt.Errorf("changes.%s holds settings, not a value", path)
			}
			if err := checkChanges(nested, fields, path); err != nil {
				return err
			}
		default:
			return fmt.Errorf("changes.%s is not a setting a fork can change; temper preset fields lists them", path)
		}
	}
	return nil
}

func checkValue(f Field, value any) error {
	if value == nil {
		return errors.New("cannot be removed: a fork only replaces values")
	}
	ok := false
	switch f.Type {
	case FieldInteger:
		_, ok = value.(int)
	case FieldNumber:
		switch value.(type) {
		case int, float64:
			ok = true
		}
	case FieldSwitch:
		_, ok = value.(bool)
	case FieldText, FieldChoice:
		var text string
		text, ok = value.(string)
		if ok && f.Type == FieldChoice && !slices.Contains(f.Choices, text) {
			return fmt.Errorf("must be one of %s", strings.Join(f.Choices, ", "))
		}
	}
	if !ok {
		return fmt.Errorf("must be a %s", map[string]string{FieldInteger: "whole number", FieldNumber: "number", FieldSwitch: "true or false", FieldText: "text", FieldChoice: "text"}[f.Type])
	}
	return nil
}

func sortedIDs[V any](m map[string]V) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Change is one setting that differs between two builds of a preset.
type Change struct {
	Path string `json:"path"`
	From any    `json:"from"`
	To   any    `json:"to"`
}

// Values are a preset's settings by field path, read from its first profile,
// which in a lock is the selected one.
func Values(p Preset) map[string]any {
	p.Profiles = p.Profiles[:min(len(p.Profiles), 1)]
	raw, _ := json.Marshal(p)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var record map[string]any
	_ = decoder.Decode(&record)
	values := map[string]any{}
	var walk func(any, string)
	walk = func(value any, prefix string) {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if prefix == "" && key == "profiles" {
					if profiles, _ := item.([]any); len(profiles) == 1 {
						walk(profiles[0], "")
					}
					continue
				}
				path := key
				if prefix != "" {
					path = prefix + "." + key
				}
				walk(item, path)
			}
		default:
			values[prefix] = v
		}
	}
	walk(record, "")
	return values
}

// PresetChanges lists the settings that differ between two one-profile
// presets, such as a fork's lock and its origin's, by field path. Display
// text is left out.
func PresetChanges(from, to Preset) []Change {
	a, b := Values(from), Values(to)
	var changes []Change
	for _, path := range sortedIDs(a) {
		if !strings.HasPrefix(path, "presentation.") && !reflect.DeepEqual(a[path], b[path]) {
			changes = append(changes, Change{Path: path, From: a[path], To: b[path]})
		}
	}
	for _, path := range sortedIDs(b) {
		if _, ok := a[path]; !ok && !strings.HasPrefix(path, "presentation.") {
			changes = append(changes, Change{Path: path, To: b[path]})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}
