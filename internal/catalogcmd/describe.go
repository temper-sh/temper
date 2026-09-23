package catalogcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/software/catalogsource"
	"gopkg.in/yaml.v3"
)

// describe edits an explicit authoring file. It neither publishes a catalog nor
// touches installed/selected configurations. All cooperating writers lock the
// parent directory so replacing the file cannot change the lock's inode.
func describe(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	f := flag.NewFlagSet("temper catalog describe", flag.ContinueOnError)
	f.SetOutput(diagnostics)
	path := f.String("catalog", "", "local authoring catalog to edit")
	artifact := f.String("artifact", "", "model artifact ID")
	description := f.String("description", "", "your brief model assessment (empty clears it)")
	descriptionFile := f.String("description-file", "", "UTF-8 file containing your assessment")
	link := f.String("assessment-url", "", "optional assessment URL (empty clears the existing link)")
	ifEmpty := f.Bool("if-empty", false, "suggest only when no description exists")
	dry := f.Bool("dry-run", false, "show the proposed description without writing")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	explicit := map[string]bool{}
	f.Visit(func(v *flag.Flag) { explicit[v.Name] = true })
	if f.NArg() != 0 || *path == "" || *artifact == "" || explicit["description"] == explicit["description-file"] {
		return failed(diagnostics, errors.New("provide --catalog, --artifact and exactly one of --description or --description-file"))
	}
	if err := ctx.Err(); err != nil {
		return failed(diagnostics, err)
	}
	if !*dry {
		lock, err := lockDescriptionParent(filepath.Dir(*path))
		if err != nil {
			return failed(diagnostics, err)
		}
		defer lock.Close()
	}
	raw, info, err := readDescriptionFile(*path)
	if err != nil {
		return failed(diagnostics, err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		return failed(diagnostics, err)
	}
	if explicit["description-file"] {
		text, _, err := readDescriptionFile(*descriptionFile)
		if err != nil {
			return failed(diagnostics, err)
		}
		*description = strings.TrimSpace(string(text))
	}
	var assessmentURL *string
	if explicit["assessment-url"] {
		assessmentURL = link
	}
	edited, err := catalog.Describe(d, *artifact, *description, assessmentURL, *ifEmpty)
	if err != nil {
		return failed(diagnostics, err)
	}
	before, after := d.Artifacts[*artifact], edited.Artifacts[*artifact]
	changed := before.Description != after.Description || before.AssessmentURL != after.AssessmentURL
	if changed && !*dry {
		var candidate []byte
		if json.Valid(raw) {
			candidate, err = json.MarshalIndent(edited, "", "  ")
			candidate = append(candidate, '\n')
		} else {
			candidate, err = yaml.Marshal(edited)
		}
		if err != nil {
			return failed(diagnostics, err)
		}
		if _, err := catalog.Parse(candidate); err != nil {
			return failed(diagnostics, err)
		}
		if err := replaceDescription(ctx, *path, raw, candidate, info); err != nil {
			return failed(diagnostics, err)
		}
	}
	fmt.Fprintf(out, "RESULT catalog-describe %s artifact=%s path=%q\n", status(changed, *dry), *artifact, *path)
	fmt.Fprintln(out, after.Description)
	if after.AssessmentURL != "" {
		fmt.Fprintln(out, "Assessment: "+after.AssessmentURL)
	}
	return 0
}

func readDescriptionFile(path string) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > catalogsource.MaxCatalogBytes {
		return nil, nil, errors.New("catalog and description inputs must be bounded regular files, not symlinks")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, nil, errors.New("input changed while opening; retry the edit")
	}
	raw, err := io.ReadAll(io.LimitReader(f, catalogsource.MaxCatalogBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(raw)) > catalogsource.MaxCatalogBytes {
		return nil, nil, errors.New("input exceeds catalog size limit")
	}
	return raw, info, nil
}

func replaceDescription(ctx context.Context, path string, original, candidate []byte, info os.FileInfo) error {
	stage, err := os.CreateTemp(filepath.Dir(path), ".temper-description-*")
	if err != nil {
		return err
	}
	defer os.Remove(stage.Name())
	if err = stage.Chmod(info.Mode().Perm()); err == nil {
		_, err = stage.Write(candidate)
	}
	if err == nil {
		err = stage.Sync()
	}
	closeErr := stage.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	current, currentInfo, err := readDescriptionFile(path)
	if err != nil {
		return err
	}
	if !os.SameFile(info, currentInfo) || !bytes.Equal(original, current) {
		return errors.New("catalog changed during the edit; rerun with the current file")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(stage.Name(), path); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("description saved, but directory sync failed: %w", err)
	}
	return nil
}
