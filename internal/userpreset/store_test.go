package userpreset_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/temper-sh/temper/internal/userpreset"
)

func TestSaveDryRunCancellationConflictAndCleanRepeat(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	ctx := context.Background()
	original := []byte(`{"schema":"temper-preset/v1","origin":"original"}`)
	changed, err := userpreset.Save(ctx, root, "mine", nil, original, true)
	if err != nil || !changed {
		t.Fatalf("dry save: %v %v", changed, err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run made root: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := userpreset.Save(cancelled, root, "mine", nil, original, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancel made root: %v", err)
	}
	if _, err := userpreset.Save(ctx, root, "mine", nil, original, false); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(userpreset.Path(root, "mine"))
	if err != nil {
		t.Fatal(err)
	}
	changed, err = userpreset.Save(ctx, root, "mine", original, original, false)
	if err != nil || changed {
		t.Fatalf("unchanged save: %v %v", changed, err)
	}
	after, err := os.Stat(userpreset.Path(root, "mine"))
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || !os.SameFile(before, after) {
		t.Fatal("unchanged save rewrote file")
	}
	newer := []byte(`{"schema":"temper-preset/v1","origin":"newer"}`)
	if _, err := userpreset.Save(ctx, root, "mine", original, newer, false); err != nil {
		t.Fatal(err)
	}
	if _, err := userpreset.Save(ctx, root, "mine", original, []byte("lost update"), false); err == nil {
		t.Fatal("stale save succeeded")
	}
	if _, err := userpreset.Save(ctx, root, "mine", nil, original, false); err == nil {
		t.Fatal("creation overwrote existing file")
	}
	raw, err := userpreset.Read(root, "mine")
	if err != nil || string(raw) != string(newer) {
		t.Fatalf("winner was lost: %s %v", raw, err)
	}
}

func TestDeleteOnlyRemovesTheReviewedFile(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	original := []byte("broken but inspectable")
	if err := userpreset.Create(root, "mine", original); err != nil {
		t.Fatal(err)
	}
	template := filepath.Join(userpreset.Dir(root), "mine.jinja")
	if err := os.WriteFile(template, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := userpreset.Delete(ctx, root, "mine", original, true); err != nil {
		t.Fatal(err)
	}
	if _, err := userpreset.Read(root, "mine"); err != nil {
		t.Fatal("dry deletion removed file")
	}
	if err := userpreset.Replace(root, "mine", original, []byte("newer")); err != nil {
		t.Fatal(err)
	}
	if _, err := userpreset.Delete(ctx, root, "mine", original, false); err == nil {
		t.Fatal("deleted a concurrent edit")
	}
	changed, err := userpreset.Delete(ctx, root, "mine", []byte("newer"), false)
	if err != nil || !changed {
		t.Fatalf("delete: %v %v", changed, err)
	}
	changed, err = userpreset.Delete(ctx, root, "mine", []byte("newer"), false)
	if err != nil || changed {
		t.Fatalf("repeated delete: %v %v", changed, err)
	}
	if raw, err := os.ReadFile(template); err != nil || string(raw) != "keep" {
		t.Fatalf("deleted template: %s %v", raw, err)
	}
}

func TestConcurrentWritersCannotBothReplaceTheSameRevision(t *testing.T) {
	root := t.TempDir()
	original := []byte("original")
	if err := userpreset.Create(root, "mine", original); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var done sync.WaitGroup
	for _, candidate := range []string{"first", "second"} {
		done.Add(1)
		go func() {
			defer done.Done()
			<-start
			_, err := userpreset.Save(context.Background(), root, "mine", original, []byte(candidate), false)
			results <- err
		}()
	}
	close(start)
	done.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("got %d successful writers; want one", succeeded)
	}
	data, err := userpreset.Read(root, "mine")
	if err != nil || (string(data) != "first" && string(data) != "second") {
		t.Fatalf("incomplete publication: %s %v", data, err)
	}
}

func TestRefusedPublicationDoesNotFollowSymlinksOrLeaveStagedFiles(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(userpreset.Dir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, userpreset.Path(root, "mine")); err != nil {
		t.Fatal(err)
	}
	if _, err := userpreset.Save(context.Background(), root, "mine", []byte("keep"), []byte("replace"), false); err == nil {
		t.Fatal("replaced symlink")
	}
	if _, err := userpreset.Delete(context.Background(), root, "../outside", []byte("keep"), false); err == nil {
		t.Fatal("accepted path traversal")
	}
	raw, err := os.ReadFile(outside)
	if err != nil || string(raw) != "keep" {
		t.Fatalf("changed outside file: %s %v", raw, err)
	}
	staged, err := filepath.Glob(filepath.Join(userpreset.Dir(root), ".temper-preset-*"))
	if err != nil || len(staged) != 0 {
		t.Fatalf("staging residue: %v %v", staged, err)
	}
}
