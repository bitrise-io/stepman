package stepman

import (
	"errors"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/models"
)

// ErrStepSourceNotCached is returned by ActivateStepSourceFromModel in offline
// mode when the step source isn't already in the local cache. Callers may match
// it via errors.Is to build a richer message.
var ErrStepSourceNotCached = errors.New("step source not available in the local cache and offline mode is set")

// ActivateStepSourceFromModel materializes the source for id@version into destDir
// without requiring the local steplib to be set up (no ReadStepSpec/SetupLibrary).
// It reuses the V1 on-disk cache when it is already populated; otherwise it
// downloads the source from the passed step source (git URL + commit), which the
// caller obtained from the V2 API. In offline mode a missing local cache returns
// an error wrapping ErrStepSourceNotCached.
func ActivateStepSourceFromModel(uri, id, version string, source *models.StepSourceModel, destDir string, log Logger, isOfflineMode bool) error {
	// Reuse the V1 on-disk cache when it's already populated (e.g. a prior V1
	// activation, or a warm cache). This is the only place the local steplib is
	// consulted, and it's optional — a missing route just means "not cached".
	if route, found := ReadRoute(uri); found {
		cacheDir := GetStepCacheDirPath(route, id, version)
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
	if err := downloadStepArchive(destDir, locations, id, version, source.Commit, log); err != nil {
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
