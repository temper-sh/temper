package presetcmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/huggingface"
	templatepatch "github.com/temper-sh/temper/internal/patch"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/userpreset"
)

// Hub reads exact file facts from Hugging Face for weights and template
// records, so nobody has to type a size or a SHA-256.
type Hub interface {
	Revision(ctx context.Context, repo, revision string) (huggingface.Repository, error)
	Files(ctx context.Context, repo, commit string, paths []string) ([]huggingface.FileInfo, error)
	Open(ctx context.Context, repo, revision, file string) (io.ReadCloser, error)
}

const (
	// A small file has no SHA-256 in Hugging Face's metadata, so Temper
	// downloads and hashes it; weights files are always large files.
	maxHashedFile = 64 << 20
	// Preparation reads at most this much of a template.
	maxTemplate = 16 << 20
)

var invalidIDRun = regexp.MustCompile(`[^a-z0-9]+`)

// recordID derives a record ID from a file name: Qwen3.8-27B-Q6_K.gguf → qwen3.8-27b-q6-k.
func recordID(file string) string {
	name := strings.ToLower(strings.TrimSuffix(path.Base(file), path.Ext(file)))
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '.' })
	for i, part := range parts {
		parts[i] = strings.Trim(invalidIDRun.ReplaceAllString(part, "-"), "-")
	}
	return strings.Trim(strings.Join(slices.DeleteFunc(parts, func(p string) bool { return p == "" }), "."), ".-")
}

// splitRepository reads REPO[@REVISION]; the default revision is main.
func splitRepository(value string) (string, string) {
	repo, revision, found := strings.Cut(value, "@")
	if !found || revision == "" {
		revision = "main"
	}
	return repo, revision
}

// material reads exact records for files at one revision: the commit, the
// card's license and each file's size and SHA-256. Patterns such as
// *.safetensors match the files at that commit.
func (c Command) material(ctx context.Context, repo, revision string, patterns []string) (huggingface.Repository, []catalog.File, error) {
	if c.Hub == nil {
		return huggingface.Repository{}, nil, errors.New("Hugging Face is unavailable")
	}
	r, err := c.Hub.Revision(ctx, repo, revision)
	if err != nil {
		return r, nil, err
	}
	var paths []string
	for _, pattern := range patterns {
		if !strings.ContainsAny(pattern, "*?[") {
			paths = append(paths, pattern)
			continue
		}
		before := len(paths)
		for _, file := range r.Files {
			if ok, _ := path.Match(pattern, file); ok && !slices.Contains(paths, file) {
				paths = append(paths, file)
			}
		}
		if len(paths) == before {
			return r, nil, fmt.Errorf("no file in %s matches %q", repo, pattern)
		}
	}
	infos, err := c.Hub.Files(ctx, repo, r.Commit, paths)
	if err != nil {
		return r, nil, err
	}
	files := make([]catalog.File, 0, len(infos))
	for _, info := range infos {
		file := catalog.File{Path: info.Path, Bytes: info.Bytes, SHA256: info.SHA256}
		if file.SHA256 == "" {
			data, err := c.download(ctx, repo, r.Commit, info.Path, maxHashedFile)
			if err != nil {
				return r, nil, err
			}
			sum := sha256.Sum256(data)
			file.Bytes, file.SHA256 = int64(len(data)), hex.EncodeToString(sum[:])
		}
		files = append(files, file)
	}
	return r, files, nil
}

func (c Command) download(ctx context.Context, repo, commit, file string, limit int64) ([]byte, error) {
	reader, err := c.Hub.Open(ctx, repo, commit, file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s/%s: %w", repo, file, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s/%s is larger than %d MiB and has no SHA-256 from Hugging Face", repo, file, limit>>20)
	}
	return data, nil
}

// edit reads one of your presets, lets change modify it, checks that the
// result still compiles and replaces the file only then.
func (c Command) edit(root, id string, change func(*catalog.UserPreset, catalog.View) error) (catalog.UserPreset, error) {
	view, err := c.view(root)
	if err != nil {
		return catalog.UserPreset{}, err
	}
	raw, err := userpreset.Read(root, id)
	if err != nil {
		return catalog.UserPreset{}, fmt.Errorf("your preset %s: %w", id, err)
	}
	u, err := catalog.ParseUserPreset(id, raw)
	if err != nil {
		return u, fmt.Errorf("your preset %s: %w", id, err)
	}
	if err := change(&u, view); err != nil {
		return u, err
	}
	next, err := catalog.MarshalUserPreset(u)
	if err != nil {
		return u, err
	}
	loaded, err := userpreset.Resolve(root, id, next, view)
	if err != nil {
		return u, err
	}
	if _, err := catalog.CompilePreset(loaded.Document, id, 0, 0, target); err != nil {
		return u, err
	}
	return u, userpreset.Replace(root, id, raw, next)
}

// setChange sets one value in a fork's changes.
func setChange(u *catalog.UserPreset, key string, value any) {
	if u.Changes == nil {
		u.Changes = map[string]any{}
	}
	u.Changes[key] = value
}

func (c Command) weights(ctx context.Context, id, repository string, patterns []string, o options, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	repo, revision := splitRepository(repository)
	r, files, err := c.material(ctx, repo, revision, patterns)
	if err != nil {
		return err
	}
	format := o.format
	if format == "" {
		if len(files) != 1 || !strings.HasSuffix(files[0].Path, ".gguf") {
			return errors.New("say which format these files are with --format gguf, mlx-safetensors or safetensors")
		}
		format = "gguf"
	}
	license := o.license
	if license == "" {
		if license = r.License; license == "" {
			return fmt.Errorf("%s declares no license; give it with --license", repo)
		}
	}
	wid := o.recordID
	if wid == "" {
		wid = recordID(files[0].Path)
	}
	if !catalog.ValidPresetID(wid) {
		return fmt.Errorf("weights ID %q is invalid; give one with --weights-id", wid)
	}
	record := catalog.UserArtifact{Artifact: catalog.Artifact{Format: format, Files: files, License: license,
		Sources: []catalog.Source{{Hub: catalog.HubHuggingFace, Repo: repo, Revision: r.Commit}}}, SameModelAs: o.sameModelAs}
	u, err := c.edit(o.root, id, func(u *catalog.UserPreset, view catalog.View) error {
		if u.Artifacts == nil {
			u.Artifacts = map[string]catalog.UserArtifact{}
		}
		if u.Fork() {
			origin, ok := view.Document.Presets[u.Origin]
			if !ok {
				return fmt.Errorf("origin %q is not in this catalog", u.Origin)
			}
			own := view.Document.Artifacts[origin.Artifact]
			if record.SameModelAs == "" && own.HuggingFace().Repo == repo {
				record.SameModelAs = origin.Artifact
			}
			if record.SameModelAs == "" {
				return fmt.Errorf("a fork keeps its model: if these weights are another build of %s, say --same-model-as %s; for another model, make a copy", origin.Artifact, origin.Artifact)
			}
			setChange(u, "artifact", wid)
		} else {
			u.Preset.Artifact = wid
			if t, ok := u.Templates[u.Preset.Template]; ok && !slices.Contains(t.CompatibleArtifacts, wid) {
				t.CompatibleArtifacts = append(t.CompatibleArtifacts, wid)
				u.Templates[u.Preset.Template] = t
			}
		}
		u.Artifacts[wid] = record
		return nil
	})
	if err != nil {
		return err
	}
	var total int64
	for _, f := range files {
		total += f.Bytes
	}
	if o.json {
		return encode(out, map[string]any{"schema": "temper-preset-write/v1", "preset": id, "form": form(u), "weights": wid, "repo": repo, "commit": r.Commit, "bytes": total, "path": userpreset.Path(o.root, id)})
	}
	fmt.Fprintf(out, "%s now uses weights %s: %d file(s), %s, from %s at %s.\n", id, wid, len(files), setup.Size(total), repo, r.Commit[:7])
	return nil
}

func (c Command) template(ctx context.Context, values []string, o options, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	id := values[0]
	builtin := len(values) == 2 && values[1] == catalog.BuiltinTemplate
	local := o.local != ""
	if local && len(values) != 1 || !local && !builtin && len(values) != 3 {
		return errors.New("give REPO[@REVISION] FILE, --local FILE.jinja, or builtin")
	}
	tid := o.recordID
	if tid == "" {
		tid = id + "-template"
	}
	var record catalog.Template
	var commit string
	if local {
		if !templatepatch.ValidLocalFile(o.local) {
			return fmt.Errorf("%q must be a plain file name in %s, not a .json file", o.local, userpreset.Dir(o.root))
		}
		license := o.license
		if license == "" {
			license = "your own file"
		}
		// Temper reads the size and SHA-256 from the file at each compile.
		record = catalog.Template{Transform: o.transform, License: license,
			Sources: []catalog.Source{{Hub: catalog.HubLocal}}, Files: []catalog.File{{Path: o.local}}}
	} else if !builtin {
		if !catalog.ValidPresetID(tid) {
			return fmt.Errorf("template ID %q is invalid; give one with --template-id", tid)
		}
		repo, revision := splitRepository(values[1])
		r, err := c.Hub.Revision(ctx, repo, revision)
		if err != nil {
			return err
		}
		commit = r.Commit
		source, err := c.download(ctx, repo, r.Commit, values[2], maxTemplate)
		if err != nil {
			return err
		}
		// The record describes the template as prepared: after its transform.
		final, err := templatepatch.Apply(o.transform, source)
		if err != nil {
			return err
		}
		license := o.license
		if license == "" {
			if license = r.License; license == "" {
				return fmt.Errorf("%s declares no license; give it with --license", repo)
			}
		}
		sum := sha256.Sum256(final)
		record = catalog.Template{Transform: o.transform, License: license,
			Sources: []catalog.Source{{Hub: catalog.HubHuggingFace, Repo: repo, Revision: r.Commit}},
			Files:   []catalog.File{{Path: values[2], Bytes: int64(len(final)), SHA256: hex.EncodeToString(sum[:])}}}
	}
	u, err := c.edit(o.root, id, func(u *catalog.UserPreset, view catalog.View) error {
		chosen := catalog.BuiltinTemplate
		if !builtin {
			weights := ""
			if u.Fork() {
				origin, ok := view.Document.Presets[u.Origin]
				if !ok {
					return fmt.Errorf("origin %q is not in this catalog", u.Origin)
				}
				weights = origin.Artifact
				if a, ok := u.Changes["artifact"].(string); ok {
					weights = a
				}
			} else {
				weights = u.Preset.Artifact
			}
			record.CompatibleArtifacts = []string{weights}
			if u.Templates == nil {
				u.Templates = map[string]catalog.Template{}
			}
			u.Templates[tid] = record
			chosen = tid
		}
		if u.Fork() {
			setChange(u, "template", chosen)
		} else {
			u.Preset.Template = chosen
		}
		return nil
	})
	if err != nil {
		return err
	}
	if o.json {
		report := map[string]any{"schema": "temper-preset-write/v1", "preset": id, "form": form(u), "template": tid, "path": userpreset.Path(o.root, id)}
		switch {
		case builtin:
			report["template"] = catalog.BuiltinTemplate
		case local:
			report["file"] = filepath.Join(userpreset.Dir(o.root), o.local)
		default:
			report["commit"] = commit
		}
		return encode(out, report)
	}
	if builtin {
		fmt.Fprintf(out, "%s now uses the chat template inside its weights.\n", id)
		return nil
	}
	if local {
		fmt.Fprintf(out, "%s now uses template %s from %s. Each compile reads the file again and records its exact content in the lock.\n", id, tid, filepath.Join(userpreset.Dir(o.root), o.local))
		return nil
	}
	fmt.Fprintf(out, "%s now uses template %s: %s from %s at %s.\n", id, tid, values[2], values[1], commit[:7])
	return nil
}

func form(u catalog.UserPreset) string {
	if u.Fork() {
		return "fork"
	}
	return "copy"
}
