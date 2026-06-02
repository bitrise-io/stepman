package steplib

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/stepman/internal/sri"
	"github.com/bitrise-io/stepman/models"
	"github.com/hashicorp/go-retryablehttp"
)

func activateStepExecutable(
	stepLibURI string,
	stepID string,
	version string,
	executable models.Executable,
	destinationDir string,
	destinationStepYML string,
) (string, error) {
	body, err := downloadExecutable(executable)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := body.Close(); err != nil {
			log.Warnf("Failed to close response body: %s\n", err)
		}
	}()

	err = os.MkdirAll(destinationDir, 0755)
	if err != nil {
		return "", fmt.Errorf("create directory %s: %w", destinationDir, err)
	}

	path := filepath.Join(destinationDir, stepID)
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create file %s: %w", path, err)
	}
	defer func() {
		err := file.Close()
		if err != nil {
			log.Warnf("Failed to close file %s: %s\n", path, err)
		}
	}()

	_, err = io.Copy(file, body)
	if err != nil {
		return "", fmt.Errorf("download to %s: %w", path, err)
	}

	err = validateHash(path, executable.Hash)
	if err != nil {
		return "", fmt.Errorf("validate hash: %s", err)
	}

	err = os.Chmod(path, 0755)
	if err != nil {
		return "", fmt.Errorf("set executable permission on file: %s", err)
	}

	if err := copyStepYML(stepLibURI, stepID, version, destinationStepYML); err != nil {
		return "", fmt.Errorf("copy step.yml: %s", err)
	}

	return path, nil
}

func validateHash(filePath string, expectedHash string) (err error) {
	if expectedHash == "" {
		return fmt.Errorf("hash is empty")
	}

	if !strings.HasPrefix(expectedHash, sri.Prefix) {
		return fmt.Errorf("only SHA256 hashes supported at this time, make sure to prefix the hash with `sha256-`. Found hash value: %s", expectedHash)
	}

	reader, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := reader.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	actualHash, err := sri.SHA256Reader(reader)
	if err != nil {
		return fmt.Errorf("calculate hash: %w", err)
	}
	if actualHash != expectedHash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	return nil
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

func downloadExecutable(executable models.Executable) (io.ReadCloser, error) {
	bases := PrecompiledStepsDefaultStorageURLs
	if override := os.Getenv(PrecompiledStepsStorageURLsEnv); override != "" {
		bases = strings.Split(override, ",")
	}

	urls, err := buildDownloadURLs(bases, executable)
	if err != nil {
		return nil, err
	}
	return downloadFromURLs(urls)
}

func downloadFromURLs(urls []string) (io.ReadCloser, error) {
	var errs []error
	for _, url := range urls {
		resp, err := retryablehttp.Get(url)
		if err == nil && resp.StatusCode < 400 {
			return resp.Body, nil
		}

		if err != nil {
			log.Warnf("Failed to download step from %s: %s\n", url, err)
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
		} else {
			if closeErr := resp.Body.Close(); closeErr != nil {
				log.Warnf("Failed to close response body: %s\n", closeErr)
			}
			log.Warnf("Storage returned status %d for %s\n", resp.StatusCode, url)
			errs = append(errs, fmt.Errorf("%s: status %d", url, resp.StatusCode))
		}
	}
	return nil, fmt.Errorf("failed to download executable: %w", errors.Join(errs...))
}
