package httpcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
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
// basename and a meta.json describing it. Entries are never auto-evicted, so a
// stale entry stays available as a last-resort fallback when the network fails.
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

// Lookup reads the entry's meta.json. A missing entry, an unparseable meta.json,
// or a meta.json whose body file is gone are all reported as a miss (found ==
// false) with no error, so a corrupt entry simply triggers a refetch. A real
// error is returned only for unexpected IO failures.
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
	if _, serr := os.Stat(filepath.Join(s.entryDir(key), m.BodyFile)); serr != nil {
		return Meta{}, false, nil // body gone -> treat as miss
	}
	return m, true, nil
}

// Open returns a reader over the cached body. The caller closes it.
func (s *Store) Open(key string, m Meta) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(s.entryDir(key), m.BodyFile))
	if err != nil {
		return nil, fmt.Errorf("open cached body %s: %w", key, err)
	}
	return f, nil
}

// Save writes the body and then meta.json into the entry directory, each via a
// temp-file-plus-rename so a partially written entry is never observable. The
// body is written before meta.json: a directory without a valid meta.json reads
// back as a miss.
func (s *Store) Save(key string, m Meta, body []byte) error {
	dir := s.entryDir(key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir %s: %w", dir, err)
	}
	if err := writeFileAtomic(filepath.Join(dir, m.BodyFile), body, 0o644); err != nil {
		return err
	}
	return s.writeMeta(key, m)
}

// Touch rewrites meta.json only, used to refresh timestamps/validators after a
// 304 Not Modified without rewriting the unchanged body.
func (s *Store) Touch(key string, m Meta) error {
	return s.writeMeta(key, m)
}

func (s *Store) writeMeta(key string, m Meta) error {
	data, err := json.MarshalIndent(m, "", "\t")
	if err != nil {
		return fmt.Errorf("marshal cache meta %s: %w", key, err)
	}
	return writeFileAtomic(s.metaPath(key), append(data, '\n'), 0o644)
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
