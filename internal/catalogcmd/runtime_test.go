package catalogcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalogcmd"
	"github.com/temper-sh/temper/internal/render"
)

func TestExecutionServeRejectsWrongGenerationAndChangedBytesBeforeEffects(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("supervised serving currently requires macOS")
	}
	parent := t.TempDir()
	lock := filepath.Join(parent, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	raw, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	projections, err := locked.Projections()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "root")
	bundle, err := render.Build(render.Inputs{Manifest: projections.Manifest, Lock: projections.Artifacts, Mode: locked.Selection.Profile, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	base := []string{"serve", "--lock", lock, "--root", root, "--installation", "fixture", "--status-file", filepath.Join(parent, "status.json"), "--generation"}
	called := false
	dispatch := func(_ context.Context, args []string, _ io.Writer, _ io.Writer) int { called = true; return 0 }
	var out, diagnostics bytes.Buffer
	if code := catalogcmd.Runtime(context.Background(), append(base, strings.Repeat("a", 64)), &out, &diagnostics, dispatch); code == 0 || called {
		t.Fatal("wrong generation admitted")
	}
	for _, artifact := range bundle.Artifacts {
		path := filepath.Join(root, "rendered", "generations", bundle.Digest(), artifact.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, artifact.Data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := append(base, bundle.Digest())
	if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, dispatch); code != 0 || !called {
		t.Fatal("valid generation refused", &diagnostics)
	}
	called = false
	path := filepath.Join(root, "rendered", "generations", bundle.Digest(), bundle.Artifacts[0].Path)
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, dispatch); code == 0 || called {
		t.Fatal("changed generation admitted")
	}
}

func TestExecutionInspectionAndDryRunDoNotWriteOrInvokeEffects(t *testing.T) {
	parent := t.TempDir()
	lock := filepath.Join(parent, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	for _, args := range [][]string{
		{"inspect", "--lock", lock},
		{"prepare", "--lock", lock, "--root", filepath.Join(parent, "installation"), "--installation", "fixture", "--dry-run"},
	} {
		var out, diagnostics bytes.Buffer
		code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, func(context.Context, []string, io.Writer, io.Writer) int {
			t.Fatal("read-only operation invoked an effect")
			return 1
		})
		if code != 0 || !json.Valid(out.Bytes()) {
			t.Fatalf("exit %d: %s %s", code, &out, &diagnostics)
		}
		entries, _ := os.ReadDir(parent)
		if len(entries) != 1 {
			t.Fatal("read-only operation wrote files")
		}
	}
}

func TestExecutionPreparationOwnsLegacyInputsAndReturnsBoundMaterial(t *testing.T) {
	parent := t.TempDir()
	lock := filepath.Join(parent, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	var operations, projections []string
	dispatch := func(_ context.Context, args []string, out, diagnostics io.Writer) int {
		operation := args[0]
		if operation == "software" || operation == "field-kit" {
			operation += " " + args[1]
		}
		operations = append(operations, operation)
		for index, arg := range args {
			if arg == "--manifest" || arg == "--manifest-lock" || arg == "--lock" {
				path := args[index+1]
				if _, err := os.ReadFile(path); err != nil {
					t.Fatal(err)
				}
				projections = append(projections, path)
			}
		}
		switch operation {
		case "apply":
			fmt.Fprintln(out, "RESULT apply changed mode=fixture generation="+strings.Repeat("b", 64))
		case "field-kit bind":
			fmt.Fprintln(out, "schema: temper-field-kit-binding/v1")
		}
		return 0
	}
	var out, diagnostics bytes.Buffer
	args := []string{"prepare", "--lock", lock, "--root", filepath.Join(parent, "root"), "--installation", "fixture"}
	if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, dispatch); code != 0 {
		t.Fatalf("exit %d: %s", code, &diagnostics)
	}
	want := []string{"software install", "fetch", "software check", "apply", "check", "field-kit bind"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("%v", operations)
	}
	for _, path := range projections {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("temporary inputs survived")
		}
	}
	var material map[string]any
	if err := json.Unmarshal(out.Bytes(), &material); err != nil {
		t.Fatal(err)
	}
	if material["generation"] != strings.Repeat("b", 64) || material["binding"] != "schema: temper-field-kit-binding/v1\n" {
		t.Fatalf("%v", material)
	}
	// Rendering a candidate never repeats installation or fetch.
	operations = nil
	args[0] = "render"
	out.Reset()
	if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, dispatch); code != 0 {
		t.Fatal(diagnostics.String())
	}
	if !reflect.DeepEqual(operations, want[2:]) {
		t.Fatalf("render effects: %v", operations)
	}
}

func TestExecutionFailureStopsSubsequentEffectsAndRemovesTemporaryInputs(t *testing.T) {
	parent := t.TempDir()
	lock := filepath.Join(parent, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	calls := 0
	temporary := ""
	dispatch := func(_ context.Context, args []string, out, diagnostics io.Writer) int {
		calls++
		for index, arg := range args {
			if arg == "--lock" {
				temporary = filepath.Dir(args[index+1])
			}
		}
		fmt.Fprintln(diagnostics, "injected install failure")
		return 7
	}
	var out, diagnostics bytes.Buffer
	code := catalogcmd.Runtime(context.Background(), []string{"prepare", "--lock", lock, "--root", filepath.Join(parent, "root"), "--installation", "fixture"}, &out, &diagnostics, dispatch)
	if code == 0 || calls != 1 || !strings.Contains(diagnostics.String(), "injected install failure") {
		t.Fatal(code, calls, &diagnostics)
	}
	if _, err := os.Lstat(temporary); !os.IsNotExist(err) {
		t.Fatal("temporary inputs survived failure")
	}
}
