package specgen

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// fileWriter abstracts the OS calls used by writeJSON, making them injectable
// for testing without affecting the fs.FS-based read path.
type fileWriter interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(name string, data []byte, perm os.FileMode) error
}

type realFileWriter struct{}

func (realFileWriter) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (realFileWriter) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

// writer emits files under outputDir and tracks file count + byte count for Stats.
type writer struct {
	outputDir string
	fw        fileWriter
	fileCount int
	byteCount int64
}

func (w *writer) writeJSON(relPath string, v any) error {
	bytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	full := filepath.Join(w.outputDir, relPath)
	if err := w.fw.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := w.fw.WriteFile(full, bytes, 0o644); err != nil {
		return err
	}
	w.fileCount++
	w.byteCount += int64(len(bytes))
	return nil
}

func (w *writer) copyFileFromFS(srcFS fs.FS, srcPath, relDst string) error {
	dst := filepath.Join(w.outputDir, relDst)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := srcFS.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	n, err := io.Copy(out, in)
	if err != nil {
		return err
	}
	w.fileCount++
	w.byteCount += n
	return nil
}
