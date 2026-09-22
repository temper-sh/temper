package distribution

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsource"
)

func fixture(t *testing.T, sequence uint64) (Publication, publication.TrustRoot) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{19}, ed25519.SeedSize))
	trust, err := publication.NewTrustRoot(map[string]ed25519.PublicKey{"test-key": key.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../../catalog/qwen38-m5-refresh.json")
	if err != nil {
		t.Fatal(err)
	}
	// A prose-only catalog change gives each fixture a distinct identity without
	// changing the executable composition or claiming a new model measurement.
	var authored map[string]json.RawMessage
	if err := json.Unmarshal(data, &authored); err != nil {
		t.Fatal(err)
	}
	authored["date"] = json.RawMessage(fmt.Sprintf(`"2026-09-%02d"`, sequence))
	data, err = json.Marshal(authored)
	if err != nil {
		t.Fatal(err)
	}
	sha := digest(data)
	channel := []byte(fmt.Sprintf("schema: temper-catalog-channel/v1\nchannel: stable\ncatalog:\n  schema: temper-catalog/v2\n  sequence: %d\n  sha256: %s\n  locator: https://catalog.example/snapshots/%s/\n", sequence, sha, sha))
	return Publication{Channel: signed(channel, key), Catalog: signed(data, key)}, trust
}

func signed(data []byte, key ed25519.PrivateKey) publication.SignedArtifact {
	return publication.SignedArtifact{Data: data, Signature: []byte(fmt.Sprintf("schema: temper-signature/v1\nkey_id: test-key\nalgorithm: ed25519\nsignature: %s\n", base64.StdEncoding.EncodeToString(ed25519.Sign(key, data))))}
}

type fixtureSource struct {
	publication  Publication
	failure      error
	catalogReads int
}

func (s *fixtureSource) Channel(context.Context, string) (publication.SignedArtifact, error) {
	return s.publication.Channel, nil
}
func (s *fixtureSource) CatalogJSON(context.Context, string) (publication.SignedArtifact, error) {
	s.catalogReads++
	return s.publication.Catalog, s.failure
}

func TestUpdateRollbackAndOfflineReadPreserveIndependentUserFiles(t *testing.T) {
	ctx := context.Background()
	first, trust := fixture(t, 1)
	second, _ := fixture(t, 2)
	third, _ := fixture(t, 3)
	root := filepath.Join(t.TempDir(), "temper root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"selection.json", "execution.lock.json", "installed.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("user owns "+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	users := tree(t, root)
	source := &fixtureSource{publication: first}
	before := tree(t, root)
	if result, err := Update(ctx, root, true, trust, source); err != nil || !result.Changed || !result.DryRun {
		t.Fatalf("dry update: %+v %v", result, err)
	}
	if !reflect.DeepEqual(before, tree(t, root)) {
		t.Fatal("dry update mutated the root")
	}
	result, err := Update(ctx, root, false, trust, source)
	if err != nil || !result.Changed {
		t.Fatalf("update: %+v %v", result, err)
	}
	firstSHA := result.Active.SHA256
	before = tree(t, root)
	if replay, err := Update(ctx, root, false, trust, source); err != nil || replay.Changed {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if !reflect.DeepEqual(before, tree(t, root)) {
		t.Fatal("identical update rewrote catalog files")
	}
	source.publication = second
	result, err = Update(ctx, root, false, trust, source)
	if err != nil || result.PreviousSHA256 != firstSHA {
		t.Fatalf("second update: %+v %v", result, err)
	}
	secondSHA := result.Active.SHA256
	before = tree(t, root)
	if _, err := Rollback(ctx, root, firstSHA, true, trust); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, tree(t, root)) {
		t.Fatal("dry rollback mutated the root")
	}
	rolled, err := Rollback(ctx, root, firstSHA, false, trust)
	if err != nil || rolled.Active.SHA256 != firstSHA || rolled.Latest.SHA256 != secondSHA {
		t.Fatalf("rollback: %+v %v", rolled, err)
	}
	before = tree(t, root)
	if replay, err := Rollback(ctx, root, firstSHA, false, trust); err != nil || replay.Changed {
		t.Fatalf("rollback replay: %+v %v", replay, err)
	}
	if !reflect.DeepEqual(before, tree(t, root)) {
		t.Fatal("rollback replay wrote files")
	}
	view, err := Inspect(root, trust)
	if err != nil || view.Active.SHA256 != firstSHA || len(view.Snapshots) != 2 {
		t.Fatalf("offline inspect: %+v %v", view, err)
	}
	source.publication = first
	if _, err := Update(ctx, root, false, trust, source); err == nil || !strings.Contains(err.Error(), "highest accepted") {
		t.Fatalf("network rollback accepted after explicit rollback: %v", err)
	}
	source.publication = second
	if restored, err := Update(ctx, root, false, trust, source); err != nil || restored.Active.SHA256 != secondSHA {
		t.Fatalf("restore highest: %+v %v", restored, err)
	}
	source.publication = third
	if newer, err := Update(ctx, root, false, trust, source); err != nil || newer.Latest.Sequence != 3 {
		t.Fatalf("forward update: %+v %v", newer, err)
	}
	for name, original := range users {
		if !reflect.DeepEqual(tree(t, root)[name], original) {
			t.Fatalf("catalog operation changed user file %s", name)
		}
	}
}

func TestFailuresDoNotCreateOrReplaceActiveState(t *testing.T) {
	good, trust := fixture(t, 1)
	next, _ := fixture(t, 2)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{19}, ed25519.SeedSize))
	tests := []struct {
		name   string
		change func(*fixtureSource)
	}{
		{"download failure", func(s *fixtureSource) { s.failure = errors.New("download interrupted") }},
		{"bad channel signature", func(s *fixtureSource) { s.publication.Channel.Data = append(s.publication.Channel.Data, ' ') }},
		{"bad catalog signature", func(s *fixtureSource) { s.publication.Catalog.Data = append(s.publication.Catalog.Data, ' ') }},
		{"signed digest mismatch", func(s *fixtureSource) { s.publication.Catalog = signed(append(s.publication.Catalog.Data, ' '), key) }},
		{"unsupported schema", func(s *fixtureSource) {
			s.publication.Channel = signed(bytes.Replace(s.publication.Channel.Data, []byte("temper-catalog/v2"), []byte("temper-catalog/v99"), 1), key)
		}},
		{"wrong channel", func(s *fixtureSource) {
			s.publication.Channel = signed(bytes.Replace(s.publication.Channel.Data, []byte("channel: stable"), []byte("channel: other"), 1), key)
		}},
		{"insecure locator", func(s *fixtureSource) {
			s.publication.Channel = signed(bytes.Replace(s.publication.Channel.Data, []byte("https:"), []byte("http:"), 1), key)
		}},
		{"same sequence different digest", func(s *fixtureSource) {
			s.publication.Channel = signed(bytes.Replace(s.publication.Channel.Data, []byte("sequence: 2"), []byte("sequence: 1"), 1), key)
		}},
		{"unsupported engine capability", func(s *fixtureSource) {
			oldSHA := digest(s.publication.Catalog.Data)
			data := bytes.Replace(s.publication.Catalog.Data, []byte("llama-server/v2"), []byte("unsupported-engine/v9"), 1)
			s.publication.Catalog = signed(data, key)
			s.publication.Channel = signed(bytes.ReplaceAll(s.publication.Channel.Data, []byte(oldSHA), []byte(digest(data))), key)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			if _, err := Update(context.Background(), root, false, trust, &fixtureSource{publication: good}); err != nil {
				t.Fatal(err)
			}
			before := tree(t, root)
			source := &fixtureSource{publication: next}
			// Copy slices before mutation so subtests remain independent.
			source.publication.Channel = cloneArtifact(next.Channel)
			source.publication.Catalog = cloneArtifact(next.Catalog)
			tc.change(source)
			if _, err := Update(context.Background(), root, false, trust, source); err == nil {
				t.Fatal("invalid update accepted")
			}
			switch tc.name {
			case "bad channel signature", "unsupported schema", "wrong channel", "insecure locator", "same sequence different digest":
				if source.catalogReads != 0 {
					t.Fatal("untrusted or refused channel caused a catalog fetch")
				}
			}
			if !reflect.DeepEqual(before, tree(t, root)) {
				t.Fatal("failed update changed the usable catalog")
			}
			if _, err := Read(root, trust); err != nil {
				t.Fatal(err)
			}
		})
	}
	root := filepath.Join(t.TempDir(), "absent")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Update(ctx, root, false, trust, &fixtureSource{publication: good}); err == nil {
		t.Fatal("cancelled update succeeded")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("cancelled update created root")
	}
	if _, err := Update(context.Background(), root, true, trust, &fixtureSource{publication: good}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("dry update created root")
	}
}

func TestStoredTamperingAndSymlinksAreRefused(t *testing.T) {
	good, trust := fixture(t, 1)
	for _, name := range []string{"catalog.json", "catalog.signature.yaml", "channel.yaml", "channel.signature.yaml", "symlink", "state"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			result, err := Update(context.Background(), root, false, trust, &fixtureSource{publication: good})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "catalog", "snapshots", result.Active.SHA256, name)
			if name == "state" {
				path = filepath.Join(root, "catalog", "state.json")
			}
			if name == "symlink" {
				path = filepath.Join(root, "catalog", "snapshots", result.Active.SHA256, "catalog.json")
				if err := os.Rename(path, filepath.Join(root, "target")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "target"), path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Read(root, trust); err == nil {
				t.Fatal("unverified local catalog accepted")
			}
		})
	}
}

func TestInterruptedStagingIsReusableAndStaleWritersCannotCommit(t *testing.T) {
	good, trust := fixture(t, 1)
	next, _ := fixture(t, 2)
	root := filepath.Join(t.TempDir(), "root")
	if _, err := Update(context.Background(), root, false, trust, &fixtureSource{publication: good}); err != nil {
		t.Fatal(err)
	}
	observed, err := readStore(root, trust)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := Verify(next, trust)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce a crash after the immutable snapshot rename, before state commit.
	if err := observed.storeSnapshot(candidate); err != nil {
		t.Fatal(err)
	}
	staged := tree(t, filepath.Join(root, "catalog", "snapshots", candidate.SHA256))
	if active, err := Read(root, trust); err != nil || active.Sequence != 1 {
		t.Fatalf("staged snapshot became active: %+v %v", active.Identity, err)
	}
	if _, err := Update(context.Background(), root, false, trust, &fixtureSource{publication: next}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(staged, tree(t, filepath.Join(root, "catalog", "snapshots", candidate.SHA256))) {
		t.Fatal("retry rewrote the complete immutable stage")
	}
	if err := observed.commit(context.Background(), candidate, candidate); err == nil || !strings.Contains(err.Error(), "concurrently") {
		t.Fatalf("stale observation committed: %v", err)
	}
	lock, err := lockStore(filepath.Join(root, "catalog", "write.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if other, err := lockStore(filepath.Join(root, "catalog", "write.lock")); err == nil {
		other.Close()
		t.Fatal("two writers acquired the same lock")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBoundedHTTPSUpdatesFromExactlyTheFourPublishedFiles(t *testing.T) {
	p, trust := fixture(t, 1)
	sha := digest(p.Catalog.Data)
	files := map[string][]byte{
		"https://catalog.example/channels/stable/channel.yaml":                 p.Channel.Data,
		"https://catalog.example/channels/stable/channel.signature.yaml":       p.Channel.Signature,
		"https://catalog.example/snapshots/" + sha + "/catalog.json":           p.Catalog.Data,
		"https://catalog.example/snapshots/" + sha + "/catalog.signature.yaml": p.Catalog.Signature,
	}
	reads := 0
	source, err := catalogsource.NewHTTPS(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		data, ok := files[r.URL.String()]
		if !ok || r.Method != "GET" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		reads++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data))}, nil
	})}, "https://catalog.example/channels/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Update(context.Background(), filepath.Join(t.TempDir(), "root"), false, trust, source); err != nil {
		t.Fatal(err)
	}
	if reads != 4 {
		t.Fatalf("read %d publications, want 4", reads)
	}
}

type fileState struct {
	Mode     fs.FileMode
	Modified int64
	Data     string
}

func tree(t *testing.T, root string) map[string]fileState {
	t.Helper()
	result := map[string]fileState{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		result[relative] = fileState{Mode: info.Mode(), Modified: info.ModTime().UnixNano(), Data: string(data)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
