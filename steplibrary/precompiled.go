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

// precompiledURL resolves an Executable's storage_uri against the configured
// primary storage (BITRISE_PRECOMPILED_STEPS_PRIMARY_STORAGE) or the default
// GCS bucket.
func precompiledURL(e models.Executable) string {
	base := os.Getenv(steplib.PrecompiledStepsPrimaryStorageEnv)
	if base == "" {
		base = steplib.PrecompiledStepsDefaultStorage
	}
	base = strings.TrimRight(base, "/")
	return fmt.Sprintf("%s/%s", base, strings.TrimLeft(e.StorageURI, "/"))
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

// fetchBinary downloads url, validates its hash, and marks it executable,
// leaving the result at a staging path inside destDir. On error the staging
// file is removed; on success the caller owns cleanup.
func (s *Steplib) fetchBinary(ctx context.Context, executable models.Executable, destDir, stepID string) (_ string, err error) {
	url := precompiledURL(executable)
	stagingPath := filepath.Join(destDir, stepID+".staging")
	if dlErr := s.fetcher.Download(ctx, stagingPath, url); dlErr != nil {
		return "", fmt.Errorf("download %s: %w", url, dlErr)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(stagingPath))
		}
	}()

	if err = validateSHA256(stagingPath, executable.Hash); err != nil {
		return "", err
	}
	if err = os.Chmod(stagingPath, 0o755); err != nil {
		return "", fmt.Errorf("chmod %s: %w", stagingPath, err)
	}
	return stagingPath, nil
}

// downloadPrecompiled fetches `executable` for the current platform, verifies
// its SHA256, makes the file executable, and atomically renames it into
// destDir as `<stepID>`. Returns the final binary path.
func (s *Steplib) downloadPrecompiled(ctx context.Context, stepID string, executable models.Executable, destDir string) (string, error) {
	stagingPath, err := s.fetchBinary(ctx, executable, destDir, stepID)
	if err != nil {
		return "", err
	}

	binPath := filepath.Join(destDir, stepID)
	if renameErr := os.Rename(stagingPath, binPath); renameErr != nil {
		return "", errors.Join(fmt.Errorf("rename to %s: %w", binPath, renameErr), os.Remove(stagingPath))
	}
	return binPath, nil
}
