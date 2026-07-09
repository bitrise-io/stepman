package steplib

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/stepman"
)

// ErrStepSourceNotCached is returned (wrapped) on the V2 source path in offline
// mode when the step source is not in the local cache.
var ErrStepSourceNotCached = errors.New("step source not available in the local cache and offline mode is set")

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
