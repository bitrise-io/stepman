package steplib

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary"
	"github.com/bitrise-io/stepman/stepman"
)

// PrecompiledStepsEnv enables the precompiled-binary download path.
const PrecompiledStepsEnv = "BITRISE_EXPERIMENT_PRECOMPILED_STEPS"

// PrecompiledStepsEnabled reports whether the precompiled-binary download path
// is enabled.
func PrecompiledStepsEnabled() bool {
	return os.Getenv(PrecompiledStepsEnv) == "true" || os.Getenv(PrecompiledStepsEnv) == "1"
}

const precompiledStepsStorageURLsEnv = "BITRISE_PRECOMPILED_STEPS_STORAGE_URLS"

var precompiledStepsDefaultStorageURLs = []string{
	"https://steplib.bitrise.io",
	"https://storage.googleapis.com/bitrise-steplib-storage",
}

type APIActivatedStep struct {
	StepInfo       models.StepInfoModel
	StepYMLPath    string
	ExecutablePath string
}

func ActivateStep(stepLibURI string, id, version, destination, destinationStepYML string, log stepman.Logger, isOfflineMode bool) (string, error) {
	stepCollection, err := stepman.ReadStepSpec(stepLibURI)
	if err != nil {
		return "", fmt.Errorf("failed to read %s steplib: %s", stepLibURI, err)
	}

	step, version, err := queryStepMetadata(stepCollection, stepLibURI, id, version)
	if err != nil {
		return "", fmt.Errorf("failed to find step: %s", err)
	}

	if PrecompiledStepsEnabled() && step.Executables != nil {
		platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
		executableForPlatform, ok := (*step.Executables)[platform]
		if ok && executableForPlatform.Hash != "" && executableForPlatform.StorageURI != "" {
			log.Debugf("Downloading executable for %s", platform)
			downloadStart := time.Now()
			execPath, err := activateStepExecutable(stepLibURI, id, version, executableForPlatform, destination, destinationStepYML, true)
			if err == nil {
				log.Debugf("Downloaded executable in %s", time.Since(downloadStart).Round(time.Millisecond))
				return execPath, nil
			}
			log.Warnf("Failed to download step executable, fallback to step source activation: %s", err)
		}
		log.Infof("No prebuilt executable found for %s, fallback to step source activation", platform)
	}

	err = activateStepSource(stepCollection, stepLibURI, id, version, step, destination, destinationStepYML, log, isOfflineMode)
	if err != nil {
		return "", err
	}

	return "", nil
}

func ActivateStepWithAPI(stepLibURI string, apiClient steplibrary.Client, id, version, destination, destinationStepYML string, log stepman.Logger, isOfflineMode bool) (APIActivatedStep, error) {
	inventoryResult, err := apiClient.Activate(context.Background(), id, version, steplibrary.ActivateOutputPaths{
		YMLPath: destinationStepYML,
	})
	if err != nil {
		return APIActivatedStep{}, fmt.Errorf("Step inventory API: %w", err)
	}

	step := inventoryResult.StepInfo.Step
	if PrecompiledStepsEnabled() && step.Executables != nil {
		platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
		executableForPlatform, ok := (*step.Executables)[platform]
		if ok && executableForPlatform.Hash != "" && executableForPlatform.StorageURI != "" {
			log.Debugf("Downloading executable for %s", platform)
			downloadStart := time.Now()
			execPath, err := activateStepExecutable(stepLibURI, id, inventoryResult.StepInfo.Version, executableForPlatform, destination, destinationStepYML, false)
			if err == nil {
				log.Debugf("Downloaded executable in %s", time.Since(downloadStart).Round(time.Millisecond))
				return APIActivatedStep{
					StepInfo:       inventoryResult.StepInfo,
					StepYMLPath:    destinationStepYML,
					ExecutablePath: execPath,
				}, nil
			}
			log.Warnf("Failed to download step executable, fallback to step source activation: %s", err)
		}
		log.Infof("No prebuilt executable found for %s, fallback to step source activation", platform)
	}

	return APIActivatedStep{}, fmt.Errorf("no prebuilt executable found via API")
}

func queryStepMetadata(stepLib models.StepCollectionModel, stepLibURI string, id, version string) (models.StepModel, string, error) {
	step, stepFound, versionFound := stepLib.GetStep(id, version)

	if !stepFound {
		return models.StepModel{}, "", fmt.Errorf("%s steplib does not contain %s step", stepLibURI, id)
	}
	if !versionFound {
		return models.StepModel{}, "", fmt.Errorf("%s steplib does not contain %s step %s version", stepLibURI, id, version)
	}

	if version == "" {
		latest, err := stepLib.GetLatestStepVersion(id)
		if err != nil {
			return models.StepModel{}, "", fmt.Errorf("failed to find latest version of %s step", id)
		}
		version = latest
	}

	return step, version, nil
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
