package setup_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/python"
	"github.com/temper-sh/temper/internal/software/installplan"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/receiptstore"
)

func studyCatalog(t *testing.T) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile("../testfixture/testdata/study-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	view, err := catalog.Read(raw, buildinfo.DevVersion)
	if err != nil || len(view.Unavailable) != 0 {
		t.Fatal(err, view.Unavailable)
	}
	return view.Document
}

func TestStudyPlansEveryNativeAndPythonArtifactWithoutWriting(t *testing.T) {
	d := studyCatalog(t)
	for id := range d.Presets {
		t.Run(id, func(t *testing.T) {
			lock := selectedLock(t, d, id)
			root := filepath.Join(t.TempDir(), "absent")
			plan, err := buildPlan(root, facts(64), 1<<40, []catalog.Lock{lock}, setup.Material{})
			if err != nil {
				t.Fatal(err)
			}
			sets, err := preset.Software(lock)
			if err != nil {
				t.Fatal(err)
			}
			var want, got int64
			var artifacts, downloads int
			for _, set := range sets {
				for _, unit := range set.Lock.Units {
					for _, artifact := range unit.Artifacts {
						want += artifact.Size
						artifacts++
					}
				}
			}
			for _, item := range plan.RemainingDownloads {
				if item.Kind == "software" {
					got += item.Bytes
					downloads++
				}
			}
			if got != want || downloads != artifacts {
				t.Fatalf("software bytes/files = %d/%d; want %d/%d", got, downloads, want, artifacts)
			}
			if plan.FreshDiskBytes <= plan.DownloadBytes || plan.RemainingDiskBytes != plan.FreshDiskBytes {
				t.Fatal("fresh installation omitted expanded/staging allowance")
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatalf("planning wrote root: %v", err)
			}
		})
	}
}

// Exercise the real provider, receipt, and planning boundary. The only doubles
// are artifact transport and environment installation; no Python runs.
func TestPlanCreditsOnlyVerifiedPythonEnvironmentAndLeavesItUntouched(t *testing.T) {
	ctx := context.Background()
	d := studyCatalog(t)
	supply := d.Engines["rapid-mlx"].Supply.Python
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	body := []byte("managed python fixture")
	if err := tw.WriteHeader(&tar.Header{Name: "python/bin/python3", Mode: 0755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	content := map[string][]byte{supply.Runtime.Artifact.Locator: archive.Bytes()}
	setBytes := func(a *software.Artifact, b []byte) {
		a.Size = int64(len(b))
		a.SHA256 = fmt.Sprintf("%x", sha256.Sum256(b))
		content[a.Locator] = b
	}
	setBytes(&supply.Runtime.Artifact, archive.Bytes())
	for i := range supply.Packages {
		setBytes(&supply.Packages[i].Artifact, []byte("wheel fixture"))
	}
	lock := selectedLock(t, d, "rapid-mlx")
	sets, err := preset.Software(lock)
	if err != nil {
		t.Fatal(err)
	}
	var selected preset.Installation
	for _, set := range sets {
		if set.Package == "rapid-mlx" {
			selected = set
		}
	}
	root := t.TempDir()
	installation := installplan.Installation{ID: selected.ID, Root: root}
	member, err := python.NewInstallationAdapter(memoryArtifacts(content), fixtureEnvironment{})
	if err != nil {
		t.Fatal(err)
	}
	family, err := adapter.NewInstallationFamily(member)
	if err != nil {
		t.Fatal(err)
	}
	before, err := family.Inspect(ctx, selected.Lock.Target, installation, selected.Lock.Units)
	if err != nil {
		t.Fatal(err)
	}
	models, err := family.EffectModels(selected.Lock)
	if err != nil {
		t.Fatal(err)
	}
	install, err := installplan.Build(selected.Lock, installation, models, before, installplan.State{})
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range install.Groups {
		if err := family.Install(ctx, selected.Lock.Target, installation, group, selected.Lock.Units); err != nil {
			t.Fatal(err)
		}
	}
	after, err := family.Inspect(ctx, selected.Lock.Target, installation, selected.Lock.Units)
	if err != nil {
		t.Fatal(err)
	}
	r, err := receipt.Build(selected.Lock, install, after, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := receiptstore.Read(root, selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Commit(ctx, r); err != nil {
		t.Fatal(err)
	}
	var beforeFiles []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		beforeFiles = append(beforeFiles, fmt.Sprint(path, info.Size(), info.ModTime().UnixNano()))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	material, err := setup.InspectModels(root, []catalog.Lock{lock})
	if err != nil {
		t.Fatal(err)
	}
	if err := material.InspectPresetSoftware(ctx, []catalog.Lock{lock}); err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(root, facts(64), 1<<40, []catalog.Lock{lock}, material)
	if err != nil {
		t.Fatal(err)
	}
	var reused int
	for _, item := range plan.Downloads {
		if item.Kind == "software" && strings.Contains(item.Name, "/rapid-mlx/") {
			if !item.Cached {
				t.Fatalf("verified environment not reused: %+v", item)
			}
			reused++
		}
	}
	if reused != len(supply.Packages)+1 {
		t.Fatalf("reused %d Python artifacts", reused)
	}
	if plan.RemainingDiskBytes >= plan.FreshDiskBytes {
		t.Fatal("environment received no disk credit")
	}
	var afterFiles []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		afterFiles = append(afterFiles, fmt.Sprint(path, info.Size(), info.ModTime().UnixNano()))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(beforeFiles, "\n") != strings.Join(afterFiles, "\n") {
		t.Fatal("read-only planning changed installed material")
	}
	for _, unit := range after.Units {
		if err := os.WriteFile(filepath.Join(unit.Location, "bin", "rapid-mlx"), []byte("drift"), 0755); err != nil {
			t.Fatal(err)
		}
		break
	}
	if installed, err := preset.Installed(ctx, root, selected); installed || err == nil {
		t.Fatal("drifted environment received reuse credit")
	}
}

type memoryArtifacts map[string][]byte

func (m memoryArtifacts) Open(_ context.Context, locator string) (io.ReadCloser, error) {
	b, ok := m[locator]
	if !ok {
		return nil, fmt.Errorf("unexpected download %s", locator)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

type fixtureEnvironment struct{}

func (fixtureEnvironment) Install(_ context.Context, r python.EnvironmentInstallRequest) error {
	return os.WriteFile(filepath.Join(r.EnvironmentPath, "bin", "rapid-mlx"), []byte("fixture"), 0755)
}
