package steplib

import (
	"context"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary"
	"github.com/bitrise-io/stepman/stepman"
)

// ErrStepSourceNotCached is returned (wrapped) on the V2 source path in offline
// mode when the step source is not in the local cache.
var ErrStepSourceNotCached = errors.New("step source not available in the local cache and offline mode is set")

// activateStepSourceV2 materializes id@version's source into destDir over the
// steplib API, without setting up the local steplib. It reuses the V1 on-disk
// cache when present; otherwise it downloads from the inventory's locations (zip
// fast path, git fallback). The inventory locations are fetched only on the
// download path, so offline mode and a cache hit do no network. Offline with no
// cache returns ErrStepSourceNotCached.
func activateStepSourceV2(libraryAPI *steplibrary.Client, uri, id, version string, source *models.StepSourceModel, destDir string, log stepman.Logger, isOfflineMode bool) error {
	if reused, err := reuseCachedStepSource(uri, id, version, destDir); err != nil {
		return err
	} else if reused {
		return nil
	}

	if isOfflineMode {
		return fmt.Errorf("%s@%s: %w", id, version, ErrStepSourceNotCached)
	}

	if source == nil || source.Git == "" {
		return fmt.Errorf("step %s@%s has no source git URL to download from", id, version)
	}

	locations, err := libraryAPI.StepDownloadLocations(context.Background(), id, version, source.Git)
	if err != nil {
		return fmt.Errorf("resolve download locations for %s@%s: %s", id, version, err)
	}
	if len(locations) == 0 {
		return fmt.Errorf("step %s@%s has no download location", id, version)
	}

	if err := stepman.DownloadStepArchive(destDir, locations, id, version, source.Commit, log); err != nil {
		return fmt.Errorf("download step source %s@%s: %s", id, version, err)
	}
	return nil
}

// reuseCachedStepSource copies the V1 on-disk cache for id@version into destDir
// when it is populated, reporting whether it did. This is the only steplib
// lookup on the V2 path and is optional: a missing route just means "not cached".
func reuseCachedStepSource(uri, id, version, destDir string) (bool, error) {
	route, found := stepman.ReadRoute(uri)
	if !found {
		return false, nil
	}
	cacheDir := stepman.GetStepCacheDirPath(route, id, version)
	exists, err := pathutil.IsPathExists(cacheDir)
	if err != nil {
		return false, fmt.Errorf("check if %s exists: %s", cacheDir, err)
	}
	if !exists {
		return false, nil
	}
	return true, copyStep(cacheDir, destDir)
}
