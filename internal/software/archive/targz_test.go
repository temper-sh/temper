package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTarGzInspectExtractAndScanShareCanonicalInventory(t *testing.T) {
	data := tarGzFixture(t, []archiveTestEntry{
		{name: "python/bin/python3", body: "runtime", mode: 0o755, kind: tar.TypeReg},
		{name: "python/bin/python", link: "python3", kind: tar.TypeSymlink},
		{name: "python/lib/config", body: "settings", mode: 0o644, kind: tar.TypeReg},
	})
	archivePath := filepath.Join(t.TempDir(), "runtime.tar.gz")
	if err := os.WriteFile(archivePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	spec := TarGzSpec{
		Root: "python", MaxEntries: 5, ExactEntries: 5,
		MaxUnpackedBytes: 15, ExactUnpackedBytes: 15, Label: "test archive",
	}
	want, err := InspectTarGz(context.Background(), archivePath, spec)
	if err != nil {
		t.Fatalf("InspectTarGz() error = %v", err)
	}
	destination := filepath.Join(t.TempDir(), "environment")
	if err := ExtractTarGz(context.Background(), archivePath, destination, spec, want); err != nil {
		t.Fatalf("ExtractTarGz() error = %v", err)
	}
	got, err := ScanTree(context.Background(), destination, "test tree")
	if err != nil {
		t.Fatalf("ScanTree() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("installed inventory = %#v, want %#v", got, want)
	}
}

func TestTarGzInspectionRefusesUnsafeOrOutOfBoundsPayloads(t *testing.T) {
	tests := []struct {
		name    string
		entries []archiveTestEntry
		spec    TarGzSpec
		want    string
	}{
		{
			name: "traversal", entries: []archiveTestEntry{{name: "../escape", body: "x", kind: tar.TypeReg}},
			spec: TarGzSpec{Root: "python", MaxEntries: 10, MaxUnpackedBytes: 10}, want: "unsafe",
		},
		{
			name: "hard link", entries: []archiveTestEntry{{name: "python/value", body: "x", kind: tar.TypeReg}, {name: "python/hard", link: "python/value", kind: tar.TypeLink}},
			spec: TarGzSpec{Root: "python", MaxEntries: 10, MaxUnpackedBytes: 10}, want: "unsupported tar type",
		},
		{
			name: "dangling symlink", entries: []archiveTestEntry{{name: "python/link", link: "missing", kind: tar.TypeSymlink}},
			spec: TarGzSpec{Root: "python", MaxEntries: 10, MaxUnpackedBytes: 10}, want: "targets missing path",
		},
		{
			name: "entry bound", entries: []archiveTestEntry{{name: "python/a", body: "a", kind: tar.TypeReg}, {name: "python/b", body: "b", kind: tar.TypeReg}},
			spec: TarGzSpec{Root: "python", MaxEntries: 1, MaxUnpackedBytes: 10}, want: "entry limit",
		},
		{
			name: "exact bytes", entries: []archiveTestEntry{{name: "python/value", body: "abc", kind: tar.TypeReg}},
			spec: TarGzSpec{Root: "python", MaxEntries: 2, MaxUnpackedBytes: 4, ExactUnpackedBytes: 4}, want: "unpacked size is 3, want 4",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := tarGzFixture(t, test.entries)
			archivePath := filepath.Join(t.TempDir(), "payload.tar.gz")
			if err := os.WriteFile(archivePath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := InspectTarGz(context.Background(), archivePath, test.spec)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("InspectTarGz() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestTarGzExtractionRequiresAnEmptyRealDestination(t *testing.T) {
	data := tarGzFixture(t, []archiveTestEntry{{name: "python/value", body: "x", kind: tar.TypeReg}})
	archivePath := filepath.Join(t.TempDir(), "payload.tar.gz")
	if err := os.WriteFile(archivePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	spec := TarGzSpec{Root: "python", MaxEntries: 1, MaxUnpackedBytes: 1}
	entries, err := InspectTarGz(context.Background(), archivePath, spec)
	if err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(destination, "existing"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ExtractTarGz(context.Background(), archivePath, destination, spec, entries); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("ExtractTarGz() error = %v, want non-empty destination refusal", err)
	}
}

type archiveTestEntry struct {
	name string
	body string
	link string
	mode int64
	kind byte
}

func tarGzFixture(t *testing.T, entries []archiveTestEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	compressed := gzip.NewWriter(&output)
	writer := tar.NewWriter(compressed)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{Name: entry.name, Mode: mode, Typeflag: kind, Linkname: entry.link}
		if kind == tar.TypeReg || kind == tar.TypeRegA {
			header.Size = int64(len(entry.body))
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size != 0 {
			if _, err := io.WriteString(writer, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
