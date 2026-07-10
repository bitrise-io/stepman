package steplib

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
)

func activateStepExecutable(
	ctx context.Context,
	fetcher httpfetch.Client,
	log stepman.Logger,
	stepID string,
	executable models.Executable,
	destinationDir string,
) (string, error) {
	path := filepath.Join(destinationDir, stepID)

	if err := downloadExecutable(ctx, fetcher, log, executable, path); err != nil {
		return "", err
	}

	if err := os.Chmod(path, 0755); err != nil {
		return "", fmt.Errorf("set executable permission on file: %s", err)
	}

	return path, nil
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

func downloadExecutable(ctx context.Context, fetcher httpfetch.Client, log stepman.Logger, executable models.Executable, destPath string) error {
	bases := precompiledStepsDefaultStorageURLs
	if override := os.Getenv(precompiledStepsStorageURLsEnv); override != "" {
		bases = strings.Split(override, ",")
	}

	urls, err := buildDownloadURLs(bases, executable)
	if err != nil {
		return err
	}
	return downloadFromURLs(ctx, fetcher, log, urls, executable.Hash, destPath)
}

func downloadFromURLs(ctx context.Context, fetcher httpfetch.Client, log stepman.Logger, urls []string, hash, destPath string) error {
	var errs []error
	for _, url := range urls {
		err := fetcher.DownloadWithHash(ctx, destPath, url, hash)
		if err == nil {
			return nil
		}
		log.Warnf("Failed to download step executable from %s: %s", url, err)
		errs = append(errs, fmt.Errorf("%s: %w", url, err))
	}
	return fmt.Errorf("failed to download executable: %w", errors.Join(errs...))
}
