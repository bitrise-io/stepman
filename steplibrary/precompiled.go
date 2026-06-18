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

// PrecompiledStepsExperimentEnv gates precompiled-binary activation.
const PrecompiledStepsExperimentEnv = "BITRISE_EXPERIMENT_PRECOMPILED_STEPS"

// PrecompiledStepsStorageURLsEnv overrides the default storage base URLs (comma-separated).
const PrecompiledStepsStorageURLsEnv = "BITRISE_PRECOMPILED_STEPS_STORAGE_URLS"

// PrecompiledStepsEnabled reports whether the precompiled-steps experiment is enabled.
func PrecompiledStepsEnabled() bool {
	v := os.Getenv(PrecompiledStepsExperimentEnv)
	return v == "true" || v == "1"
}

// PrecompiledStepsDefaultStorageURLs lists the storage base URLs used when
// PrecompiledStepsStorageURLsEnv is unset.
var PrecompiledStepsDefaultStorageURLs = []string{
	"https://storage.googleapis.com/bitrise-steplib-storage",
	"https://storage-gateway.services.bitrise.io",
}

func currentPlatform() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

// ResolveExecutable returns the step's precompiled binary for the current
// platform, the platform key, and whether a usable binary was found.
func ResolveExecutable(step models.StepModel) (executable models.Executable, platform string, ok bool) {
	platform = currentPlatform()
	if step.Executables == nil {
		return models.Executable{}, platform, false
	}
	e, found := (*step.Executables)[platform]
	if !found || e.StorageURI == "" || e.Hash == "" {
		return models.Executable{}, platform, false
	}
	return e, platform, true
}

func precompiledURLs(e models.Executable) ([]string, error) {
	bases := PrecompiledStepsDefaultStorageURLs
	if override := os.Getenv(PrecompiledStepsStorageURLsEnv); override != "" {
		bases = strings.Split(override, ",")
	}
	return buildPrecompiledURLs(bases, e)
}

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

// DownloadPrecompiled downloads executable into destDir as <stepID> and returns
// the resulting binary path.
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
		return "", fmt.Errorf("set executable permission on file: %w", err)
	}
	return binPath, nil
}

func downloadFromURLs(ctx context.Context, fetcher httpfetch.Client, log stepman.Logger, destPath, expectedHash string, urls []string) error {
	var errs []error
	for _, url := range urls {
		err := fetcher.DownloadWithHash(ctx, destPath, url, expectedHash)
		if err != nil {
			log.Warnf("Failed to download from %s: %s", url, err)
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to download executable: %w", errors.Join(errs...))
}
