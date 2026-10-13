// Package releaseartifact builds the deterministic, versioned Temper release
// archive and the third-party notice document that accompanies it.
package releaseartifact

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

const (
	TargetOS   = "darwin"
	TargetArch = "arm64"
)

var archiveTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// File is one required release-archive file. Modes and archive paths are
// assigned by BuildArchive rather than accepted from callers.
type File struct {
	Name string
	Data []byte
}

// NoticeFile is one license or notice file from a module root.
type NoticeFile struct {
	Name string
	Data []byte
}

// ModuleNotice describes the third-party material linked into the binary.
type ModuleNotice struct {
	Path    string
	Version string
	Files   []NoticeFile
}

// ValidateVersion accepts a SemVer release version without the tag's leading
// v. Canonical text is required so one release identity has one filename.
func ValidateVersion(value string) error {
	parsed, err := semver.StrictNewVersion(value)
	if err != nil {
		return fmt.Errorf("version %q is not strict SemVer: %w", value, err)
	}
	if parsed.String() != value {
		return fmt.Errorf("version %q is not canonical SemVer (want %q)", value, parsed.String())
	}
	return nil
}

// ArchiveBase returns the single top-level directory in the release ZIP.
func ArchiveBase(version string) (string, error) {
	if err := ValidateVersion(version); err != nil {
		return "", err
	}
	return fmt.Sprintf("temper_%s_%s_%s", version, TargetOS, TargetArch), nil
}

// ArchiveName returns the public release-asset filename.
func ArchiveName(version string) (string, error) {
	base, err := ArchiveBase(version)
	if err != nil {
		return "", err
	}
	return base + ".zip", nil
}

// BuildArchive returns the deterministic release ZIP, its lowercase SHA-256,
// and a shasum-compatible checksum file. The input must contain exactly the
// three files required by the release contract.
func BuildArchive(version string, files []File) ([]byte, string, []byte, error) {
	base, err := ArchiveBase(version)
	if err != nil {
		return nil, "", nil, err
	}
	archiveName := base + ".zip"

	required := []struct {
		name string
		mode fs.FileMode
	}{
		{name: "temper", mode: 0o755},
		{name: "LICENSE", mode: 0o644},
		{name: "THIRD_PARTY_NOTICES.txt", mode: 0o644},
	}
	provided := make(map[string][]byte, len(files))
	for _, file := range files {
		if _, exists := provided[file.Name]; exists {
			return nil, "", nil, fmt.Errorf("duplicate release file %q", file.Name)
		}
		provided[file.Name] = file.Data
	}
	if len(provided) != len(required) {
		return nil, "", nil, fmt.Errorf("release archive requires exactly %d files, got %d", len(required), len(provided))
	}

	var output bytes.Buffer
	zw := zip.NewWriter(&output)
	for _, item := range required {
		data, ok := provided[item.name]
		if !ok {
			_ = zw.Close()
			return nil, "", nil, fmt.Errorf("release archive is missing %q", item.name)
		}
		if len(data) == 0 {
			_ = zw.Close()
			return nil, "", nil, fmt.Errorf("release file %q is empty", item.name)
		}
		header := &zip.FileHeader{
			Name:   base + "/" + item.name,
			Method: zip.Deflate,
		}
		header.SetMode(item.mode)
		header.SetModTime(archiveTime)
		writer, createErr := zw.CreateHeader(header)
		if createErr != nil {
			_ = zw.Close()
			return nil, "", nil, fmt.Errorf("create ZIP entry %q: %w", item.name, createErr)
		}
		if _, writeErr := writer.Write(data); writeErr != nil {
			_ = zw.Close()
			return nil, "", nil, fmt.Errorf("write ZIP entry %q: %w", item.name, writeErr)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, "", nil, fmt.Errorf("finish release ZIP: %w", err)
	}

	digest := sha256.Sum256(output.Bytes())
	identity := hex.EncodeToString(digest[:])
	checksum := []byte(fmt.Sprintf("%s  %s\n", identity, archiveName))
	return output.Bytes(), identity, checksum, nil
}

// BuildNotices renders root license and notice files for every linked
// third-party module in stable module/file order.
func BuildNotices(modules []ModuleNotice) ([]byte, error) {
	if len(modules) == 0 {
		return nil, fmt.Errorf("third-party module list is empty")
	}
	modules = append([]ModuleNotice(nil), modules...)
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].Path == modules[j].Path {
			return modules[i].Version < modules[j].Version
		}
		return modules[i].Path < modules[j].Path
	})

	var output bytes.Buffer
	output.WriteString("Third-party notices for Temper\n\n")
	output.WriteString("The Temper source tree is licensed under 0BSD. The release binary also\n")
	output.WriteString("contains the following Go modules under their respective terms.\n")

	previous := ""
	for _, module := range modules {
		if strings.TrimSpace(module.Path) == "" || strings.TrimSpace(module.Version) == "" {
			return nil, fmt.Errorf("module path and version must be non-empty")
		}
		identity := module.Path + "@" + module.Version
		if identity == previous {
			return nil, fmt.Errorf("duplicate module notice %q", identity)
		}
		previous = identity
		if len(module.Files) == 0 {
			return nil, fmt.Errorf("module %s has no root license or notice files", identity)
		}

		files := append([]NoticeFile(nil), module.Files...)
		sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
		seen := make(map[string]struct{}, len(files))
		for _, file := range files {
			if !validNoticeName(file.Name) {
				return nil, fmt.Errorf("module %s has invalid notice filename %q", identity, file.Name)
			}
			if _, exists := seen[file.Name]; exists {
				return nil, fmt.Errorf("module %s repeats notice filename %q", identity, file.Name)
			}
			seen[file.Name] = struct{}{}
			if len(file.Data) == 0 {
				return nil, fmt.Errorf("module %s notice file %q is empty", identity, file.Name)
			}
		}

		output.WriteString("\n================================================================================\n")
		fmt.Fprintf(&output, "MODULE %s %s\n", module.Path, module.Version)
		for _, file := range files {
			fmt.Fprintf(&output, "\nFILE %s\n", file.Name)
			output.WriteString("--------------------------------------------------------------------------------\n")
			output.Write(file.Data)
			if file.Data[len(file.Data)-1] != '\n' {
				output.WriteByte('\n')
			}
		}
	}
	return output.Bytes(), nil
}

func validNoticeName(name string) bool {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || name == "." || name == ".." {
		return false
	}
	upper := strings.ToUpper(name)
	return strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "COPYING") || strings.HasPrefix(upper, "NOTICE")
}
