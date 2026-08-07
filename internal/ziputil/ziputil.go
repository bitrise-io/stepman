package ziputil

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)
func UnZip(zipPath, destDir string) (err error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", zipPath, err)
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close zip %s: %w", zipPath, closeErr))
		}
	}()

	cleanDest := filepath.Clean(destDir)
	if err := os.MkdirAll(cleanDest, 0o755); err != nil {
		return fmt.Errorf("create destination dir %s: %w", cleanDest, err)
	}

	for _, f := range r.File {
		if err := extractEntry(f, cleanDest); err != nil {
			return err
		}
	}

	return nil
}

func extractEntry(f *zip.File, destDir string) error {
	// filepath.Join cleans the result, so a "../escape" entry resolves outside
	// destDir; reject anything that does not stay under it.
	path := filepath.Join(destDir, f.Name)
	if path != destDir && !strings.HasPrefix(path, destDir+string(os.PathSeparator)) {
		return fmt.Errorf("illegal path in zip entry: %s", f.Name)
	}

	if f.FileInfo().IsDir() {
		if err := os.MkdirAll(path, f.Mode().Perm()); err != nil {
			return fmt.Errorf("create dir %s: %w", path, err)
		}
		return nil
	}

	// Entries are not required to be preceded by their parent directory entry.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir %s: %w", filepath.Dir(path), err)
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", f.Name, err)
	}
	defer func() {
		// Read-side close failures cannot corrupt what was written, and the
		// error that matters is the one from the copy below.
		_ = rc.Close()
	}()

	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := io.Copy(dst, rc); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), dst.Close())
	}
	// A failed Close can mean the final write never reached disk, so the file may
	// be incomplete: treat it as a hard failure rather than ignoring it.
	if err := dst.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}

	return nil
}
