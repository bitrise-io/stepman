package steplibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bitrise-io/stepman/activator/steplib"
	"github.com/bitrise-io/stepman/models"
)

// currentPlatform returns the runtime platform key (e.g. "darwin-arm64") used
// to look up an entry in models.StepModel.Executables.
func currentPlatform() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

// resolveExecutable picks the binary for the current OS+arch from the step
// model. Returns false when no precompiled binary is available, when the
// platform isn't covered, or when the entry is missing storage_uri/hash.
func resolveExecutable(step models.StepModel) (models.Executable, bool) {
	if step.Executables == nil {
		return models.Executable{}, false
	}
	e, ok := (*step.Executables)[currentPlatform()]
	if !ok || e.StorageURI == "" || e.Hash == "" {
		return models.Executable{}, false
	}
	return e, true
}

// precompiledURLs builds the ordered list of download URLs for an executable.
// Bases come from BITRISE_PRECOMPILED_STEPS_STORAGE_URLS (comma-separated) or
// the two built-in defaults (GCS bucket + storage gateway).
func precompiledURLs(e models.Executable) ([]string, error) {
	bases := steplib.PrecompiledStepsDefaultStorageURLs
	if override := os.Getenv(steplib.PrecompiledStepsStorageURLsEnv); override != "" {
		bases = strings.Split(override, ",")
	}

	uri := strings.TrimLeft(e.StorageURI, "/")
	var urls []string
	for _, base := range bases {
		base = strings.TrimRight(strings.TrimSpace(base), "/")
		if base == "" {
			continue
		}
		url := fmt.Sprintf("%s/%s", base, uri)
		if strings.HasPrefix(url, "http://") {
			return nil, fmt.Errorf("http URL is unsupported, please use https: %s", url)
		}
		urls = append(urls, url)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no storage URLs configured")
	}
	return urls, nil
}

// validateSHA256 verifies that the file at path matches the given hash. The
// expected hash must be a "sha256-<hex>" string (the convention carried over
// from V1 step.yml).
func validateSHA256(path, expected string) (err error) {
	if expected == "" {
		return fmt.Errorf("hash is empty")
	}
	if !strings.HasPrefix(expected, "sha256-") {
		return fmt.Errorf("only sha256 hashes supported, got: %s", expected)
	}
	expected = strings.TrimPrefix(expected, "sha256-")

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("hash mismatch for %s: expected sha256-%s, got sha256-%s", path, expected, got)
	}
	return nil
}

// downloadFromURLs tries each url in order, returning on the first success.
func (s *Steplib) downloadFromURLs(ctx context.Context, destPath string, urls []string) error {
	var errs []error
	for _, url := range urls {
		if err := s.fetcher.Download(ctx, destPath, url); err == nil {
			return nil
		} else {
			s.log.Warnf("Failed to download from %s: %s\n", url, err)
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
		}
	}
	return fmt.Errorf("failed to download executable: %w", errors.Join(errs...))
}

// downloadPrecompiled fetches `executable` for the current platform, verifies
// its SHA256, makes the file executable, and places it at destDir/<stepID>.
// Returns the final binary path.
func (s *Steplib) downloadPrecompiled(ctx context.Context, stepID string, executable models.Executable, destDir string) (binPath string, err error) {
	urls, err := precompiledURLs(executable)
	if err != nil {
		return "", err
	}

	binPath = filepath.Join(destDir, stepID)
	if err = s.downloadFromURLs(ctx, binPath, urls); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(binPath))
		}
	}()

	if err = validateSHA256(binPath, executable.Hash); err != nil {
		return "", err
	}
	if err = os.Chmod(binPath, 0o755); err != nil {
		return "", fmt.Errorf("chmod %s: %w", binPath, err)
	}

	return binPath, nil
}
