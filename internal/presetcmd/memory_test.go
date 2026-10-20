package presetcmd_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/presetcmd"
	"github.com/temper-sh/temper/internal/userpreset"
)

func TestCheckPreservesMeasurementInForksAndCopies(t *testing.T) {
	for _, form := range []string{"fork", "copy"} {
		t.Run(form, func(t *testing.T) {
			c, root := command(t)
			view, err := c.Catalog(root)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := c.Facts(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			l, err := catalog.CompilePreset(view.Document, mtp, 32, 0, view.Document.Runtime.Router.Target)
			if err != nil {
				t.Fatal(err)
			}
			p := view.Document.Presets[mtp]
			p.Profiles[0].Memory = &catalog.MemoryMeasurement{
				ObservedOn: "2026-10-20", Source: "https://example.com/assessment", Workload: "Fixture workload",
				Counter: "engine-rss", PeakBytes: 4 << 30, Limitations: "Fixture observation; loaded memory not sampled.",
				Machine: catalog.MemoryMachine{Chip: facts.Chip, PhysicalMemoryBytes: facts.PhysicalMemoryBytes,
					MetalMemoryMiB: facts.MetalDeviceMemoryMiB, OS: "macOS 26.6"},
				Execution: l.MemoryExecution(),
			}
			view.Document.Presets[mtp] = p
			c.Catalog = func(string) (catalog.View, error) { return view, nil }
			if out, diagnostics, code := run(t, c, root, form, mtp, "measured"); code != 0 {
				t.Fatal(out, diagnostics)
			}
			out, diagnostics, code := run(t, c, root, "check", "measured")
			if code != 0 || !strings.Contains(out, "observed peak is within the budget") ||
				!strings.Contains(out, "Measured memory: 4.00 GiB peak; loaded not sampled") ||
				strings.Contains(out, "accounted estimate") {
				t.Fatal(out, diagnostics)
			}
			out, diagnostics, code = run(t, c, root, "check", "measured", "--json")
			if code != 0 || !strings.Contains(out, `"basis": "measured"`) || !strings.Contains(out, `"loaded_bytes": null`) ||
				strings.Contains(out, `"fits": true`) {
				t.Fatal(out, diagnostics)
			}
		})
	}
}

type geometryHub struct {
	presetcmd.Hub
	t      *testing.T
	cancel context.CancelFunc
}

func (h geometryHub) Open(ctx context.Context, repo, commit, file string) (io.ReadCloser, error) {
	h.t.Helper()
	if repo != "unsloth/Qwen3.8-27B-GGUF" || commit != q6Commit || file != "Qwen3.8-27B-Q6_K.gguf" {
		h.t.Fatal("geometry read moving or unrelated source", repo, commit, file)
	}
	if h.cancel != nil {
		h.cancel()
		return nil, ctx.Err()
	}
	var b bytes.Buffer
	put := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	str := func(v string) { put(uint64(len(v))); b.WriteString(v) }
	b.WriteString("GGUF")
	put(uint32(3))
	put(uint64(1))
	put(uint64(5))
	str("general.architecture")
	put(uint32(8))
	str("qwen3")
	for _, kv := range []struct {
		k string
		v uint32
	}{
		{"block_count", 32}, {"embedding_length", 4096}, {"attention.head_count", 32}, {"attention.head_count_kv", 8},
	} {
		str("qwen3." + kv.k)
		put(uint32(4))
		put(kv.v)
	}
	return io.NopCloser(&b), nil
}

func TestWeightsAcquiresGeometryFromPinnedHeaderAndCheckUsesIt(t *testing.T) {
	c, root := withHub(t)
	c.Hub = geometryHub{Hub: c.Hub, t: t}
	if _, d, code := run(t, c, root, "fork", mtp, "qwen-cache"); code != 0 {
		t.Fatal(d)
	}
	out, d, code := run(t, c, root, "weights", "qwen-cache", "unsloth/Qwen3.8-27B-GGUF", "Qwen3.8-27B-Q6_K.gguf")
	if code != 0 || strings.Contains(out, "unavailable") {
		t.Fatal(out, d)
	}
	record := read(t, root, "qwen-cache").Artifacts["qwen3.8-27b-q6-k"]
	if record.ContextMemory == nil || record.ContextMemory.Layers != 32 || record.ContextMemory.KVHeads != 8 {
		t.Fatal("artifact lost source geometry", record)
	}
	out, d, code = run(t, c, root, "check", "qwen-cache", "--json")
	if code != 0 || !strings.Contains(out, `"kv_cache_bytes": 4563402752`) || strings.Contains(out, `"fits": true`) {
		t.Fatal("check did not use acquired q8/64K cache estimate", out, d)
	}
}

func TestCancelledMetadataReadDoesNotCommitWeights(t *testing.T) {
	c, root := withHub(t)
	if _, d, code := run(t, c, root, "fork", mtp, "qwen-cache"); code != 0 {
		t.Fatal(d)
	}
	before, err := os.ReadFile(userpreset.Path(root, "qwen-cache"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Hub = geometryHub{Hub: c.Hub, t: t, cancel: cancel}
	var out, diagnostics bytes.Buffer
	if code := c.Run(ctx, []string{"weights", "qwen-cache", "unsloth/Qwen3.8-27B-GGUF", "Qwen3.8-27B-Q6_K.gguf", "--root", root}, &out, &diagnostics); code == 0 {
		t.Fatal("cancelled read committed")
	}
	after, err := os.ReadFile(userpreset.Path(root, "qwen-cache"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("cancel changed weights", err)
	}
}
