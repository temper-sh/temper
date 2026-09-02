package pinning_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/pinning"
	"github.com/temper-sh/temper/internal/upstream"
)

type snapshotSource struct {
	calls int
	pin   upstream.SnapshotPin
}

func (s *snapshotSource) Resolve(context.Context, string, string) (upstream.FilePin, error) {
	return upstream.FilePin{}, errors.New("per-file resolution must not run")
}

func (s *snapshotSource) ResolveSnapshot(_ context.Context, repo string, files []string) (upstream.SnapshotPin, error) {
	s.calls++
	if repo != "owner/model" || strings.Join(files, ",") != "config.json,model.safetensors" {
		return upstream.SnapshotPin{}, errors.New("unexpected snapshot request")
	}
	return s.pin, nil
}

func (s *snapshotSource) Open(context.Context, string, string, string) (io.ReadCloser, error) {
	return nil, errors.New("open must not run")
}

func TestResolveLayoutsPinsOneCompleteSnapshot(t *testing.T) {
	source := &snapshotSource{pin: upstream.SnapshotPin{
		Revision: strings.Repeat("a", 40),
		Files: []upstream.SnapshotFilePin{
			{Name: "config.json", SHA256: strings.Repeat("b", 64)},
			{Name: "model.safetensors", SHA256: strings.Repeat("c", 64)},
		},
	}}
	document := manifest.Document{Layouts: map[string]manifest.Layout{
		"large": {Model: manifest.Model{Repo: "owner/model", Files: []string{"config.json", "model.safetensors"}}},
	}}
	entries, err := pinning.ResolveLayouts(context.Background(), document, []string{"large"}, "2026-09-02", source)
	if err != nil {
		t.Fatal(err)
	}
	entry := entries["large"]
	if source.calls != 1 || entry.Revision != source.pin.Revision || len(entry.Files) != 2 || entry.Files[1].Name != "model.safetensors" {
		t.Fatalf("calls=%d entry=%#v", source.calls, entry)
	}
}

func TestResolveLayoutsRefusesIncompleteSnapshotPin(t *testing.T) {
	source := &snapshotSource{pin: upstream.SnapshotPin{
		Revision: strings.Repeat("a", 40),
		Files:    []upstream.SnapshotFilePin{{Name: "config.json", SHA256: strings.Repeat("b", 64)}},
	}}
	document := manifest.Document{Layouts: map[string]manifest.Layout{
		"large": {Model: manifest.Model{Repo: "owner/model", Files: []string{"config.json", "model.safetensors"}}},
	}}
	_, err := pinning.ResolveLayouts(context.Background(), document, []string{"large"}, "2026-09-02", source)
	if err == nil || !strings.Contains(err.Error(), "incomplete model snapshot pin") {
		t.Fatalf("ResolveLayouts() error = %v", err)
	}
}
