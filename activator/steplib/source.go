package steplib

import (
	"errors"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/command"
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
// given source (git URL + commit, as returned by the V2 API). Offline with no
// cache returns ErrStepSourceNotCached.
func activateStepSourceFromModel(uri, id, version string, source *models.StepSourceModel, destDir string, log stepman.Logger, isOfflineMode bool) error {
	// Reuse the V1 cache when populated. This is the only steplib lookup, and it
	// is optional: a missing route just means "not cached".
	if route, found := stepman.ReadRoute(uri); found {
		cacheDir := stepman.GetStepCacheDirPath(route, id, version)
		if exists, err := pathutil.IsPathExists(cacheDir); err != nil {
			return fmt.Errorf("check if %s exists: %s", cacheDir, err)
		} else if exists {
			return copyStepDir(cacheDir, destDir)
		}
	}

	if isOfflineMode {
		return fmt.Errorf("%s@%s: %w", id, version, ErrStepSourceNotCached)
	}

	if source == nil || source.Git == "" {
		return fmt.Errorf("step %s@%s has no source git URL to download from", id, version)
	}

	locations := []models.DownloadLocationModel{{Type: "git", Src: source.Git}}
	if err := stepman.DownloadStepArchive(destDir, locations, id, version, source.Commit, log); err != nil {
		return fmt.Errorf("download step source %s@%s: %s", id, version, err)
	}
	return nil
}

func copyStepDir(src, dst string) error {
	if exists, err := pathutil.IsPathExists(dst); err != nil {
		return fmt.Errorf("check if %s exists: %s", dst, err)
	} else if !exists {
		if err := os.MkdirAll(dst, 0777); err != nil {
			return fmt.Errorf("create dir %s: %s", dst, err)
		}
	}
	if err := command.CopyDir(src+"/", dst, true); err != nil {
		return fmt.Errorf("copy %s to %s: %s", src, dst, err)
	}
	return nil
}
