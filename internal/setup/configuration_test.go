package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
)

func composition(t *testing.T) setup.Configuration {
	t.Helper()
	d := catalogDocument(t)
	c := setup.EmptyConfiguration()
	c.Presets[compactLayout] = setup.Preset{Name: "Helper", Lock: selectedLock(t, d, compactLocal)}
	c.Layouts["writing"] = setup.Layout{Name: "Writing", Presets: []string{compactLayout}, Startup: []string{}, IdleSeconds: 60}
	c.Layouts["review"] = setup.Layout{Name: "Review", Presets: []string{compactLayout}, Startup: []string{compactLayout}, Default: compactLayout, IdleSeconds: 60}
	return c
}

func TestComposedAlternativesShareDiskWithoutImplyingConcurrentMemory(t *testing.T) {
	d := catalogDocument(t)
	c := setup.EmptyConfiguration()
	ids := []string{"qwen3.8-27b-q4xl-mtp", "gemma-4-31b-qat-ud-q4-k-xl-llama"}
	for _, id := range ids {
		lock, err := catalog.CompilePreset(d, id, "", 0, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			t.Fatal(err)
		}
		c.Presets[id] = setup.Preset{Name: id, Lock: lock}
	}
	c.Layouts["work"] = setup.Layout{Name: "Work", Presets: ids, IdleSeconds: 60}
	p, err := setup.BuildConfiguration(t.TempDir(), facts(32), 100<<30, c, setup.Material{})
	if err != nil || !p.CanPrepare || p.Layouts[0].AllFit {
		t.Fatal("alternatives charged as co-resident", p.Refusals, err)
	}
	var routers int
	for _, download := range p.Downloads {
		if strings.Contains(download.Name, "llama-swap") {
			routers++
		}
	}
	if routers != 1 {
		t.Fatalf("shared router counted %d times", routers)
	}
	l := c.Layouts["work"]
	l.Startup = ids
	c.Layouts["work"] = l
	p, err = setup.BuildConfiguration(t.TempDir(), facts(32), 100<<30, c, setup.Material{})
	if err != nil || len(p.Layouts[0].Refusals) == 0 {
		t.Fatal("oversized startup accepted", err)
	}
}

func TestConfigurationSharesPresetsAndSeparatesDefaultFromStartup(t *testing.T) {
	c := composition(t)
	l := c.Layouts["writing"]
	l.Default = compactLayout
	c.Layouts["writing"] = l
	raw, err := c.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := setup.ParseConfiguration(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Presets) != 1 || len(got.Layouts["writing"].Startup) != 0 || got.Layouts["writing"].Default != compactLayout {
		t.Fatal("default implied residency or shared files were copied")
	}
	delete(c.Presets, compactLayout)
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "layout") {
		t.Fatal("removing referenced preset did not identify affected layout")
	}
}

func TestConfigurationSaveIsAtomicRevisionCheckedAndRepeatable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	c := composition(t)
	_, _, err := setup.SaveConfiguration(context.Background(), root, c, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("dry save created root")
	}
	rev, changed, err := setup.SaveConfiguration(context.Background(), root, c, "", false)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	_, changed, err = setup.SaveConfiguration(context.Background(), root, c, rev, false)
	if err != nil || changed {
		t.Fatal("repeated save", changed, err)
	}
	// A terminated writer's staged bytes are not the published configuration.
	if err := os.WriteFile(filepath.Join(root, ".configuration-interrupted"), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	delete(c.Layouts, "writing")
	_, _, err = setup.SaveConfiguration(context.Background(), root, c, "stale", false)
	if err == nil {
		t.Fatal("stale writer replaced user choices")
	}
	got, current, err := setup.ReadConfiguration(root)
	if err != nil || current != rev || len(got.Layouts) != 2 {
		t.Fatal("failed save damaged current state", err)
	}
	_, _, err = setup.SaveConfiguration(context.Background(), root, c, rev, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err = setup.ReadConfiguration(root)
	if err != nil || len(got.Presets) != 1 || len(got.Layouts) != 1 {
		t.Fatal("layout deletion removed shared preset", err)
	}
}

func TestConcurrentConfigurationEditsCannotLoseUpdates(t *testing.T) {
	root := t.TempDir()
	c := composition(t)
	rev, _, err := setup.SaveConfiguration(context.Background(), root, c, "", false)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"First", "Second"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy, _, err := setup.ReadConfiguration(root)
			if err != nil {
				results <- err
				return
			}
			l := copy.Layouts["writing"]
			l.Name = name
			copy.Layouts["writing"] = l
			<-start
			_, _, err = setup.SaveConfiguration(context.Background(), root, copy, rev, false)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful competing edits = %d", successes)
	}
}

func TestInvalidStartupAndRemovedDefaultAreVisible(t *testing.T) {
	for _, mutate := range []func(*setup.Layout){func(l *setup.Layout) { l.Startup = []string{"missing"} }, func(l *setup.Layout) { l.Presets = nil }, func(l *setup.Layout) { l.Startup = []string{compactLayout, compactLayout} }} {
		c := composition(t)
		l := c.Layouts["review"]
		mutate(&l)
		c.Layouts["review"] = l
		if err := c.Validate(); err == nil {
			t.Fatal("invalid layout accepted")
		}
	}
}
