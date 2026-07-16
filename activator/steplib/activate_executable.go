package steplib

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
)

func activateStepExecutable(
	ctx context.Context,
	fetcher httpfetch.Client,
	stepID, version, platform string,
	executable models.Executable,
	destinationDir string,
	logger stepman.Logger,
	storageURLs []string,
) (string, error) {
	cachePath, err := stepExecutableCachePath(stepID, version, platform)
	if err != nil {
		return "", fmt.Errorf("executable cache path: %w", err)
	}

	expectedHash, err := parseExpectedHash(executable.Hash)
	if err != nil {
		return "", fmt.Errorf("parse expected hash: %w", err)
	}

	needsDownload := false
	switch err := validateHash(cachePath, expectedHash); {
	case err == nil:
		logger.Debugf("Disk cache hit for %s@%s", stepID, version)
	case errors.Is(err, fs.ErrNotExist):
		needsDownload = true
		logger.Debugf("Disk cache miss for %s@%s", stepID, version)
	default:
		logger.Warnf("Cached step executable failed validation, re-downloading: %s", err)
		if rmErr := os.Remove(cachePath); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			logger.Warnf("Failed to remove invalid cache entry %s: %s", cachePath, rmErr)
		}
		needsDownload = true
	}

	if needsDownload {
		if err := downloadExecutable(ctx, fetcher, executable, expectedHash, cachePath, logger, storageURLs); err != nil {
			return "", err
		}
		if err := os.Chmod(cachePath, 0755); err != nil {
			return "", fmt.Errorf("set executable permission on file: %s", err)
		}
	}

	// Instead of returning the cache path, copy the executable to the destination dir.
	// Bitrise CLI runs in a wide variety of environments, including user-provided container images.
	// We can't make assumptions about the cache directory's path and availability, especially when
	// mounted across containers with different usernames, filesystems, etc.
	destPath := filepath.Join(destinationDir, stepID)
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return "", fmt.Errorf("create destination dir: %w", err)
	}
	if err := fileutil.NewFileManager().CopyFile(cachePath, destPath, &fileutil.CopyOptions{Overwrite: true}); err != nil {
		return "", fmt.Errorf("copy cached executable to destination: %w", err)
	}

	return destPath, nil
}

func stepExecutableCachePath(stepID, version, platform string) (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	base := filepath.Join(userCacheDir, "bitrise", "steps", "executables")
	return filepath.Join(base, stepID, version, platform, stepID), nil
}

func buildDownloadURLs(bases []string, executable models.Executable) ([]string, error) {
	uri := strings.TrimLeft(executable.StorageURI, "/")
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

func downloadExecutable(ctx context.Context, fetcher httpfetch.Client, executable models.Executable, expectedHash, destPath string, logger stepman.Logger, storageURLs []string) error {
	urls, err := buildDownloadURLs(storageURLs, executable)
	if err != nil {
		return err
	}

	return downloadFromURLs(ctx, fetcher, urls, destPath, expectedHash, logger)
}

// downloadFromURLs tries each URL in order via fetcher, verifying the expected
// hash on each attempt; a mismatch or failure falls through to the next mirror,
// logging each failed attempt so a mirror silently degrading isn't invisible on
// fallback success.
func downloadFromURLs(ctx context.Context, fetcher httpfetch.Client, urls []string, destPath, expectedSHA256 string, logger stepman.Logger) error {
	var errs []error
	for _, url := range urls {
		err := fetcher.DownloadWithHash(ctx, destPath, url, expectedSHA256)
		if err == nil {
			return nil
		}
		// err already names the failing URL (fetcher wraps it in the underlying
		// GET/status/hash-mismatch error), so it isn't repeated here.
		logger.Warnf("Failed to download step executable: %s", err)
		errs = append(errs, fmt.Errorf("%s: %w", url, err))
	}
	return fmt.Errorf("failed to download executable: %w", errors.Join(errs...))
}

func parseExpectedHash(hash string) (string, error) {
	if hash == "" {
		return "", fmt.Errorf("hash is empty")
	}
	if !strings.HasPrefix(hash, "sha256-") {
		return "", fmt.Errorf("only SHA256 hashes supported at this time, make sure to prefix the hash with `sha256-`. Found hash value: %s", hash)
	}
	return strings.TrimPrefix(hash, "sha256-"), nil
}

func validateHash(filePath string, expectedHexHash string) error {
	reader, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	h := sha256.New()
	_, err = io.Copy(h, reader)
	if err != nil {
		return fmt.Errorf("calculate hash: %w", err)
	}
	actualHash := hex.EncodeToString(h.Sum(nil))
	if actualHash != expectedHexHash {
		return fmt.Errorf("hash mismatch: expected sha256-%s, got sha256-%s", expectedHexHash, actualHash)
	}
	return nil
}
