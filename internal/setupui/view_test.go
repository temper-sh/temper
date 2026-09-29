package setupui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDownloadTableFitsAndPreservesFullFileNames(t *testing.T) {
	item := Download{File: "模型-Qwen-with-a-very-long-name-Q4_K_M-00001-of-00002.gguf", Size: "3.25 GiB", Status: "Cached in Hugging Face"}
	for width := 22; width <= 104; width++ {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			view := ansi.Strip(downloadTable([]Download{item}, width))
			var file, action strings.Builder
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("table exceeds %d columns: %s", width, view)
				}
				cells := strings.Split(line, "│")
				if len(cells) > 2 {
					file.WriteString(strings.TrimSpace(cells[1]))
					action.WriteString(strings.TrimSpace(cells[len(cells)-2]))
				}
			}
			if !strings.Contains(file.String(), item.File) || !strings.Contains(strings.ReplaceAll(action.String(), " ", ""), "CachedinHuggingFace") {
				t.Fatalf("table truncated file or status: %s", view)
			}
		})
	}
}

func TestDownloadsCardDoesNotRewrapTableBorders(t *testing.T) {
	items := []Download{
		{File: "Qwen3.5-4B-Q4_K_M.gguf", Size: "2.55 GiB", Status: "Download on Prepare"},
		{File: "local/llama-cpp b10964", Size: "10.63 MiB", Status: "May download"},
	}
	for _, width := range []int{26, 40, 76, 96, 108} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			block, _ := downloadsBlock(Section{Downloads: items}, width, true, false)
			frame := ansi.Strip(block)
			for _, line := range strings.Split(ansi.Strip(downloadTable(items, width-4)), "\n") {
				if !strings.Contains(frame, "│ "+line+" │") {
					t.Fatalf("table rewrapped inside its card:\n%s", frame)
				}
			}
		})
	}
}

func textPosition(t *testing.T, frame, label string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(frame), "\n") {
		if x := strings.Index(line, label); x >= 0 {
			return ansi.StringWidth(line[:x]), y
		}
	}
	t.Fatalf("label %q is not visible:\n%s", label, ansi.Strip(frame))
	return 0, 0
}

func assertFrameFits(t *testing.T, frame string, width, height int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) > height {
		t.Fatalf("frame is %d lines in a %d-line terminal:\n%s", len(lines), height, ansi.Strip(frame))
	}
	for _, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("line is %d cells in a %d-cell terminal: %q", got, width, ansi.Strip(line))
		}
	}
}
