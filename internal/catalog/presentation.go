package catalog

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Presentation values are tolerant: an unknown key is ignored and an invalid
// value falls back, so presentation never hides a record. The issues stay
// for the publisher's strict check; they are never serialized.

type ArtifactPresentation struct {
	ModelName   string `yaml:"model_name,omitempty" json:"model_name,omitempty"`
	WeightsName string `yaml:"weights_name,omitempty" json:"weights_name,omitempty"`
	issues      []string
}

func (p *ArtifactPresentation) UnmarshalYAML(node *yaml.Node) error {
	*p = ArtifactPresentation{}
	p.issues = decodeText(node, map[string]*string{"model_name": &p.ModelName, "weights_name": &p.WeightsName})
	return nil
}

type TemplatePresentation struct {
	DisplayName string `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Version     string `yaml:"version,omitempty" json:"version,omitempty"`
	issues      []string
}

func (p *TemplatePresentation) UnmarshalYAML(node *yaml.Node) error {
	*p = TemplatePresentation{}
	p.issues = decodeText(node, map[string]*string{"display_name": &p.DisplayName, "description": &p.Description, "version": &p.Version})
	return nil
}

type EnginePresentation struct {
	DisplayName string `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	issues      []string
}

func (p *EnginePresentation) UnmarshalYAML(node *yaml.Node) error {
	*p = EnginePresentation{}
	p.issues = decodeText(node, map[string]*string{"display_name": &p.DisplayName})
	return nil
}

// PresetPresentation places a preset in a group and an optional section and
// explains why to choose it. It is never part of what runs.
type PresetPresentation struct {
	DisplayName   string `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Description   string `yaml:"description,omitempty" json:"description,omitempty"`
	AssessmentURL string `yaml:"assessment_url,omitempty" json:"assessment_url,omitempty"`
	Group         string `yaml:"group,omitempty" json:"group,omitempty"`
	Section       string `yaml:"section,omitempty" json:"section,omitempty"`
	MemoryTier    string `yaml:"memory_tier,omitempty" json:"memory_tier,omitempty"`
	issues        []string
}

func (p *PresetPresentation) UnmarshalYAML(node *yaml.Node) error {
	*p = PresetPresentation{}
	p.issues = decodeText(node, map[string]*string{"display_name": &p.DisplayName, "description": &p.Description,
		"assessment_url": &p.AssessmentURL, "group": &p.Group, "section": &p.Section, "memory_tier": &p.MemoryTier})
	if p.AssessmentURL != "" && !webURL(p.AssessmentURL) {
		p.issues = append(p.issues, "assessment_url must be an HTTP(S) URL")
		p.AssessmentURL = ""
	}
	if p.MemoryTier != "" && MemoryTier(p.MemoryTier).Rank() < 0 {
		p.issues = append(p.issues, fmt.Sprintf("unknown memory_tier %q", p.MemoryTier))
		p.MemoryTier = ""
	}
	for _, id := range []*string{&p.Group, &p.Section} {
		if *id != "" && !idPattern.MatchString(*id) {
			p.issues = append(p.issues, fmt.Sprintf("invalid group or section id %q", *id))
			*id = ""
		}
	}
	return nil
}

// Presentation is what describes no single record: the ordered groups (tabs)
// with their sections, the fallback group and the preset order.
type Presentation struct {
	Groups        []Group  `yaml:"groups,omitempty" json:"groups,omitempty"`
	FallbackGroup string   `yaml:"fallback_group,omitempty" json:"fallback_group,omitempty"`
	PresetOrder   []string `yaml:"preset_order,omitempty" json:"preset_order,omitempty"`
	issues        []string
}

type Group struct {
	ID          string    `yaml:"id" json:"id"`
	DisplayName string    `yaml:"display_name" json:"display_name"`
	Description string    `yaml:"description,omitempty" json:"description,omitempty"`
	Sections    []Section `yaml:"sections,omitempty" json:"sections,omitempty"`
}

type Section struct {
	ID          string `yaml:"id" json:"id"`
	DisplayName string `yaml:"display_name" json:"display_name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

func (p *Presentation) UnmarshalYAML(node *yaml.Node) error {
	*p = Presentation{}
	if node.Kind != yaml.MappingNode {
		p.issues = []string{"presentation must be a mapping"}
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		switch key {
		case "groups":
			for _, item := range sequence(value, &p.issues, key) {
				var g Group
				fields := map[string]*string{"id": &g.ID, "display_name": &g.DisplayName, "description": &g.Description}
				p.issues = append(p.issues, decodeTextExcept(item, fields, "sections")...)
				if sections := child(item, "sections"); sections != nil {
					for _, raw := range sequence(sections, &p.issues, "sections") {
						var s Section
						p.issues = append(p.issues, decodeText(raw, map[string]*string{"id": &s.ID, "display_name": &s.DisplayName, "description": &s.Description})...)
						if idPattern.MatchString(s.ID) {
							g.Sections = append(g.Sections, s)
						} else {
							p.issues = append(p.issues, fmt.Sprintf("section id %q is invalid", s.ID))
						}
					}
				}
				if idPattern.MatchString(g.ID) {
					p.Groups = append(p.Groups, g)
				} else {
					p.issues = append(p.issues, fmt.Sprintf("group id %q is invalid", g.ID))
				}
			}
		case "fallback_group":
			p.issues = append(p.issues, decodeScalar(value, &p.FallbackGroup, key)...)
		case "preset_order":
			for _, item := range sequence(value, &p.issues, key) {
				var id string
				p.issues = append(p.issues, decodeScalar(item, &id, key)...)
				if id != "" {
					p.PresetOrder = append(p.PresetOrder, id)
				}
			}
		default:
			p.issues = append(p.issues, fmt.Sprintf("unknown presentation key %q", key))
		}
	}
	return nil
}

func sequence(node *yaml.Node, issues *[]string, key string) []*yaml.Node {
	if node.Kind != yaml.SequenceNode {
		*issues = append(*issues, fmt.Sprintf("presentation %q must be a list", key))
		return nil
	}
	return node.Content
}

func child(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// decodeText reads text fields from a mapping. Unknown keys and non-text
// values are issues; control characters are removed for terminal safety.
func decodeText(node *yaml.Node, fields map[string]*string) []string {
	return decodeTextExcept(node, fields)
}

func decodeTextExcept(node *yaml.Node, fields map[string]*string, handled ...string) []string {
	if node.Kind != yaml.MappingNode {
		return []string{"presentation must be a mapping"}
	}
	var issues []string
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		if slices.Contains(handled, key) {
			continue
		}
		target, ok := fields[key]
		if !ok {
			issues = append(issues, fmt.Sprintf("unknown presentation key %q", key))
			continue
		}
		issues = append(issues, decodeScalar(value, target, key)...)
	}
	return issues
}

func decodeScalar(node *yaml.Node, target *string, key string) []string {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return []string{fmt.Sprintf("presentation %q must be text", key)}
	}
	clean := terminalText(node.Value)
	*target = clean
	if clean != node.Value {
		return []string{fmt.Sprintf("presentation %q contains control characters", key)}
	}
	return nil
}

// LicenseURL links known licenses to readable guidance or the publisher's terms.
// Other licenses stay attached to the exact model source instead of borrowing
// an explanation for a license that may have different conditions.
func (a Artifact) LicenseURL() string {
	switch strings.ToLower(strings.TrimSpace(a.License)) {
	case "apache-2.0":
		return "https://choosealicense.com/licenses/apache-2.0/"
	case "gemma":
		return "https://ai.google.dev/gemma/terms"
	case "cc-by-nc-4.0":
		return "https://creativecommons.org/licenses/by-nc/4.0/"
	default:
		hf := a.HuggingFace()
		return "https://huggingface.co/" + hf.Repo + "/blob/" + hf.Revision + "/README.md"
	}
}

// OtherGroup is the built-in group for presets whose group is unknown or
// missing when a catalog names no fallback, and for saved presets.
var OtherGroup = Group{ID: "other", DisplayName: "Other", Description: "Additional presets and saved choices outside the curated recommendations. Review their intended use and limits."}

// Groups returns the catalog's groups in order, always including the
// fallback group, which is built in when the catalog names none.
func (d Document) Groups() []Group {
	var groups []Group
	if d.Presentation != nil {
		for _, g := range d.Presentation.Groups {
			if !slices.ContainsFunc(groups, func(seen Group) bool { return seen.ID == g.ID }) {
				groups = append(groups, g)
			}
		}
	}
	if !slices.ContainsFunc(groups, func(g Group) bool { return g.ID == d.FallbackGroup() }) {
		groups = append(groups, OtherGroup)
	}
	return groups
}

// FallbackGroup collects presets with an unknown or missing group.
func (d Document) FallbackGroup() string {
	if d.Presentation != nil {
		for _, g := range d.Presentation.Groups {
			if g.ID == d.Presentation.FallbackGroup {
				return g.ID
			}
		}
	}
	return OtherGroup.ID
}

// Placement returns the group a preset is shown in and its section heading,
// if any. An unknown group falls back; an unknown section shows no heading.
func (d Document) Placement(p PresetPresentation) (Group, *Section) {
	groups := d.Groups()
	index := slices.IndexFunc(groups, func(g Group) bool { return g.ID == p.Group })
	if index < 0 {
		index = slices.IndexFunc(groups, func(g Group) bool { return g.ID == d.FallbackGroup() })
	}
	group := groups[index]
	for _, s := range group.Sections {
		if s.ID == p.Section {
			return group, &s
		}
	}
	return group, nil
}

// PresetRank orders presets by the catalog's preset_order; others follow.
func (d Document) PresetRank(id string) int {
	if d.Presentation == nil {
		return 0
	}
	if v := slices.Index(d.Presentation.PresetOrder, id); v >= 0 {
		return v
	}
	return len(d.Presentation.PresetOrder)
}

// MemoryTier describes expected machine capacity, independently of model file
// size and of the machine checks for a selected execution configuration.
type MemoryTier string

var memoryTiers = []MemoryTier{"XS", "S", "M", "L", "XL", "XXL"}

func (t MemoryTier) Rank() int { return slices.Index(memoryTiers, t) }

func (t MemoryTier) Label() string {
	switch t {
	case "XS":
		return "XS · up to 16 GB"
	case "S":
		return "S · >16–32 GB"
	case "M":
		return "M · >32–64 GB"
	case "L":
		return "L · >64–128 GB"
	case "XL":
		return "XL · >128–256 GB"
	case "XXL":
		return "XXL · >256–512 GB"
	default:
		return "Memory tier unspecified"
	}
}

// validatePresentation is the publisher's editorial check: every presentation
// value is understood and valid, groups and sections are complete, every
// preset is placed and described, and preset_order lists every preset once.
func (d Document) validatePresentation() error {
	var issues []string
	add := func(owner string, values []string) {
		for _, v := range values {
			issues = append(issues, owner+": "+v)
		}
	}
	if d.Presentation == nil {
		return errors.New("catalog presentation with groups and preset_order is required")
	}
	add("presentation", d.Presentation.issues)
	for id, a := range d.Artifacts {
		add("artifact "+id, a.Presentation.issues)
	}
	for id, t := range d.Templates {
		add("template "+id, t.Presentation.issues)
	}
	for id, e := range d.Engines {
		add("engine "+id, e.Presentation.issues)
	}
	for id, p := range d.Presets {
		add("preset "+id, p.Presentation.issues)
	}
	if len(issues) > 0 {
		slices.Sort(issues)
		return fmt.Errorf("presentation: %s", strings.Join(issues, "; "))
	}
	groups := map[string]Group{}
	for _, g := range d.Presentation.Groups {
		if _, repeated := groups[g.ID]; repeated || strings.TrimSpace(g.DisplayName) == "" || strings.TrimSpace(g.Description) == "" {
			return fmt.Errorf("group %q needs a unique id, a display_name and a description", g.ID)
		}
		sections := map[string]bool{}
		for _, s := range g.Sections {
			if sections[s.ID] || strings.TrimSpace(s.DisplayName) == "" || strings.TrimSpace(s.Description) == "" {
				return fmt.Errorf("group %q section %q needs a unique id, a display_name and a description", g.ID, s.ID)
			}
			sections[s.ID] = true
		}
		groups[g.ID] = g
	}
	if _, ok := groups[d.Presentation.FallbackGroup]; !ok {
		return fmt.Errorf("fallback_group %q must name a group", d.Presentation.FallbackGroup)
	}
	for id, p := range d.Presets {
		pp := p.Presentation
		if strings.TrimSpace(pp.DisplayName) == "" || strings.TrimSpace(pp.Description) == "" {
			return fmt.Errorf("preset %q needs a display_name and a description", id)
		}
		g, ok := groups[pp.Group]
		if !ok {
			return fmt.Errorf("preset %q names unknown group %q", id, pp.Group)
		}
		if pp.Section != "" && !slices.ContainsFunc(g.Sections, func(s Section) bool { return s.ID == pp.Section }) {
			return fmt.Errorf("preset %q section %q does not belong to group %q", id, pp.Section, pp.Group)
		}
		if pp.Section == "" && len(g.Sections) > 0 {
			return fmt.Errorf("preset %q needs a section of group %q", id, pp.Group)
		}
		if p.Interface == "chat-completions" && p.Template == "" {
			return fmt.Errorf("chat preset %q must name its template (%q for the model's own)", id, BuiltinTemplate)
		}
	}
	seen := map[string]bool{}
	for _, id := range d.Presentation.PresetOrder {
		if _, ok := d.Presets[id]; !ok || seen[id] {
			return fmt.Errorf("preset_order names an unknown or repeated preset %q", id)
		}
		seen[id] = true
	}
	if len(seen) != len(d.Presets) {
		return errors.New("preset_order must list every preset")
	}
	return nil
}
