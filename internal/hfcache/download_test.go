//go:build darwin || linux

package hfcache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func downloadEntry() Entry {
	return Entry{Repo: "example/model", Revision: strings.Repeat("a", 40), Name: "nested/model.gguf", SHA256: strings.Repeat("b", 64)}
}

func TestDownloadUsesOfficialHFWithExactScope(t *testing.T) {
	e := downloadEntry()
	cache := filepath.Join(t.TempDir(), "shared cache")
	for _, installed := range []string{"hf", "uv", "neither"} {
		t.Run(installed, func(t *testing.T) {
			path, args, err := downloadCommand(e, cache, func(name string) (string, error) {
				if name == installed {
					return "/tools/" + name, nil
				}
				return "", exec.ErrNotFound
			})
			if installed == "neither" {
				if err == nil || !strings.Contains(err.Error(), "uv tool install huggingface-hub") {
					t.Fatalf("missing downloader lacks recovery command: %v", err)
				}
				return
			}
			want := []string{"download", "--revision", e.Revision, "--cache-dir", cache, "--quiet", "--", e.Repo, e.Name}
			if installed == "uv" {
				want = append([]string{"tool", "run", "--no-config", "--from", "huggingface-hub", "hf"}, want...)
			}
			if err != nil || path != "/tools/"+installed || !reflect.DeepEqual(args, want) {
				t.Fatalf("command: %q %q %v", path, args, err)
			}
		})
	}
	for _, invalid := range []Entry{
		{Repo: e.Repo, Revision: "main", Name: e.Name, SHA256: e.SHA256},
		{Repo: e.Repo, Revision: e.Revision, Name: "../../escape", SHA256: e.SHA256},
	} {
		if _, _, err := downloadCommand(invalid, cache, func(string) (string, error) {
			t.Fatal("invalid input reached executable lookup")
			return "", nil
		}); err == nil {
			t.Fatal("invalid download scope accepted")
		}
	}
}

func TestEnsureDelegatesOnlyMissingFilesAndNeverCleansHFState(t *testing.T) {
	e := downloadEntry()
	c := Cache{Root: filepath.Join(t.TempDir(), "hub")}
	partial := filepath.Join(c.Root, "hf-owned.incomplete")
	wantErr := errors.New("connection interrupted")
	if _, err := c.Ensure(context.Background(), e, func(_ context.Context, got Entry, root string) error {
		if got != e || root != c.Root {
			t.Fatal("download scope changed")
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(partial, []byte("HF can resume this"), 0o644); err != nil {
			return err
		}
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("lost HF failure: %v", err)
	}
	if data, err := os.ReadFile(partial); err != nil || string(data) != "HF can resume this" {
		t.Fatalf("Temper removed HF retry state: %v", err)
	}
	if _, err := c.Ensure(context.Background(), e, func(context.Context, Entry, string) error { return nil }); err == nil {
		t.Fatal("accepted a successful process without the requested cache material")
	}
	result, err := c.Ensure(context.Background(), e, func(_ context.Context, got Entry, root string) error {
		path := filepath.Join(root, got.snapshot())
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("weights"), 0o644)
	})
	if err != nil || result.Size != 7 {
		t.Fatalf("downloaded candidate: %+v %v", result, err)
	}
	if _, err := c.Ensure(context.Background(), e, nil); err != nil {
		t.Fatalf("cache hit required a downloader: %v", err)
	}
	// HF effects may succeed just as a cancellation arrives; Temper must not
	// continue installation or clean the shared result on that account.
	e.Revision = strings.Repeat("c", 40)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := c.Ensure(ctx, e, func(context.Context, Entry, string) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestDownloadProcessDisablesTelemetryAndPreservesAuthentication(t *testing.T) {
	directory := t.TempDir()
	hf := filepath.Join(directory, "hf")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEMPER_TEST_ARGS\"\nprintf '%s\\n' \"$HF_HUB_DISABLE_TELEMETRY/$HF_HUB_DISABLE_UPDATE_CHECK/$HF_TOKEN\" >&2\nprintf 'ignored stdout\\n'\nexit 9\n"
	if err := os.WriteFile(hf, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(directory, "args")
	t.Setenv("PATH", directory)
	t.Setenv("TEMPER_TEST_ARGS", argsFile)
	t.Setenv("HF_HUB_DISABLE_TELEMETRY", "0")
	t.Setenv("HF_HUB_DISABLE_UPDATE_CHECK", "0")
	t.Setenv("HF_TOKEN", "fixture-token")
	var diagnostics bytes.Buffer
	err := Download(context.Background(), downloadEntry(), filepath.Join(directory, "hub"), &diagnostics)
	if err == nil || !strings.Contains(err.Error(), "exit status 9") || diagnostics.String() != "1/1/fixture-token\n" {
		t.Fatalf("process result: %v, diagnostics %q", err, diagnostics.String())
	}
	data, err := os.ReadFile(argsFile)
	if err != nil || strings.Contains(string(data), "fixture-token") || !strings.Contains(string(data), "--revision\n"+downloadEntry().Revision+"\n") {
		t.Fatalf("incorrect argv: %s %v", data, err)
	}
}

func TestCanceledDownloadStopsChildProcess(t *testing.T) {
	directory := t.TempDir()
	ready, escaped := filepath.Join(directory, "ready"), filepath.Join(directory, "escaped")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-c", `(/bin/sleep 0.4; printf escaped > "$2") & printf ready > "$1"; wait`, "hf-fixture", ready, escaped)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	finished := make(chan error, 1)
	go func() { finished <- runDownload(command) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("download process did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("canceled download reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled download did not exit")
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(escaped); !os.IsNotExist(err) {
		t.Fatalf("download child survived cancellation: %v", err)
	}
}
