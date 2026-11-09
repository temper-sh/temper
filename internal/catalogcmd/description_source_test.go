package catalogcmd

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDescriptionSourceKeepsDateTextAndIsolatesYAMLAliases(t *testing.T) {
	raw := []byte(`date: 2026-11-09
presets:
  edited:
    presentation: &shared
      description: Original
      assessment_url: https://example.test/original
  untouched:
    presentation: *shared
`)
	written, err := descriptionSource(raw, "edited", "Changed", nil)
	if err != nil {
		t.Fatal(err)
	}
	var actual struct {
		Date    string
		Presets map[string]struct {
			Presentation struct {
				Description string
				URL         string `yaml:"assessment_url"`
			}
		}
	}
	if err := yaml.Unmarshal(written, &actual); err != nil {
		t.Fatal(err)
	}
	if actual.Date != "2026-11-09" {
		t.Fatalf("date changed: %q", actual.Date)
	}
	if actual.Presets["edited"].Presentation.Description != "Changed" || actual.Presets["untouched"].Presentation.Description != "Original" {
		t.Fatal("edit did not stay local to the selected preset")
	}
	if actual.Presets["edited"].Presentation.URL != "https://example.test/original" || actual.Presets["untouched"].Presentation.URL != "https://example.test/original" {
		t.Fatal("description-only edit changed assessment URLs")
	}
}
