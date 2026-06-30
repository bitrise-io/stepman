package activator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/pointers"
	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/activator/steplib"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepid"
	"github.com/bitrise-io/stepman/steplibrary"
	"github.com/bitrise-io/stepman/stepman"
)

const (
	bitriseV1SteplibURL = "https://github.com/bitrise-io/bitrise-steplib.git"
	// bitriseSteplibAPIURL is V2 Steplib API
	bitriseSteplibAPIURL           = "https://steplib.bitrise.io"
	steplibAPIURLOverrideEnv       = "BITRISE_EXPERIMENT_STEPLIB_API_URL_OVERRIDE"
	shouldMigrateV1SteplibToAPIEnv = "BITRISE_EXPERIMENT_STEPLIB_API_ENABLE_MIGRATE"
)

func ActivateSteplibRefStep(
	log stepman.Logger,
	id stepid.CanonicalID,
	activatedStepDir string,
	workDir string,
	didStepLibUpdateInWorkflow bool,
	isOfflineMode bool,
) (ActivatedStep, error) {
	stepYMLPath := filepath.Join(workDir, "current_step.yml")
	//nolint:exhaustruct // missing fields are added down below based on activation result
	activationResult := ActivatedStep{
		StepYMLPath:      stepYMLPath,
		DidStepLibUpdate: false,
	}
	libraryAPI := inventoryAPIClientFactory(id.SteplibSource, log)

	var legacyStepVersion string
	if libraryAPI == nil {
		// Old stepman preparation codepath
		stepInfo, didUpdate, err := prepareStepLibForActivation(log, id, didStepLibUpdateInWorkflow, isOfflineMode)
		activationResult.StepInfo = stepInfo
		activationResult.DidStepLibUpdate = didUpdate
		if err != nil {
			return activationResult, err
		}
		legacyStepVersion = stepInfo.Version
	}

	// ActivateStep dispatches to the v2 or legacy codepath. For the legacy path
	// we pass the already-resolved version so it isn't resolved a second time.
	resolvedStep, err := steplib.ActivateStep(id, activatedStepDir, stepYMLPath, log, isOfflineMode, libraryAPI, legacyStepVersion)
	if libraryAPI != nil {
		activationResult.StepInfo = resolvedStep.StepInfo
	}
	activationResult.ExecutablePath = resolvedStep.ExecPath
	if resolvedStep.ExecPath != "" {
		activationResult.ActivationType = ActivationTypeSteplibExecutable
	} else {
		activationResult.ActivationType = ActivationTypeSteplibSource
	}
	if err != nil {
		return activationResult, err
	}

	return activationResult, nil
}

// inventoryAPIClientFactory builds a Steplib API client when the V2 read path
// should be used, or returns nil to keep the legacy (V1) activation path.
func inventoryAPIClientFactory(steplibURI string, logger stepman.Logger) (client *steplibrary.Client) {
	shouldMigrate := os.Getenv(shouldMigrateV1SteplibToAPIEnv) == "true" || os.Getenv(shouldMigrateV1SteplibToAPIEnv) == "1"
	APIURL := os.Getenv(steplibAPIURLOverrideEnv)
	if strings.TrimSpace(APIURL) == "" {
		APIURL = bitriseSteplibAPIURL
	}
	if shouldMigrate && steplibURI == bitriseV1SteplibURL {
		return steplibrary.New(logger, APIURL, fileutil.NewFileManager())
	}
	return nil
}

func prepareStepLibForActivation(
	log stepman.Logger,
	id stepid.CanonicalID,
	didStepLibUpdateInWorkflow bool,
	isOfflineMode bool,
) (stepInfo models.StepInfoModel, didUpdate bool, err error) {
	err = stepman.SetupLibrary(id.SteplibSource, log)
	if err != nil {
		return models.StepInfoModel{}, false, fmt.Errorf("setup %s: %s", id.SteplibSource, err)
	}

	versionConstraint, err := models.ParseRequiredVersion(id.Version)
	if err != nil {
		return models.StepInfoModel{}, false, err
	}
	if versionConstraint.VersionLockType == models.InvalidVersionConstraint {
		return models.StepInfoModel{}, false, fmt.Errorf("version constraint is invalid: %s %s", id.IDorURI, id.Version)
	}

	if shouldUpdateStepLibForStep(versionConstraint, isOfflineMode, didStepLibUpdateInWorkflow) {
		log.Infof("Step uses latest version, updating StepLib...")
		_, err = stepman.UpdateLibrary(id.SteplibSource, log)
		if err != nil {
			log.Warnf("Step version constraint is latest or version locked, but failed to update StepLib, err: %s", err)
		} else {
			didUpdate = true
		}
	}

	stepInfo, err = stepman.QueryStepInfoFromLibrary(id.SteplibSource, id.IDorURI, id.Version, log)
	if err != nil {
		if !canUpdateStepLib(isOfflineMode, didStepLibUpdateInWorkflow) {
			return stepInfo, didUpdate, err
		}

		log.Infof("Step not found in local StepLib cache, trying to update StepLib...")
		_, err = stepman.UpdateLibrary(id.SteplibSource, log)
		if err != nil {
			return stepInfo, didUpdate, err
		} else {
			didUpdate = true
		}

		stepInfo, err = stepman.QueryStepInfoFromLibrary(id.SteplibSource, id.IDorURI, id.Version, log)
		if err != nil {
			return stepInfo, didUpdate, err
		}
	}

	if stepInfo.Step.Title == nil || *stepInfo.Step.Title == "" {
		stepInfo.Step.Title = pointers.NewStringPtr(stepInfo.ID)
	}
	stepInfo.OriginalVersion = id.Version

	return stepInfo, didUpdate, nil
}

func shouldUpdateStepLibForStep(constraint models.VersionConstraint, isOfflineMode bool, didStepLibUpdateInWorkflow bool) bool {
	if !canUpdateStepLib(isOfflineMode, didStepLibUpdateInWorkflow) {
		return false
	}

	return (constraint.VersionLockType == models.Latest) ||
		(constraint.VersionLockType == models.MinorLocked) ||
		(constraint.VersionLockType == models.MajorLocked)
}

func canUpdateStepLib(isOfflineMode bool, didStepLibUpdateInWorkflow bool) bool {
	if isOfflineMode {
		return false
	}

	if didStepLibUpdateInWorkflow {
		return false
	}

	return true
}
