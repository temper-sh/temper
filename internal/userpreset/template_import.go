package userpreset

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TemplateFile is a bounded snapshot of an explicitly selected file. Selection
// reads only; its managed name never replaces a different existing template.
type TemplateFile struct {
	Name, ManagedName string
	Content           []byte
}

var templateNameRun = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func ReadTemplateFile(path string) (TemplateFile, error) {
	// Inspect before opening so an ordinary chooser selection cannot wait for
	// a pipe writer. Recheck the opened file below, then bound the actual read.
	info, err := os.Stat(path)
	if err != nil {
		return TemplateFile{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return TemplateFile{}, fmt.Errorf("choose a regular template file of at most 16 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return TemplateFile{}, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return TemplateFile{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return TemplateFile{}, fmt.Errorf("choose a regular template file of at most 16 MiB")
	}
	content, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil {
		return TemplateFile{}, err
	}
	if len(content) == 0 || len(content) > 16<<20 {
		return TemplateFile{}, fmt.Errorf("choose a nonempty template file of at most 16 MiB")
	}
	name := filepath.Base(path)
	stem := strings.Trim(templateNameRun.ReplaceAllString(strings.TrimSuffix(name, filepath.Ext(name)), "-"), "-")
	if stem == "" {
		stem = "template"
	}
	if len(stem) > 48 {
		stem = stem[:48]
	}
	sum := sha256.Sum256(content)
	return TemplateFile{Name: name, ManagedName: fmt.Sprintf("%s-%x.jinja", stem, sum[:8]), Content: content}, nil
}

// existingTemplate never follows a symlink when deciding whether an imported
// name can be reused. A matching ordinary file is already done.
func existingTemplate(root, name string, content []byte) (bool, error) {
	path := filepath.Join(Dir(root), name)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("template destination %s is not a regular file", path)
	}
	existing, err := readTemplate(path)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(existing, content) {
		return false, fmt.Errorf("template destination %s already has different contents", path)
	}
	return true, nil
}
