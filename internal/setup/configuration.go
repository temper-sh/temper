package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/harness"
)

const ConfigurationSchema = "temper-configuration/v1"
const ConfigurationFile = "configuration.json"

var choiceID = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

// Preset is one saved exact preset. Choices hold only what the user chose
// explicitly. UserPreset identifies an authored file in presets.d; otherwise
// the lock's preset ID identifies the origin catalog preset.
type Preset struct {
	Name         string        `json:"name"`
	UserPreset   string        `json:"user_preset,omitempty"`
	ForkBaseline *ForkBaseline `json:"fork_baseline,omitempty"`
	Choices      Choices       `json:"choices"`
	Lock         catalog.Lock  `json:"lock"`
}

// Choices are explicit user settings for one saved preset; zero means the
// catalog's value.
type Choices struct {
	ContextWindow int                   `json:"context_window,omitempty"`
	Compute       catalog.ComputeDevice `json:"compute,omitempty"`
}

type Layout struct {
	Name        string   `json:"name"`
	Presets     []string `json:"presets"`
	Startup     []string `json:"startup"`
	Default     string   `json:"default,omitempty"`
	IdleSeconds int      `json:"idle_seconds"`
}

// Configuration is the user's file. Software is one setting for every preset:
// a layout runs one router.
type Configuration struct {
	Schema       string              `json:"schema"`
	Software     string              `json:"software"`
	Presets      map[string]Preset   `json:"presets"`
	Layouts      map[string]Layout   `json:"layouts"`
	Integrations []harness.Selection `json:"integrations,omitempty"`
}

func EmptyConfiguration() Configuration {
	return Configuration{Schema: ConfigurationSchema, Software: catalog.SoftwareStable, Presets: map[string]Preset{}, Layouts: map[string]Layout{}}
}

func StarterLayouts(presets []string) map[string]Layout {
	return map[string]Layout{"default": {Name: "Default", Presets: append([]string{}, presets...), Startup: []string{}, IdleSeconds: 1800}}
}

func validName(s string) bool {
	return strings.TrimSpace(s) != "" && !strings.ContainsAny(s, "\x00\r\n\x1b")
}

func (c Configuration) Validate() error {
	if err := harness.Validate(c.Integrations); err != nil {
		return err
	}
	if c.Schema != ConfigurationSchema || c.Presets == nil || c.Layouts == nil {
		return errors.New("configuration requires temper-configuration/v1, presets and layouts")
	}
	if c.Software != catalog.SoftwareStable && c.Software != catalog.SoftwareLatest {
		return errors.New("configuration software must be stable or latest")
	}
	for id, p := range c.Presets {
		if !choiceID.MatchString(id) || !validName(p.Name) {
			return fmt.Errorf("preset %q requires a stable ID and name", id)
		}
		if p.UserPreset != "" && (!catalog.ValidPresetID(p.UserPreset) || p.Lock.Preset != p.UserPreset) {
			return fmt.Errorf("preset %q user_preset must name its exact lock's source preset", id)
		}
		if b := p.ForkBaseline; b != nil && (p.UserPreset == "" || !catalog.ValidPresetID(b.Origin) || b.Values == nil) {
			return fmt.Errorf("preset %q fork_baseline requires a user preset, catalog origin and values", id)
		}
		if p.Choices.ContextWindow < 0 || p.Choices.Compute != "" && p.Choices.Compute != catalog.ComputeCPU && p.Choices.Compute != catalog.ComputeGPU {
			return fmt.Errorf("preset %q has an invalid choice", id)
		}
		if err := p.Lock.Validate(); err != nil {
			return fmt.Errorf("preset %q: %w", id, err)
		}
		if len(p.Lock.Records.Presets) != 1 {
			return fmt.Errorf("preset %q requires one exact engine configuration", id)
		}
	}
	for id, l := range c.Layouts {
		if !choiceID.MatchString(id) || !validName(l.Name) || l.IdleSeconds <= 0 {
			return fmt.Errorf("layout %q requires a stable ID, name and positive idle_seconds", id)
		}
		seen := map[string]bool{}
		for _, p := range l.Presets {
			if _, ok := c.Presets[p]; !ok {
				return fmt.Errorf("layout %q references removed or unselected preset %q; explicitly correct membership, startup and default", id, p)
			}
			if seen[p] {
				return fmt.Errorf("layout %q repeats preset %q", id, p)
			}
			seen[p] = true
		}
		startup := map[string]bool{}
		for _, p := range l.Startup {
			if !seen[p] || startup[p] {
				return fmt.Errorf("layout %q startup %q must be included exactly once", id, p)
			}
			startup[p] = true
		}
		if l.Default != "" && !seen[l.Default] {
			return fmt.Errorf("layout %q default %q must be included; choose a replacement or explicitly clear it", id, l.Default)
		}
	}
	return nil
}

func ParseConfiguration(raw []byte) (Configuration, error) {
	var c Configuration
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return c, errors.New("configuration must contain exactly one JSON value")
	}
	return c, c.Validate()
}

func (c Configuration) Bytes() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	return append(raw, '\n'), err
}

func revision(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// ReadConfiguration is read-only, including for an absent root.
func ReadConfiguration(root string) (Configuration, string, error) {
	if _, err := ExistingDirectory(root); err != nil {
		return Configuration{}, "", err
	}
	if err := datadir.Check(root); err != nil {
		return Configuration{}, "", err
	}
	path := filepath.Join(root, ConfigurationFile)
	raw, err := readRegular(path, 32<<20)
	if errors.Is(err, os.ErrNotExist) {
		return EmptyConfiguration(), "", nil
	}
	if err != nil {
		return Configuration{}, "", fmt.Errorf("read saved configuration %q: %w", path, err)
	}
	c, err := ParseConfiguration(raw)
	if err != nil {
		err = fmt.Errorf("parse saved configuration %q: %w", path, err)
	}
	return c, revision(raw), err
}

// SaveConfiguration is a compare-and-swap publication. The OS lock expires with
// the writer process; a crash can leave only an unreferenced temporary file.
func SaveConfiguration(ctx context.Context, root string, c Configuration, expected string, dry bool) (string, bool, error) {
	raw, err := c.Bytes()
	if err != nil {
		return "", false, err
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if _, err := ExistingDirectory(root); err != nil {
		return "", false, err
	}
	check := func() (string, bool, error) {
		old, rev, err := ReadConfiguration(root)
		if err != nil {
			return "", false, err
		}
		if rev != expected {
			return "", false, fmt.Errorf("configuration changed (revision %s); reload and review before saving", rev)
		}
		previous, _ := old.Bytes()
		changed := rev == "" || !bytes.Equal(raw, previous)
		if !changed {
			return rev, false, nil
		}
		return revision(raw), true, nil
	}
	if dry {
		return check()
	}
	if err := datadir.Ensure(root); err != nil {
		return "", false, err
	}
	unlock, err := lockRoot(root)
	if err != nil {
		return "", false, err
	}
	defer unlock()
	rev, changed, err := check()
	if err != nil || !changed {
		return rev, changed, err
	}
	f, err := os.CreateTemp(root, ".configuration-*")
	if err != nil {
		return "", false, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", false, err
	}
	if closeErr != nil {
		return "", false, closeErr
	}
	if err = ctx.Err(); err != nil {
		return "", false, err
	}
	if _, _, err = check(); err != nil {
		return "", false, err
	}
	if err = os.Rename(f.Name(), filepath.Join(root, ConfigurationFile)); err != nil {
		return "", false, err
	}
	if err = syncDir(root); err != nil {
		return rev, true, fmt.Errorf("configuration saved but directory sync failed: %w", err)
	}
	return rev, true, nil
}

func (c Configuration) References(preset string) []string {
	var names []string
	for id, l := range c.Layouts {
		if slices.Contains(l.Presets, preset) {
			names = append(names, id)
		}
	}
	slices.Sort(names)
	return names
}
