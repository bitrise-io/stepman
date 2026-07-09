package httpcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/stepman/internal/sri"
)

const metaFilename = "meta.json"

// Meta is the human-readable metadata stored beside each cached body in
// meta.json. It records where the body came from, the validators needed for
// conditional revalidation, and when the entry stops being fresh.
type Meta struct {
	URL          string    `json:"url"`
	Method       string    `json:"method"`
	Status       int       `json:"status"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	CacheControl string    `json:"cache_control,omitempty"`
	ContentType  string    `json:"content_type,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
	// ExpiresAt is FetchedAt+max-age; equal to FetchedAt when the response must
	// be revalidated before every reuse. Ignored when Immutable is true.
	ExpiresAt  time.Time `json:"expires_at"`
	Immutable  bool      `json:"immutable"`
	BodyFile   string    `json:"body_file"`   // basename of the body file in this entry dir
	BodySHA256 string    `json:"body_sha256"` // "sha256-<hex>"
	BodySize   int64     `json:"body_size"`
}

// Store is an on-disk HTTP cache laid out as one directory per entry, named
// "<sha256(method+url)>-<basename>" (Nix-inspired: a machine-readable hash plus
// a human-readable name). Each directory holds the body verbatim under its real
// basename and a meta.json describing it. A single ".tmp" subdir holds
// half-built entries before they are atomically renamed into place. Entries are
// never auto-evicted.
type Store struct {
	root string
}

// NewStore returns a Store rooted at dir. The directory is created lazily on
// the first Save.
func NewStore(dir string) *Store {
	return &Store{root: dir}
}

func (s *Store) entryDir(key string) string { return filepath.Join(s.root, key) }

func (s *Store) metaPath(key string) string { return filepath.Join(s.entryDir(key), metaFilename) }

// stagingDir holds half-built entries before they are atomically renamed into
// place. It lives under root so the rename stays on one filesystem (a
// cross-filesystem rename would fail), keeps the root listing free of temp
// dirs, and gives a GC a single place to sweep orphaned staging dirs. Its
// leading dot keeps it from colliding with any entry key (keys start with a hex
// hash).
func (s *Store) stagingDir() string { return filepath.Join(s.root, ".tmp") }

// Lookup reads the entry's meta.json. A missing entry or an unparseable
// meta.json is reported as a miss (found == false) with no error, so a corrupt
// entry simply triggers a refetch. A real error is returned only for unexpected
// IO failures. The body file's existence and integrity are checked separately
// by ReadBody, so a hit here does not guarantee a usable body.
func (s *Store) Lookup(key string) (meta Meta, found bool, err error) {
	data, err := os.ReadFile(s.metaPath(key))
	if errors.Is(err, fs.ErrNotExist) {
		return Meta{}, false, nil
	}
	if err != nil {
		return Meta{}, false, fmt.Errorf("read cache meta %s: %w", s.metaPath(key), err)
	}

	var m Meta
	if uerr := json.Unmarshal(data, &m); uerr != nil {
		return Meta{}, false, nil // corrupt metadata -> treat as miss
	}
	return m, true, nil
}

// ReadBody reads the cached body and verifies it against m.BodySHA256. A read
// failure (body gone) or a checksum mismatch (corruption / a torn concurrent
// write) is returned as an error so the caller treats the entry as unusable and
// refetches. Loading the body here in one read also avoids a separate
// existence-stat, closing the stat/open TOCTOU window.
func (s *Store) ReadBody(key string, m Meta) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(s.entryDir(key), m.BodyFile))
	if err != nil {
		return nil, fmt.Errorf("read cached body %s: %w", key, err)
	}
	if m.BodySHA256 != "" {
		if got := sri.SHA256(data); got != m.BodySHA256 {
			return nil, fmt.Errorf("cached body %s checksum mismatch: have %s, want %s", key, got, m.BodySHA256)
		}
	}
	return data, nil
}

// Save writes the body and meta.json into a fresh staging directory and then
// renames that directory into place as a single atomic step. Committing the
// whole entry at once means a reader (or a concurrent writer) never observes a
// directory whose meta.json and body came from different writes: an entry dir
// is only ever created by an atomic rename of a fully-written staging dir, so
// it is always complete (or absent). When the entry already exists it is
// replaced; a concurrent writer of the same key may win the rename instead,
// which is fine since both wrote the same resource.
func (s *Store) Save(key string, m Meta, body []byte) (err error) {
	staging := s.stagingDir()
	if mkErr := os.MkdirAll(staging, 0o755); mkErr != nil {
		return fmt.Errorf("create staging dir %s: %w", staging, mkErr)
	}
	tmpDir, err := os.MkdirTemp(staging, "entry-*")
	if err != nil {
		return fmt.Errorf("create temp entry dir in %s: %w", staging, err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(tmpDir))
		}
	}()

	metaBytes, err := marshalMeta(m)
	if err != nil {
		return err
	}
	if werr := os.WriteFile(filepath.Join(tmpDir, m.BodyFile), body, 0o644); werr != nil {
		return fmt.Errorf("write body in %s: %w", tmpDir, werr)
	}
	if werr := os.WriteFile(filepath.Join(tmpDir, metaFilename), metaBytes, 0o644); werr != nil {
		return fmt.Errorf("write meta in %s: %w", tmpDir, werr)
	}
	if cherr := os.Chmod(tmpDir, 0o755); cherr != nil {
		return fmt.Errorf("chmod %s: %w", tmpDir, cherr)
	}

	entryDir := s.entryDir(key)
	if rmErr := os.RemoveAll(entryDir); rmErr != nil {
		return fmt.Errorf("clear existing entry %s: %w", entryDir, rmErr)
	}
	if rnErr := os.Rename(tmpDir, entryDir); rnErr != nil {
		return fmt.Errorf("commit entry %s: %w", entryDir, rnErr)
	}
	return nil
}

// Touch rewrites meta.json only, used to refresh timestamps/validators after a
// 304 Not Modified without rewriting the unchanged body. The body filename and
// content are unchanged, so the in-place atomic meta rewrite keeps the entry
// self-consistent.
func (s *Store) Touch(key string, m Meta) error {
	metaBytes, err := marshalMeta(m)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.metaPath(key), metaBytes, 0o644)
}

func marshalMeta(m Meta) ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "\t")
	if err != nil {
		return nil, fmt.Errorf("marshal cache meta: %w", err)
	}
	return append(data, '\n'), nil
}

// writeFileAtomic writes data to a temp file in the destination directory and
// renames it into place, so readers never see a half-written file. The
// destination directory must already exist.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(tmpName))
		}
	}()

	if _, werr := tmp.Write(data); werr != nil {
		return errors.Join(fmt.Errorf("write %s: %w", tmpName, werr), tmp.Close())
	}
	if cerr := tmp.Close(); cerr != nil {
		return fmt.Errorf("close %s: %w", tmpName, cerr)
	}
	if cherr := os.Chmod(tmpName, perm); cherr != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, cherr)
	}
	if rerr := os.Rename(tmpName, path); rerr != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, rerr)
	}
	return nil
}
