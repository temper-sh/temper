package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/artifactset"
)

func TestCrossFilesystemCopyVerifiesIndependentBytes(t *testing.T) {
	for _, content := range []string{"weights", "spoiled"} {
		t.Run(content, func(t *testing.T) {
			directory := t.TempDir()
			source := filepath.Join(directory, "cached.gguf")
			if err := os.WriteFile(source, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte("weights"))
			stage := filepath.Join(directory, "stage")
			err := copyModel(context.Background(), artifactset.ModelFile{Path: source, Size: 7}, stage, "model/weights.gguf", hex.EncodeToString(hash[:]))
			if content != "weights" {
				if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
					t.Fatalf("copy accepted corrupt bytes: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			original, err := os.Stat(source)
			if err != nil {
				t.Fatal(err)
			}
			copied := filepath.Join(stage, "model", "weights.gguf")
			info, err := os.Lstat(copied)
			if err != nil || !info.Mode().IsRegular() || os.SameFile(original, info) {
				t.Fatalf("expected a durable independent copy: %v", err)
			}
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(copied); err != nil || string(data) != "weights" {
				t.Fatalf("cache deletion invalidated copy: %v", err)
			}
		})
	}
}
