package steplib

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/pointers"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepid"
	"github.com/bitrise-io/stepman/stepman"
)

const precompiledStepsEnv = "BITRISE_EXPERIMENT_PRECOMPILED_STEPS"
const precompiledStepsStorageURLsEnv = "BITRISE_PRECOMPILED_STEPS_STORAGE_URLS"

var precompiledStepsDefaultStorageURLs = []string{
	"https://steplib.bitrise.io",
	"https://storage.googleapis.com/bitrise-steplib-storage",
}

// ResolvedStep is the result of resolving and activating a steplib step.
type ResolvedStep struct {
	// ExecPath is the activated step executable. Optional: set only for
	// precompiled executable activation, empty for source activation.
	ExecPath string
	// StepInfo is the resolved step metadata (concrete version and step model).
	StepInfo models.StepInfoModel
	// DidStepLibUpdate reports whether the local steplib cache was updated during resolution.
	DidStepLibUpdate bool
}

// NewResolvedStep builds a ResolvedStep. execPath is optional (empty for source activation).
func NewResolvedStep(execPath string, stepInfo models.StepInfoModel, didStepLibUpdate bool) ResolvedStep {
	return ResolvedStep{
		ExecPath:         execPath,
		StepInfo:         stepInfo,
		DidStepLibUpdate: didStepLibUpdate,
	}
}

// ActivateStep resolves the requested step (setting up and, if needed, updating
// the local steplib cache) and activates it into destination. On error the
// returned ResolvedStep still carries the resolution state gathered so far.
func ActivateStep(id stepid.CanonicalID, destination, destinationStepYML string, log stepman.Logger, didStepLibUpdateInWorkflow, isOfflineMode bool) (ResolvedStep, error) {
	stepInfo, didUpdate, err := prepareStepLibForActivation(log, id, didStepLibUpdateInWorkflow, isOfflineMode)
	if err != nil {
		return NewResolvedStep("", stepInfo, didUpdate), err
	}

	step := stepInfo.Step
	version := stepInfo.Version

	execPath, err := downloadPrecompiled(log, step, id.IDorURI, destination)
	if err != nil {
		return NewResolvedStep("", stepInfo, didUpdate), err
	}
	if execPath != "" {
		if err := copyStepYML(id.SteplibSource, id.IDorURI, version, destinationStepYML); err != nil {
			return NewResolvedStep("", stepInfo, didUpdate), fmt.Errorf("copy step.yml: %s", err)
		}

		return NewResolvedStep(execPath, stepInfo, didUpdate), nil
	}

	stepCollection, err := stepman.ReadStepSpec(id.SteplibSource)
	if err != nil {
		return NewResolvedStep("", stepInfo, didUpdate), fmt.Errorf("failed to read %s steplib: %s", id.SteplibSource, err)
	}

	if err := activateStepSource(stepCollection, id.SteplibSource, id.IDorURI, version, step, destination, destinationStepYML, log, isOfflineMode); err != nil {
		return NewResolvedStep("", stepInfo, didUpdate), err
	}

	return NewResolvedStep("", stepInfo, didUpdate), nil
}

func downloadPrecompiled(log stepman.Logger, step models.StepModel, id string, destination string) (string, error) {
	if (os.Getenv(precompiledStepsEnv) == "true" || os.Getenv(precompiledStepsEnv) == "1") && step.Executables != nil {
		platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
		executableForPlatform, ok := (*step.Executables)[platform]
		if ok && executableForPlatform.Hash != "" && executableForPlatform.StorageURI != "" {
			log.Debugf("Downloading executable for %s", platform)
			downloadStart := time.Now()
			execPath, err := activateStepExecutable(id, executableForPlatform, destination)
			if err == nil {
				log.Debugf("Downloaded executable in %s", time.Since(downloadStart).Round(time.Millisecond))

				return execPath, nil
			}
			log.Warnf("Failed to download step executable, fallback to step source activation: %s", err)
		}
		log.Infof("No prebuilt executable found for %s, fallback to step source activation", platform)
	}
	return "", nil
}

func copyStepYML(libraryURL, id, version, dest string) error {
	route, found := stepman.ReadRoute(libraryURL)
	if !found {
		return fmt.Errorf("no route found for %s steplib", libraryURL)
	}

	if exist, err := pathutil.IsPathExists(dest); err != nil {
		return fmt.Errorf("failed to check if %s path exist: %s", dest, err)
	} else if exist {
		return fmt.Errorf("%s already exist", dest)
	}

	stepCollectionDir := stepman.GetStepCollectionDirPath(route, id, version)
	stepYMLSrc := filepath.Join(stepCollectionDir, "step.yml")
	if err := command.CopyFile(stepYMLSrc, dest); err != nil {
		return fmt.Errorf("copy command failed: %s", err)
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

func ListCachedStepVersions(log stepman.Logger, stepLib models.StepCollectionModel, stepLibURI, stepID string) []string {
	versions := []models.Semver{}

	route, found := stepman.ReadRoute(stepLibURI)
	if !found {
		return nil
	}

	for version := range stepLib.Steps[stepID].Versions {
		stepCacheDir := stepman.GetStepCacheDirPath(route, stepID, version)
		_, err := os.Stat(stepCacheDir)
		if err != nil {
			continue
		}

		v, err := models.ParseSemver(version)
		if err != nil {
			log.Warnf("failed to parse version (%s): %s", version, err)
		}

		versions = append(versions, v)
	}

	slices.SortFunc(versions, models.CmpSemver)

	versionsStr := make([]string, len(versions))
	for i, v := range versions {
		versionsStr[i] = v.String()
	}

	return versionsStr
}
