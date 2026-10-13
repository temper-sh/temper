package artifactset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RefreshCacheReceipt rehashes externally restored cache files before accepting
// their new modification times. Failure leaves the old receipt invalidated by
// the changed cache files; a retry cannot silently accept an unverified restore.
func (s Set) RefreshCacheReceipt(ctx context.Context) error {
	if _, err := s.inspect(true); err != nil {
		return err
	}
	r, err := readReceipt(s.Path(), s.layoutID, s.digest)
	if err != nil {
		return err
	}
	previous, err := s.Receipt(r.Files, r.HFCache)
	if err != nil {
		return err
	}
	for i, record := range r.Files {
		path := filepath.Join(s.Path(), filepath.FromSlash(record.Path))
		before, err := os.Stat(path)
		if err != nil {
			return err
		}
		actual, err := fileSHA256(ctx, path)
		if err != nil {
			return err
		}
		if actual != record.SHA256 {
			return fmt.Errorf("%w: %q has %s, lock requires %s", ErrContentMismatch, record.Path, actual, record.SHA256)
		}
		after, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
			return fmt.Errorf("artifact file changed during verification: %s", record.Path)
		}
		if record.Source != "" {
			r.Files[i].ModifiedNS = after.ModTime().UnixNano()
		}
	}
	data, err := s.Receipt(r.Files, r.HFCache)
	if err != nil {
		return err
	}
	if err := verifyReceiptShape(s.Path(), r); err != nil {
		return err
	}
	if bytes.Equal(data, previous) {
		return ctx.Err()
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path()), ".receipt-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	path := filepath.Join(s.Path(), "receipt.json")
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, previous) {
		if bytes.Equal(current, data) {
			return s.Verify()
		}
		return errors.New("artifact receipt changed during verification; retry preparation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(s.Path())
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
