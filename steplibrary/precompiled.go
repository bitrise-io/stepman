package steplibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
func validateSHA256(path, expected string) error {
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
	defer func() { _ = f.Close() }()

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

// downloadPrecompiled fetches `executable` for the current platform, verifies
// its SHA256, makes the file executable, and atomically renames it into
// destDir as `<stepID>`. Returns the final binary path.
func (s *Steplib) downloadPrecompiled(ctx context.Context, stepID string, executable models.Executable, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create dest dir %s: %w", destDir, err)
	}

	tmp, err := os.CreateTemp(destDir, "bin-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file in %s: %w", destDir, err)
	}
	tmpPath := tmp.Name()

	url := precompiledURL(executable)
	body, err := s.fetcher.Get(ctx, url)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	_, copyErr := io.Copy(tmp, body)
	_ = body.Close()
	if copyErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("write to %s: %w", tmpPath, copyErr)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close %s: %w", tmpPath, err)
	}

	if err := validateSHA256(tmpPath, executable.Hash); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("chmod %s: %w", tmpPath, err)
	}

	binPath := filepath.Join(destDir, stepID)
	if err := os.Rename(tmpPath, binPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("rename to %s: %w", binPath, err)
	}
	return binPath, nil
}
