package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPublicationRejectsTamperedHistoryBeforeChangingTheChannel(t *testing.T) {
	tool, seed, _ := testTool(t)
	root := filepath.Join(t.TempDir(), "catalog")
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	first := source(t, func(tree map[string]any) { tree["date"] = "2026-09-13" })
	if _, err := publish(context.Background(), first, root, false, bytes.NewReader(seed), tool, "https://example.test/catalog/v1/snapshots/", now); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	path := filepath.Join(root, "snapshots", hex.EncodeToString(sum[:]), "catalog.json")
	if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	channel := filepath.Join(root, "channels", "stable", "channel.yaml")
	before, _ := os.ReadFile(channel)
	next := source(t, func(tree map[string]any) { tree["date"] = "2026-09-14" })
	if _, err = publish(context.Background(), next, root, false, bytes.NewReader(seed), tool, "https://example.test/catalog/v1/snapshots/", now); err == nil || !strings.Contains(err.Error(), "signed channel digest") {
		t.Fatal("trusted altered history", err)
	}
	after, _ := os.ReadFile(channel)
	if !bytes.Equal(before, after) {
		t.Fatal("refusal changed publication")
	}
}
