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

func activateStepSource(
	stepLib models.StepCollectionModel,
	stepLibURI, id, version string,
	destination string,
	stepYMLDestination string,
	log stepman.Logger,
	isOfflineMode bool,
) error {
	stepCacheDir, err := stepman.GetStepSourceDir(stepLibURI, id, version, log, isOfflineMode)
	if errors.Is(err, stepman.ErrStepSourceNotCached) {
		return fmt.Errorf("download step: %s", collectOfflineAvailableStepVersions(stepLib, stepLibURI, id, log))
	}
	if err != nil {
		return err
	}

	if err := copyStep(stepCacheDir, destination); err != nil {
		return fmt.Errorf("copy step failed: %s", err)
	}

	if err := copyStepYML(stepLibURI, id, version, stepYMLDestination); err != nil {
		return fmt.Errorf("copy step.yml failed: %s", err)
	}

	return nil
}

func copyStep(src, dst string) error {
	if exist, err := pathutil.IsPathExists(dst); err != nil {
		return fmt.Errorf("failed to check if %s path exist: %s", dst, err)
	} else if !exist {
		if err := os.MkdirAll(dst, 0777); err != nil {
			return fmt.Errorf("failed to create dir for %s path: %s", dst, err)
		}
	}

	if err := command.CopyDir(src+"/", dst, true); err != nil {
		return fmt.Errorf("copy command failed: %s", err)
	}
	return nil
}

func collectOfflineAvailableStepVersions(stepLib models.StepCollectionModel, stepLibURI, id string, log stepman.Logger) string {
	availableVersions := ListCachedStepVersions(log, stepLib, stepLibURI, id)
	versionList := "Other versions available in the local cache:"
	for _, version := range availableVersions {
		versionList = versionList + fmt.Sprintf("\n- %s", version)
	}

	errMsg := fmt.Sprintf("version is not available in the local cache and $BITRISE_OFFLINE_MODE is set. %s", versionList)
	return errMsg
}
