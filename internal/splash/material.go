// Package splash prepares the immutable local assembly consumed by Splash.
// Model selection and downloads remain owned by the catalog and fetch.
package splash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
)

// MetadataProgram delegates GGUF interpretation and draft compatibility to the
// selected release. It reads only the supplied local files and emits metadata.
const MetadataProgram = `import base64,json,pathlib,sys
p=pathlib.Path(sys.executable).resolve().parents[2]
sys.path.insert(0,str(p))
from install import gguf,families,upstream
files=gguf.derived_files(pathlib.Path(sys.argv[1]))
f= families.family_for(json.loads(files['config.json']))
if f.draft.repo != sys.argv[3]: raise ValueError('catalog draft differs from Splash architecture selection: '+f.draft.repo)
c=json.loads(pathlib.Path(sys.argv[2]).read_bytes())
for k,v in f.draft.signature:
 if not upstream._same(upstream._config_value(c,k),v): raise ValueError('incompatible DFlash2 configuration: '+k)
print(json.dumps({k:base64.b64encode(v).decode('ascii') for k,v in files.items()}))
`

var metadataNames = []string{"config.json", "model.json", "tokenizer/chat_template.jinja", "tokenizer/config.json", "tokenizer/tokenizer.json", "tokenizer/tokenizer_config.json"}

type Material struct {
	root, path, key, state          string
	set                             artifactset.Set
	modelRepo, draftRepo, patchPath string
}

type record struct {
	Schema   string            `json:"schema"`
	Identity string            `json:"identity"`
	Files    map[string]string `json:"files"`
}

func Paths(root, layout, entryDigest, softwareSHA string) (assembly, state string) {
	sum := sha256.Sum256([]byte("temper-splash-assembly/v2\x00" + entryDigest + "\x00" + softwareSHA))
	return filepath.Join(root, "prepared", "splash", layout, hex.EncodeToString(sum[:])), filepath.Join(root, "runtime", "splash", softwareSHA)
}

func New(root, id string, layout manifest.Layout, entry lockfile.Entry, patches map[string]manifest.Patch) (Material, error) {
	if layout.Splash == nil || layout.Draft == nil || entry.Draft == nil {
		return Material{}, errors.New("Splash preparation requires exact target, draft and software")
	}
	set, err := artifactset.New(root, id, layout, entry, patches)
	if err != nil {
		return Material{}, err
	}
	path, state := Paths(root, id, entry.Digest(), layout.Splash.SoftwareSHA256)
	m := Material{root: root, path: path, key: filepath.Base(path), state: state, set: set, modelRepo: entry.Repo, draftRepo: entry.Draft.Repo}
	if layout.ChatTemplate != "" {
		m.patchPath = filepath.Join(set.Path(), "patches", layout.ChatTemplate, patches[layout.ChatTemplate].File)
	}
	return m, nil
}

func (m Material) links() map[string]string {
	return map[string]string{"target/model.gguf": m.set.ModelPath(), "draft/config.json": filepath.Join(m.set.Path(), "draft", "config.json"), "draft/model.safetensors": filepath.Join(m.set.Path(), "draft", "model.safetensors")}
}

// Verify hashes only the small derived files; hard-linked weights are admitted
// by the source artifact set. The caller can additionally audit that set's bytes.
func (m Material) Verify() error {
	if err := m.set.Verify(); err != nil {
		return err
	}
	if _, err := directories(m.root, m.path, false); err != nil {
		return err
	}
	data, err := readRegular(filepath.Join(m.path, "receipt.json"), 16<<10)
	if err != nil {
		return err
	}
	var r record
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	canonical, _ := json.MarshalIndent(r, "", "  ")
	if !bytes.Equal(data, append(canonical, '\n')) || r.Schema != "temper-splash-assembly/v2" || r.Identity != m.key || len(r.Files) != len(metadataNames) {
		return errors.New("Splash preparation receipt differs from selected inputs")
	}
	expected := map[string]bool{"receipt.json": true}
	for _, name := range metadataNames {
		data, err := readRegular(filepath.Join(m.path, filepath.FromSlash(name)), 64<<20)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if r.Files[name] != hex.EncodeToString(sum[:]) {
			return fmt.Errorf("Splash prepared metadata changed: %s", name)
		}
		expected[name] = true
	}
	for name, source := range m.links() {
		actual, err := os.Lstat(filepath.Join(m.path, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		original, err := os.Lstat(source)
		if err != nil {
			return err
		}
		if !actual.Mode().IsRegular() || !os.SameFile(actual, original) {
			return fmt.Errorf("Splash prepared weights differ from selected source: %s", name)
		}
		expected[name] = true
	}
	dirs := map[string]bool{".": true, "target": true, "draft": true, "tokenizer": true}
	return filepath.WalkDir(m.path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(m.path, path)
		if entry.IsDir() && dirs[rel] || entry.Type().IsRegular() && expected[filepath.ToSlash(rel)] {
			return nil
		}
		return fmt.Errorf("unexpected Splash assembly entry: %s", rel)
	})
}

// Derive reads metadata without loading a model or starting the native engine.
func Derive(ctx context.Context, python, target, draftConfig, draftRepo string) (map[string][]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-I", "-B", "-c", MetadataProgram, target, draftConfig, draftRepo)
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1"}
	var out, stderr limitedOutput
	out.limit, stderr.limit = 64<<20, 64<<10
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("derive Splash metadata: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var files map[string][]byte
	if err := json.Unmarshal(out.Bytes(), &files); err != nil {
		return nil, fmt.Errorf("decode Splash metadata: %w", err)
	}
	return files, nil
}

type limitedOutput struct {
	bytes.Buffer
	limit int
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("Splash metadata output exceeds limit")
	}
	return b.Buffer.Write(p)
}

type DeriveFunc func(context.Context, string, string, string, string) (map[string][]byte, error)

// Prepare publishes one complete assembly, or leaves the previous state intact.
// A present but changed assembly is refused, never repaired in place.
func (m Material) Prepare(ctx context.Context, python string, derive DeriveFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.Verify(); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, artifactset.ErrNotMaterialized) {
		return err
	}
	if _, err := os.Lstat(m.path); !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("Splash assembly exists but is incomplete: %s", m.path)
	}
	if err := m.set.Verify(); err != nil {
		return err
	}
	files, err := derive(ctx, python, m.set.ModelPath(), filepath.Join(m.set.Path(), "draft", "config.json"), m.draftRepo)
	if err != nil {
		return err
	}
	if len(files) != 4 {
		return errors.New("Splash returned an unexpected metadata set")
	}
	files["tokenizer/config.json"] = files["config.json"]
	// The native runtime distinguishes source assemblies from legacy packed
	// packages by this descriptor. Temper's receipt owns source provenance.
	descriptor, err := json.Marshal(struct {
		Version      int    `json:"version"`
		Model        string `json:"model"`
		TargetFormat string `json:"target_format"`
		VisionFormat string `json:"vision_format"`
	}{1, m.modelRepo, "gguf", "none"})
	if err != nil {
		return err
	}
	files["model.json"] = descriptor
	// Splash derived the Qwen pre-tokenizer from GGUF. Explicitly disable the
	// bundled Transformers loader's generic Mistral correction heuristic.
	var tokenizerConfig map[string]any
	if err := json.Unmarshal(files["tokenizer/tokenizer_config.json"], &tokenizerConfig); err != nil {
		return err
	}
	if tokenizerConfig == nil {
		return errors.New("Splash tokenizer configuration is not an object")
	}
	tokenizerConfig["fix_mistral_regex"] = false
	files["tokenizer/tokenizer_config.json"], err = json.Marshal(tokenizerConfig)
	if err != nil {
		return err
	}
	if m.patchPath != "" {
		patch, err := readRegular(m.patchPath, 4<<20)
		if err != nil {
			return err
		}
		files["tokenizer/chat_template.jinja"] = patch
	}
	for _, name := range metadataNames {
		if len(files[name]) == 0 {
			return fmt.Errorf("Splash omitted metadata %s", name)
		}
	}
	if len(files) != len(metadataNames) {
		return errors.New("Splash returned unexpected metadata paths")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parent := filepath.Dir(m.path)
	created, err := directories(m.root, parent, true)
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_ = os.Remove(created[i])
		}
	}()
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, dir := range []string{"target", "draft", "tokenizer"} {
		if err := os.Mkdir(filepath.Join(stage, dir), 0755); err != nil {
			return err
		}
	}
	r := record{Schema: "temper-splash-assembly/v2", Identity: m.key, Files: map[string]string{}}
	for _, name := range metadataNames {
		data := files[name]
		if err := os.WriteFile(filepath.Join(stage, filepath.FromSlash(name)), data, 0444); err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		r.Files[name] = hex.EncodeToString(sum[:])
	}
	links := m.links()
	names := make([]string, 0, len(links))
	for name := range links {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.Link(links[name], filepath.Join(stage, filepath.FromSlash(name))); err != nil {
			return fmt.Errorf("link Splash weights: %w", err)
		}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "receipt.json"), append(data, '\n'), 0444); err != nil {
		return err
	}
	candidate := m
	candidate.path = stage
	if err := candidate.Verify(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := publishDirectory(stage, m.path); err != nil {
		if verified := m.Verify(); verified == nil {
			return nil
		}
		return err
	}
	return m.Verify()
}

func readRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("expected bounded regular file: %s", path)
	}
	return os.ReadFile(path)
}

func directories(root, target string, create bool) ([]string, error) {
	var created []string
	rel, err := filepath.Rel(root, target)
	if err != nil || !filepath.IsAbs(root) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("Splash path escapes Temper root")
	}
	path := root
	parts := append([]string{""}, strings.Split(rel, string(filepath.Separator))...)
	for _, part := range parts {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if create && errors.Is(err, fs.ErrNotExist) {
			err = os.Mkdir(path, 0755)
			if err == nil {
				created = append(created, path)
			} else if !errors.Is(err, fs.ErrExist) {
				return created, err
			}
			info, err = os.Lstat(path)
		}
		if err != nil {
			return created, err
		}
		if !info.IsDir() {
			return created, fmt.Errorf("Splash directory is not a regular directory: %s", path)
		}
	}
	return created, nil
}

// PrepareState establishes writable cache/home roots immediately before serve.
// Refuse symlinked parents instead of redirecting native effects outside root.
func (m Material) PrepareState() error {
	for _, name := range []string{"home", "weights"} {
		if _, err := directories(m.root, filepath.Join(m.state, name), true); err != nil {
			return err
		}
	}
	return nil
}
