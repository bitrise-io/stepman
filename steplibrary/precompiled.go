package steplibrary

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
)

// PrecompiledStepsExperimentEnv gates precompiled-binary activation; both the V1
// activator and the V2 reader prefer a published binary only when it is enabled.
const PrecompiledStepsExperimentEnv = "BITRISE_EXPERIMENT_PRECOMPILED_STEPS"

// PrecompiledStepsStorageURLsEnv overrides the ordered list of storage base URLs
// at runtime (comma-separated).
const PrecompiledStepsStorageURLsEnv = "BITRISE_PRECOMPILED_STEPS_STORAGE_URLS"

// PrecompiledStepsEnabled reports whether the precompiled-steps experiment is on.
func PrecompiledStepsEnabled() bool {
	v := os.Getenv(PrecompiledStepsExperimentEnv)
	return v == "true" || v == "1"
}

// PrecompiledStepsDefaultStorageURLs is the ordered list of storage base URLs
// used when PrecompiledStepsStorageURLsEnv is unset.
var PrecompiledStepsDefaultStorageURLs = []string{
	"https://storage.googleapis.com/bitrise-steplib-storage",
	"https://storage-gateway.services.bitrise.io",
}

// currentPlatform returns the runtime platform key (e.g. "darwin-arm64") used
// to look up an entry in models.StepModel.Executables.
func currentPlatform() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

// ResolveExecutable picks the precompiled binary for the current OS+arch from
// the step model. Returns false when no precompiled binary is available, the
// platform isn't covered, or the entry is missing storage_uri/hash.
func ResolveExecutable(step models.StepModel) (models.Executable, bool) {
	if step.Executables == nil {
		return models.Executable{}, false
	}
	e, ok := (*step.Executables)[currentPlatform()]
	if !ok || e.StorageURI == "" || e.Hash == "" {
		return models.Executable{}, false
	}
	return e, true
}

// precompiledURLs builds the ordered list of download URLs for an executable,
// taking the storage bases from PrecompiledStepsStorageURLsEnv or the built-in
// defaults.
func precompiledURLs(e models.Executable) ([]string, error) {
	bases := PrecompiledStepsDefaultStorageURLs
	if override := os.Getenv(PrecompiledStepsStorageURLsEnv); override != "" {
		bases = strings.Split(override, ",")
	}
	return buildPrecompiledURLs(bases, e)
}

// buildPrecompiledURLs joins each base with the executable's storage URI,
// normalizing slashes and rejecting plain http.
func buildPrecompiledURLs(bases []string, e models.Executable) ([]string, error) {
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

// DownloadPrecompiled fetches `executable` for the current platform via fetcher,
// verifies its SHA256, makes the file executable, and places it at
// destDir/<stepID>. Returns the final binary path. Shared by the V2 reader and
// the V1 activator.
func DownloadPrecompiled(ctx context.Context, fetcher httpfetch.Client, log stepman.Logger, stepID string, executable models.Executable, destDir string) (binPath string, err error) {
	if executable.Hash == "" {
		return "", fmt.Errorf("hash is empty")
	}
	urls, err := precompiledURLs(executable)
	if err != nil {
		return "", err
	}

	binPath = filepath.Join(destDir, stepID)
	if err = downloadFromURLs(ctx, fetcher, log, binPath, executable.Hash, urls); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(binPath))
		}
	}()

	if err = os.Chmod(binPath, 0o700); err != nil {
		return "", fmt.Errorf("chmod %s: %w", binPath, err)
	}
	return binPath, nil
}

// downloadFromURLs tries each url in order, verifying expectedHash on each attempt.
func downloadFromURLs(ctx context.Context, fetcher httpfetch.Client, log stepman.Logger, destPath, expectedHash string, urls []string) error {
	var errs []error
	for _, url := range urls {
		if err := fetcher.DownloadWithHash(ctx, destPath, url, expectedHash); err == nil {
			return nil
		} else {
			log.Warnf("Failed to download from %s: %s", url, err)
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
		}
	}
	return fmt.Errorf("failed to download executable: %w", errors.Join(errs...))
}
