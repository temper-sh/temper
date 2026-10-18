package datadir

import "testing"

func TestRootsGetSeparateStableEnginePortBlocks(t *testing.T) {
	home := "/Users/someone"
	if got := startPort("/Users/someone/.temper", home); got != DefaultStartPort {
		t.Fatalf("default root moved to %d", got)
	}
	run := startPort("/Users/someone/field-kit/runs/one/temper", home)
	preview := startPort("/Users/someone/field-kit/.local/preview", home)
	moved := startPort("/Users/someone/.temper", "/Users/other")
	for _, port := range []int{run, preview, moved} {
		// A root other than the default never shares the default block.
		if !ValidStartPort(port) || port == DefaultStartPort || port+PortBlock-1 >= 49152 {
			t.Fatalf("root block %d is outside the non-default engine range", port)
		}
	}
	if run == preview {
		t.Fatalf("two roots share block %d", run)
	}
	if again := startPort("/Users/someone/field-kit/runs/one/temper", home); again != run {
		t.Fatalf("one root moved from block %d to %d", run, again)
	}
	for _, port := range []int{0, 10000, 10002, 49137, 49169} {
		if ValidStartPort(port) {
			t.Fatalf("port %d accepted as a block start", port)
		}
	}
}
