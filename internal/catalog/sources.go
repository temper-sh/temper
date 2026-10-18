package catalog

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

const (
	HubHuggingFace = "huggingface"
	HubModelScope  = "modelscope"
	// HubLocal is a template file in the root's presets.d. Only user presets
	// and their locks have it; a catalog never does.
	HubLocal = "local"
)

// Source is one hub that serves an artifact's or template's exact files at a
// pinned commit. Hub commits differ, so the revision belongs to the source.
type Source struct {
	Hub      string `yaml:"hub" json:"hub"`
	Repo     string `yaml:"repo" json:"repo"`
	Revision string `yaml:"revision" json:"revision"`
	// unusable says why this Temper cannot use the entry; never serialized.
	unusable string
}

// UnmarshalYAML keeps an entry this Temper cannot use instead of failing the
// record: a future hub or mirror is an addition, not a hidden record.
func (s *Source) UnmarshalYAML(node *yaml.Node) error {
	var value struct {
		Hub      string `yaml:"hub"`
		Repo     string `yaml:"repo"`
		Revision string `yaml:"revision"`
	}
	*s = Source{}
	if err := decodeNode(node, &value); err != nil {
		s.unusable = err.Error()
		return nil
	}
	s.Hub, s.Repo, s.Revision = value.Hub, value.Repo, value.Revision
	s.unusable = s.problem()
	return nil
}

// problem says why this Temper cannot use the source, or "".
func (s Source) problem() string {
	switch {
	case s.unusable != "":
		return s.unusable
	case s.Hub == HubLocal:
		if s.Repo != "" || s.Revision != "" {
			return "a local source has no repository or revision"
		}
		return ""
	case s.Hub != HubHuggingFace && s.Hub != HubModelScope:
		return fmt.Sprintf("unknown hub %q", s.Hub)
	case !repoPattern.MatchString(s.Repo) || !revisionPattern.MatchString(s.Revision):
		return "needs an owner/name repository and an exact 40-character commit"
	}
	return ""
}

// validateSources is the strict rule: every entry is usable, hubs do not
// repeat, and there is a Hugging Face source, the only one 0.1.0 downloads.
// A local source, where allowed, stands alone.
func validateSources(sources []Source, local bool) error {
	if len(sources) == 1 && sources[0].Hub == HubLocal {
		if !local {
			return errors.New("only templates have local files")
		}
		return sources[0].validLocal()
	}
	seen := map[string]bool{}
	for _, s := range sources {
		if problem := s.problem(); problem != "" {
			return fmt.Errorf("source: %s", problem)
		}
		if s.Hub == HubLocal {
			return errors.New("a local source stands alone")
		}
		if seen[s.Hub] {
			return fmt.Errorf("source hub %q repeats", s.Hub)
		}
		seen[s.Hub] = true
	}
	if !seen[HubHuggingFace] {
		return errors.New("needs a Hugging Face source; this Temper downloads from Hugging Face only")
	}
	return nil
}

func (s Source) validLocal() error {
	if problem := s.problem(); problem != "" {
		return fmt.Errorf("source: %s", problem)
	}
	return nil
}

// local reports whether a template's file is in the root's presets.d.
func (t Template) local() bool { return len(t.Sources) == 1 && t.Sources[0].Hub == HubLocal }

// usableSources drops the entries this Temper cannot use.
func usableSources(sources []Source) []Source {
	var result []Source
	for _, s := range sources {
		if s.problem() == "" {
			result = append(result, s)
		}
	}
	return result
}

func huggingFace(sources []Source) Source {
	for _, s := range sources {
		if s.Hub == HubHuggingFace && s.problem() == "" {
			return s
		}
	}
	return Source{}
}
