package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
)

const providerID = "temper"

type Result struct {
	Harnesses []string `json:"harnesses"`
	Path      string   `json:"path"`
	Provider  string   `json:"provider"`
	Status    string   `json:"status"`
	Error     string   `json:"error,omitempty"`
}

type change struct {
	result        Result
	before, after []byte
	info          os.FileInfo
}

type Plan struct{ changes []change }

func (p Plan) Results() []Result {
	out := make([]Result, len(p.changes))
	for i, c := range p.changes {
		out[i] = c.result
		out[i].Harnesses = slices.Clone(c.result.Harnesses)
	}
	return out
}

// Prepare validates every merge before the caller changes the service.
func Prepare(root string, selections []Selection, listen string, models []Model) (Plan, error) {
	if err := Validate(selections); err != nil {
		return Plan{}, err
	}
	if len(selections) == 0 {
		return Plan{}, nil
	}
	if !filepath.IsAbs(root) {
		return Plan{}, errors.New("harness provider requires an absolute Temper root")
	}
	base, err := endpoint(listen)
	if err != nil {
		return Plan{}, err
	}
	grouped := map[string][]string{}
	for _, s := range selections {
		path, err := canonical(s.ModelsFile)
		if err != nil {
			return Plan{}, err
		}
		if path != s.ModelsFile {
			return Plan{}, fmt.Errorf("harness destination %s changed; review its resolved path %s in configure", s.ModelsFile, path)
		}
		grouped[path] = append(grouped[path], s.ID)
	}
	var paths []string
	for path := range grouped {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	p := Plan{}
	for _, path := range paths {
		isHermes := slices.Contains(grouped[path], "hermes")
		if isHermes && len(grouped[path]) != 1 {
			return Plan{}, fmt.Errorf("%s cannot be both Hermes YAML and Pi/PiG JSON", path)
		}
		old, info, err := read(path)
		if err != nil {
			return Plan{}, err
		}
		mergeConfig := merge
		if isHermes {
			mergeConfig = mergeHermes
		}
		next, err := mergeConfig(old, root, base, models)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", path, err)
		}
		status := "pending"
		if bytes.Equal(old, next) {
			status = "unchanged"
		}
		p.changes = append(p.changes, change{Result{Harnesses: grouped[path], Path: path, Provider: providerID, Status: status}, old, next, info})
	}
	return p, nil
}

func read(path string) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 || multiplyLinked(info) {
		return nil, nil, fmt.Errorf("%s must be a regular, singly linked file of at most 16 MiB", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(info, actual) {
		return nil, nil, fmt.Errorf("%s changed while reading", path)
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > 16<<20 {
		return nil, nil, fmt.Errorf("%s exceeds 16 MiB", path)
	}
	return raw, info, nil
}

// Publish commits files independently. Its results remain useful on partial
// failure; the next ordinary Prepare/Publish recomputes remaining work.
func (p Plan) Publish(ctx context.Context) ([]Result, error) {
	out := p.Results()
	var failures []error
	for i, c := range p.changes {
		err := c.publish(ctx)
		if err != nil {
			out[i].Status, out[i].Error = "failed", err.Error()
			failures = append(failures, fmt.Errorf("%s: %w", c.result.Path, err))
		} else if c.result.Status != "unchanged" {
			out[i].Status = "updated"
		}
	}
	return out, errors.Join(failures...)
}

func (c change) check() error {
	path, err := canonical(c.result.Path)
	if err != nil {
		return err
	}
	if path != c.result.Path {
		return errors.New("destination changed after review")
	}
	raw, info, err := read(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, c.before) || (info == nil) != (c.info == nil) || info != nil && (!os.SameFile(info, c.info) || info.Mode() != c.info.Mode() || !info.ModTime().Equal(c.info.ModTime())) {
		return errors.New("harness configuration changed after preflight; review and repeat activation")
	}
	return nil
}

func (c change) publish(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.check(); err != nil {
		return err
	}
	if c.result.Status == "unchanged" {
		return nil
	}
	dir := filepath.Dir(c.result.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	unlock, err := lockDirectory(dir)
	if err != nil {
		return err
	}
	defer unlock()
	if err = c.check(); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".temper-models-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	mode := os.FileMode(0600)
	if c.info != nil {
		mode = c.info.Mode().Perm()
	}
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(c.after)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = c.check(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), c.result.Path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("models saved but opening directory failed: %w", err)
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return fmt.Errorf("models saved but directory sync failed: %w", err)
	}
	return nil
}

func object(raw []byte) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("expected a JSON object")
	}
	return value, nil
}

func merge(raw []byte, root, base string, models []Model) ([]byte, error) {
	doc := map[string]json.RawMessage{}
	if raw != nil {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := uniqueJSON(d, 0); err != nil {
			return nil, err
		}
		if _, err := d.Token(); err != io.EOF {
			return nil, errors.New("expected exactly one JSON object")
		}
		var err error
		doc, err = object(raw)
		if err != nil {
			return nil, err
		}
	}
	providers := map[string]json.RawMessage{}
	if v, ok := doc["providers"]; ok {
		var err error
		providers, err = object(v)
		if err != nil {
			return nil, fmt.Errorf("providers: %w", err)
		}
	}
	id, name := providerID, "Temper ("+filepath.Clean(root)+")"
	provider := map[string]json.RawMessage{}
	if v, ok := providers[id]; ok {
		var err error
		provider, err = object(v)
		if err != nil {
			return nil, err
		}
		var owner string
		_ = json.Unmarshal(provider["name"], &owner)
		if owner != name {
			return nil, fmt.Errorf("provider %s is not owned by this Temper root; resolve the name collision explicitly", id)
		}
	}
	put := func(key string, v any) { provider[key], _ = json.Marshal(v) }
	put("name", name)
	put("baseUrl", base)
	put("api", "openai-completions")
	if _, ok := provider["apiKey"]; !ok {
		put("apiKey", "temper")
	}
	// Retain unknown model fields for surviving IDs, replacing the owned fields.
	previous := map[string]map[string]json.RawMessage{}
	if v, ok := provider["models"]; ok {
		var entries []json.RawMessage
		if err := json.Unmarshal(v, &entries); err != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, errors.New("owned provider models must be an array")
		}
		for _, entry := range entries {
			m, err := object(entry)
			if err != nil {
				return nil, err
			}
			var id string
			if err = json.Unmarshal(m["id"], &id); err != nil || id == "" {
				return nil, errors.New("owned model requires an id")
			}
			if _, ok := previous[id]; ok {
				return nil, fmt.Errorf("duplicate owned model %q", id)
			}
			previous[id] = m
		}
	}
	next := []map[string]json.RawMessage{}
	for _, m := range models {
		v := previous[m.ID]
		if v == nil {
			v = map[string]json.RawMessage{}
		}
		b, _ := json.Marshal(m)
		fields, _ := object(b)
		// Transport belongs to this provider. A surviving per-model override
		// must not keep pointing to the listener of a previous activation.
		delete(v, "api")
		delete(v, "baseUrl")
		// Only the token defaults and these compatibility switches are ours.
		// Preserve user-added settings within the same objects as well.
		for _, key := range []string{"compat", "samplingParams", "cost", "thinkingLevelMap"} {
			old := map[string]json.RawMessage{}
			if raw, ok := v[key]; ok {
				var err error
				old, err = object(raw)
				if err != nil {
					return nil, fmt.Errorf("model %s %s: %w", m.ID, key, err)
				}
			}
			if key == "compat" {
				for _, name := range []string{"thinkingFormat", "chatTemplateKwargs"} {
					delete(old, name)
				}
			}
			if key == "thinkingLevelMap" {
				delete(old, "off")
			}
			if key == "samplingParams" {
				delete(old, "max_tokens")
				delete(old, "max_completion_tokens")
			}
			if raw, ok := fields[key]; ok {
				owned, _ := object(raw)
				for k, val := range owned {
					old[k] = val
				}
			}
			delete(v, key)
			delete(fields, key)
			if len(old) > 0 {
				fields[key], _ = json.Marshal(old)
			}
		}
		for k, value := range fields {
			v[k] = value
		}
		next = append(next, v)
	}
	put("models", next)
	providers[id], _ = json.Marshal(provider)
	doc["providers"], _ = json.Marshal(providers)
	result, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	result = append(result, '\n')
	if raw != nil && equalJSON(raw, result) {
		return raw, nil
	}
	return result, nil
}

func equalJSON(a, b []byte) bool {
	decode := func(raw []byte) any {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var v any
		_ = d.Decode(&v)
		return v
	}
	return reflect.DeepEqual(decode(a), decode(b))
}

// Duplicate keys could hide an ownership marker or discard unrelated data.
func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 128 {
		return errors.New("JSON nesting exceeds 128")
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	switch t {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return fmt.Errorf("duplicate or invalid JSON key %q", k)
			}
			seen[key] = true
			if err = uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	case json.Delim('['):
		for d.More() {
			if err = uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	return nil
}
