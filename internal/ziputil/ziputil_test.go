package ziputil

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// zipEntry is a single archive member; mode is applied when non-zero.
type zipEntry struct {
	name    string
	content string
	mode    os.FileMode
}

func writeZip(t *testing.T, entries []zipEntry) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "archive.zip")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	zw := zip.NewWriter(f)
	for _, e := range entries {
		header := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			header.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(header)
		require.NoError(t, err)
		_, err = w.Write([]byte(e.content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())

	return path
}

func TestUnZip(t *testing.T) {
	t.Run("extracts files and nested dirs, creating destDir", func(t *testing.T) {
		zipPath := writeZip(t, []zipEntry{
			{name: "step.yml", content: "title: Test\n"},
			// No explicit directory entry: the parent must still be created.
			{name: "scripts/run.sh", content: "echo hi\n"},
		})
		destDir := filepath.Join(t.TempDir(), "not-yet-created")

		require.NoError(t, UnZip(zipPath, destDir))

		got, err := os.ReadFile(filepath.Join(destDir, "step.yml"))
		require.NoError(t, err)
		require.Equal(t, "title: Test\n", string(got))

		got, err = os.ReadFile(filepath.Join(destDir, "scripts", "run.sh"))
		require.NoError(t, err)
		require.Equal(t, "echo hi\n", string(got))
	})

	t.Run("preserves the executable bit", func(t *testing.T) {
		zipPath := writeZip(t, []zipEntry{{name: "run.sh", content: "echo hi\n", mode: 0o755}})
		destDir := t.TempDir()

		require.NoError(t, UnZip(zipPath, destDir))

		info, err := os.Stat(filepath.Join(destDir, "run.sh"))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	})

	// The reason this package exists rather than using command.UnZIP, which joins
	// entry names onto destDir with no containment check.
	t.Run("rejects entries that escape destDir", func(t *testing.T) {
		for _, name := range []string{"../escaped.txt", "nested/../../escaped.txt"} {
			t.Run(name, func(t *testing.T) {
				zipPath := writeZip(t, []zipEntry{{name: name, content: "pwned"}})
				parent := t.TempDir()
				destDir := filepath.Join(parent, "dest")

				err := UnZip(zipPath, destDir)
				require.Error(t, err)
				require.Contains(t, err.Error(), "illegal path in zip entry")

				require.NoFileExists(t, filepath.Join(parent, "escaped.txt"))
			})
		}
	})

	// "foo..bar" contains ".." but stays inside destDir; a substring-based check
	// would refuse it.
	t.Run("allows dots inside a legitimate entry name", func(t *testing.T) {
		zipPath := writeZip(t, []zipEntry{{name: "module..framework/step.yml", content: "ok"}})
		destDir := t.TempDir()

		require.NoError(t, UnZip(zipPath, destDir))
		require.FileExists(t, filepath.Join(destDir, "module..framework", "step.yml"))
	})

	t.Run("errors on a corrupt archive", func(t *testing.T) {
		zipPath := filepath.Join(t.TempDir(), "corrupt.zip")
		require.NoError(t, os.WriteFile(zipPath, []byte("not a zip"), 0o644))

		err := UnZip(zipPath, t.TempDir())
		require.Error(t, err)
		require.Contains(t, err.Error(), "open zip")
	})
}
