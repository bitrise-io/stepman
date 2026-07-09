package steplib

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
)

// ErrStepSourceNotCached is returned by activateStepSourceFromModel in offline
// mode when the step source is not in the local cache. Match it with errors.Is.
var ErrStepSourceNotCached = errors.New("step source not available in the local cache and offline mode is set")

// activateStepSourceFromModel materializes id@version's source into destDir
// without setting up the local steplib (no ReadStepSpec/SetupLibrary). It reuses
// the V1 on-disk cache when already populated; otherwise it downloads from the
// locations resolveLocations returns (zip fast path + git fallback, as built
// from the V2 API), verifying commit for the git clone. resolveLocations is
// called lazily so offline mode does no network. Offline with no cache returns
// ErrStepSourceNotCached.
func activateStepSourceFromModel(uri, id, version, commit string, resolveLocations func() ([]models.DownloadLocationModel, error), destDir string, log stepman.Logger, isOfflineMode bool) error {
	// Reuse the V1 cache when populated. This is the only steplib lookup, and it
	// is optional: a missing route just means "not cached".
	if route, found := stepman.ReadRoute(uri); found {
		cacheDir := stepman.GetStepCacheDirPath(route, id, version)
		if exists, err := pathutil.IsPathExists(cacheDir); err != nil {
			return fmt.Errorf("check if %s exists: %s", cacheDir, err)
		} else if exists {
			return copyStep(cacheDir, destDir)
		}
	}

	if isOfflineMode {
		return fmt.Errorf("%s@%s: %w", id, version, ErrStepSourceNotCached)
	}

	locations, err := resolveLocations()
	if err != nil {
		return fmt.Errorf("resolve download locations for %s@%s: %s", id, version, err)
	}
	if len(locations) == 0 {
		return fmt.Errorf("step %s@%s has no download location", id, version)
	}

	if err := stepman.DownloadStepArchive(destDir, locations, id, version, commit, log); err != nil {
		return fmt.Errorf("download step source %s@%s: %s", id, version, err)
	}
	return nil
}
