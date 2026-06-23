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
	bitriseSteplibAPIURL        = "https://steplib.bitrise.io"
	shouldMigrateV1SteplibToAPI = "BITRISE_EXPERIMENT_STEPLIB_MIGRATE_V1_TO_API"
)

func ActivateSteplibRefStep(
	log stepman.Logger,
	id stepid.CanonicalID,
	activatedStepDir string,
	workDir string,
	didStepLibUpdateInWorkflow bool,
	isOfflineMode bool,
	stepInfoPtr *models.StepInfoModel,
) (ActivatedStep, error) {
	stepYMLPath := filepath.Join(workDir, "current_step.yml")
	//nolint:exhaustruct // missing fields are added down below based on activation result
	activationResult := ActivatedStep{
		StepYMLPath:      stepYMLPath,
		DidStepLibUpdate: false,
	}

	steplibURI := id.SteplibSource
	inventoryAPIClient := inventoryAPIClientFactory(steplibURI, log)

	stepInfo := models.StepInfoModel{}
	if inventoryAPIClient == nil {
		err := error(nil)
		stepInfo, activationResult.DidStepLibUpdate, err = prepareStepLibForActivation(log, id, didStepLibUpdateInWorkflow, isOfflineMode)
		if err != nil {
			return activationResult, err
		}

		execPath, err := steplib.ActivateStep(id.SteplibSource, id.IDorURI, stepInfo.Version, activatedStepDir, stepYMLPath, log, isOfflineMode)

		activationResult.ExecutablePath = execPath
		if execPath != "" {
			activationResult.ActivationType = ActivationTypeSteplibExecutable
		} else {
			activationResult.ActivationType = ActivationTypeSteplibSource
		}
		if err != nil {
			return activationResult, err
		}
	} else {
		activatedStep, err := steplib.ActivateStepWithAPI(id.SteplibSource, *inventoryAPIClient, id.IDorURI, id.Version, activatedStepDir, stepYMLPath, log, isOfflineMode)
		if err != nil {
			// todo: fallback
			return activationResult, err
		}
		stepInfo = activatedStep.StepInfo
		activationResult.ActivationType = ActivationTypeSteplibExecutable
		activationResult.ExecutablePath = activatedStep.ExecutablePath
	}

	// TODO: this is sketchy, we should clean this up, but this pointer originates in the CLI codebase
	stepInfoPtr.ID = stepInfo.ID
	if stepInfoPtr.Step.Title == nil || *stepInfoPtr.Step.Title == "" {
		stepInfoPtr.Step.Title = pointers.NewStringPtr(stepInfo.ID)
	}
	stepInfoPtr.Version = stepInfo.Version
	stepInfoPtr.LatestVersion = stepInfo.LatestVersion
	stepInfoPtr.OriginalVersion = stepInfo.OriginalVersion
	stepInfoPtr.GroupInfo = stepInfo.GroupInfo

	return activationResult, nil
}

// inventoryAPIClientFactory builds a Steplib API client when the V2 read path
// should be used, or returns nil to keep the legacy (V1) activation path.
func inventoryAPIClientFactory(steplibURI string, logger stepman.Logger) (client *steplibrary.Client) {
	inventoryURL, useAPI := determineSteplibEndpoint(steplibURI)
	if !useAPI {
		return nil
	}
	return steplibrary.New(logger, "", inventoryURL, fileutil.NewFileManager())
}

// determineSteplibEndpoint decides if the Steplib API (V2) is in use and which
// URL the inventory should be fetched from.
func determineSteplibEndpoint(steplibURI string) (endpoint string, useV2 bool) {
	switch {
	case steplibURI == bitriseV1SteplibURL: // Bitrise steplib
		shouldMigrate := os.Getenv(shouldMigrateV1SteplibToAPI) == "true" || os.Getenv(shouldMigrateV1SteplibToAPI) == "1"
		if shouldMigrate {
			endpoint, useV2 = bitriseSteplibAPIURL, true
		} else {
			endpoint, useV2 = steplibURI, false
		}
	case strings.HasSuffix(steplibURI, ".git"): // 3rd party V1 steplib
		endpoint, useV2 = steplibURI, false
	default: // we have an explicit API (V2) URL
		endpoint, useV2 = steplibURI, true
	}

	// The V2 read path only writes the step.yml; the runnable step comes from
	// the precompiled-binary download, so V2 is only usable when that path is
	// enabled. Fall back to V1 otherwise.
	if useV2 && !steplib.PrecompiledStepsEnabled() {
		return steplibURI, false
	}
	return endpoint, useV2
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
