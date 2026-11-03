package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPublicationPreservesRetirementHistoryAndUsesTheWithdrawalDate(t *testing.T) {
	tool, seed, _ := testTool(t)
	root := filepath.Join(t.TempDir(), "catalog")
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	write := func(date string, retired map[string]any, remove bool) string {
		return source(t, func(tree map[string]any) {
			tree["date"] = date
			tree["retired_presets"] = retired
			if remove {
				delete(tree["presets"].(map[string]any), "qwen3.5-4b-q4km-off")
				p := tree["presentation"].(map[string]any)
				var order []any
				for _, id := range p["preset_order"].([]any) {
					if id != "qwen3.5-4b-q4km-off" {
						order = append(order, id)
					}
				}
				p["preset_order"] = order
			}
		})
	}
	call := func(src string, at time.Time) error {
		_, err := publish(context.Background(), src, root, false, bytes.NewReader(seed), tool, "https://example.test/catalog/v1/snapshots/", at)
		return err
	}
	if err := call(write("2026-09-13", nil, false), now); err != nil {
		t.Fatal(err)
	}
	retire := func(date string) map[string]any {
		return map[string]any{"qwen3.5-4b-q4km-off": map[string]any{"retired_on": date, "reason": "Replaced", "successor": "gemma-4-e2b-q4km-off"}}
	}
	if err := call(write("2026-10-01", retire("2026-10-01"), true), now); err != nil {
		t.Fatal(err)
	}
	channel := filepath.Join(root, "channels", "stable", "channel.yaml")
	before, _ := os.ReadFile(channel)
	for _, records := range []map[string]any{retire("2026-10-02")} {
		if err := call(write("2026-10-02", records, true), now.Add(24*time.Hour)); err == nil || !strings.Contains(err.Error(), "original retired_on") {
			t.Fatal("history rewrite passed", err)
		}
		after, _ := os.ReadFile(channel)
		if !bytes.Equal(before, after) {
			t.Fatal("refused publication changed the channel")
		}
	}
	// A later retirement keeps the earlier notice with its original date.
	records := retire("2026-10-01")
	records["newly-withdrawn"] = map[string]any{"retired_on": "2026-10-02", "reason": "Later retirement"}
	if err := call(write("2026-10-02", records, true), now.Add(24*time.Hour)); err != nil {
		t.Fatal("publisher refused accumulated notices", err)
	}
	// Removing an old notice is a reviewed cleanup; the old signed snapshots
	// retain it.
	delete(records, "qwen3.5-4b-q4km-off")
	if err := call(write("2026-10-03", records, true), now.Add(48*time.Hour)); err != nil {
		t.Fatal("publisher refused cleanup of an old notice", err)
	}
	// A new withdrawal cannot use yesterday's still-valid catalog date.
	root = filepath.Join(t.TempDir(), "catalog")
	if err := call(write("2026-09-13", nil, false), now); err != nil {
		t.Fatal(err)
	}
	if err := call(write("2026-10-01", retire("2026-10-01"), true), now.Add(24*time.Hour)); err == nil || !strings.Contains(err.Error(), "today") {
		t.Fatal("new withdrawal was backdated", err)
	}
}
