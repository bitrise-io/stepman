package stepman

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/pathutil"
)

// ErrStepSourceNotCached is returned by GetStepSourceDir in offline mode when
// the requested version isn't already in the local cache. Callers may match it
// via errors.Is to build a richer message.
var ErrStepSourceNotCached = errors.New("step source not available in the local cache and offline mode is set")

// GetStepSourceDir returns the local cache directory holding the extracted
// source for id@version of the steplib at uri, downloading it via DownloadStep
// (zip+git fallback, retries, commit verification) if absent. The cache is
// immutable per version, so an existing dir is returned as-is. In offline mode
// a missing version returns an error wrapping ErrStepSourceNotCached.
func GetStepSourceDir(uri, id, version string, log Logger, isOfflineMode bool) (string, error) {
	route, found := ReadRoute(uri)
	if !found {
		return "", fmt.Errorf("no route found for %s steplib", uri)
	}
	cacheDir := GetStepCacheDirPath(route, id, version)

	exists, err := pathutil.IsPathExists(cacheDir)
	if err != nil {
		return "", fmt.Errorf("check if %s exists: %w", cacheDir, err)
	}
	if exists {
		return cacheDir, nil
	}

	if isOfflineMode {
		return "", fmt.Errorf("%s@%s: %w", id, version, ErrStepSourceNotCached)
	}

	collection, err := ReadStepSpec(uri)
	if err != nil {
		return "", fmt.Errorf("read steplib spec for %s: %w", uri, err)
	}
	step, stepFound, versionFound := collection.GetStep(id, version)
	if !stepFound || !versionFound {
		return "", fmt.Errorf("%s steplib does not contain %s@%s", uri, id, version)
	}
	commit := ""
	if step.Source != nil {
		commit = step.Source.Commit
	}

	if err := DownloadStep(uri, collection, id, version, commit, log); err != nil {
		return "", fmt.Errorf("download step %s@%s: %w", id, version, err)
	}
	return cacheDir, nil
}
