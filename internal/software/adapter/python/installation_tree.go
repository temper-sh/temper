package python

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	softwarearchive "github.com/temper-sh/temper/internal/software/archive"
	"github.com/temper-sh/temper/internal/software/installplan"
)

type environmentEntry = softwarearchive.Entry

func scanEnvironment(ctx context.Context, root string) ([]environmentEntry, error) {
	return softwarearchive.ScanTree(ctx, root, "Python environment")
}

func copyPythonWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128<<10)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			output, writeErr := destination.Write(buffer[:count])
			written += int64(output)
			if writeErr != nil {
				return written, writeErr
			}
			if output != count {
				return written, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func writePythonFile(path string, data []byte, mode fs.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("write Python installation file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func syncPythonTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, current)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if err := syncPythonDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func syncPythonDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func preparePythonScope(installation installplan.Installation, scopeRoot string) error {
	if !realPythonDirectory(installation.Root) {
		return errors.New("Python installation root must already exist as a real directory")
	}
	relative, err := filepath.Rel(installation.Root, scopeRoot)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("Python scope is outside the installation root")
	}
	current := installation.Root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		parent := current
		current = filepath.Join(current, component)
		if err := ensurePythonChildDirectory(parent, current); err != nil {
			return err
		}
	}
	return nil
}

func ensurePythonChildDirectory(parent, child string) error {
	info, err := os.Lstat(child)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(child, 0o755); err != nil {
			return fmt.Errorf("create Python directory below %q: %w", parent, err)
		}
		return syncPythonDirectory(parent)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Python path %q is not a real directory", child)
	}
	return nil
}

func inspectRealPythonGroupPath(root, group string) (bool, bool, error) {
	if !strictlyBelowPython(root, group) {
		return false, false, errors.New("Python group is outside installation root")
	}
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return true, false, nil
	}
	relative, err := filepath.Rel(root, group)
	if err != nil {
		return false, false, err
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err = os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return false, true, nil
		}
		if err != nil {
			return false, false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return true, false, nil
		}
	}
	return true, true, nil
}

func currentGeneration(installation installplan.Installation, scope string) (string, string, error) {
	scopeRoot := pythonGroupRoot(installation, scope)
	target, err := os.Readlink(filepath.Join(scopeRoot, "current"))
	if err != nil || filepath.IsAbs(target) || filepath.ToSlash(filepath.Clean(target)) != filepath.ToSlash(target) || !strings.HasPrefix(filepath.ToSlash(target), "generations/generation-") {
		return "", "", errors.New("Python current pointer is invalid")
	}
	generation := filepath.Join(scopeRoot, filepath.FromSlash(target))
	if !strictlyBelowPython(scopeRoot, generation) || !realPythonDirectory(generation) {
		return "", "", errors.New("Python current generation is invalid")
	}
	return generation, filepath.Base(generation), nil
}

func exactPythonGroupLayout(scopeRoot, currentGeneration string) error {
	entries, err := os.ReadDir(scopeRoot)
	if err != nil || len(entries) != 2 {
		return errors.New("Python group has unexpected root entries")
	}
	seenCurrent, seenGenerations := false, false
	for _, entry := range entries {
		switch entry.Name() {
		case "current":
			info, err := entry.Info()
			seenCurrent = err == nil && info.Mode()&os.ModeSymlink != 0
		case "generations":
			seenGenerations = entry.IsDir() && entry.Type()&os.ModeSymlink == 0
		}
	}
	if !seenCurrent || !seenGenerations {
		return errors.New("Python group root is malformed")
	}
	generations, err := os.ReadDir(filepath.Join(scopeRoot, "generations"))
	if err != nil || len(generations) != 1 || generations[0].Name() != currentGeneration || !generations[0].IsDir() || generations[0].Type()&os.ModeSymlink != 0 {
		return errors.New("Python group has unexpected generations")
	}
	return nil
}

func cleanPythonGenerations(scopeRoot, current string) error {
	generationsPath := filepath.Join(scopeRoot, "generations")
	entries, err := os.ReadDir(generationsPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == current {
			continue
		}
		if err := os.RemoveAll(filepath.Join(generationsPath, entry.Name())); err != nil {
			return err
		}
	}
	return syncPythonDirectory(generationsPath)
}

func realPythonDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func regularPythonFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func regularPythonExecutable(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0o111 != 0
}

func strictlyBelowPython(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
