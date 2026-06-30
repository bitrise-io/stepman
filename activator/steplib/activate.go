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
	"github.com/bitrise-io/stepman/stepid"
	"github.com/bitrise-io/stepman/steplibrary"
	"github.com/bitrise-io/stepman/stepman"
)

const precompiledStepsEnv = "BITRISE_EXPERIMENT_PRECOMPILED_STEPS"
const precompiledStepsStorageURLsEnv = "BITRISE_PRECOMPILED_STEPS_STORAGE_URLS"

var precompiledStepsDefaultStorageURLs = []string{
	"https://steplib.bitrise.io",
	"https://storage.googleapis.com/bitrise-steplib-storage",
}

type ResolvedStep struct {
	ExecPath string
	StepInfo models.StepInfoModel
}

func ActivateStep(id stepid.CanonicalID, destination, destinationStepYML string, log stepman.Logger, isOfflineMode bool, libraryAPI *steplibrary.Client) (ResolvedStep, error) {
	var stepModel models.StepModel
	var stepInfo models.StepInfoModel
	var version string
	var resolveErr error
	if libraryAPI != nil {
		stepInfo, resolveErr = resolveStepModel(*libraryAPI, id, log, destinationStepYML)
		stepModel = stepInfo.Step
		version = stepInfo.Version
	} else {
		stepModel, version, resolveErr = resolveStepModelLegacy(id)
	}
	if resolveErr != nil {
		return ResolvedStep{}, resolveErr
	}

	execPath, err := downloadPrecompiled(log, stepModel, id, destination)
	if execPath != "" {
		if libraryAPI == nil {
			if err := copyStepYML(id.SteplibSource, id.IDorURI, version, destinationStepYML); err != nil {
				return ResolvedStep{}, fmt.Errorf("copy step.yml: %s", err)
			}
		}

		return ResolvedStep{
			ExecPath: execPath,
			StepInfo: stepInfo,
		}, err
	}

	// Fallback path to step source activation
	// TODO: this is tied to the old stepman codepath because source activation needs a `stepCollection` object.
	// Might be a good cleanup in a follow-up PR, maybe source activation can be made independent of `stepCollection`
	// TODO: this assumes that the step library spec is already up-to-date.
	// This breaks when the new steplib API is NOT ENABLED and should be fixed in a follow-up PR. See steplib_ref.go.
	stepCollection, err := stepman.ReadStepSpec(id.SteplibSource)
	if err != nil {
		return ResolvedStep{}, fmt.Errorf("failed to read %s steplib: %s", id.SteplibSource, err)
	}
	if err := activateStepSource(stepCollection, id.SteplibSource, id.IDorURI, version, stepModel, destination, log, isOfflineMode); err != nil {
		return ResolvedStep{}, err
	}

	// The step.yml must be placed at destinationStepYML. On the legacy path it
	// comes from the local steplib cache; on the API path FetchStepMetadata has
	// already written it there, so copying again would fail ("already exist").
	if libraryAPI == nil {
		if err := copyStepYML(id.SteplibSource, id.IDorURI, version, destinationStepYML); err != nil {
			return ResolvedStep{}, fmt.Errorf("copy step.yml: %s", err)
		}
	}

	return ResolvedStep{StepInfo: stepInfo}, nil
}

func downloadPrecompiled(log stepman.Logger, step models.StepModel, id stepid.CanonicalID, destination string) (string, error) {
	if (os.Getenv(precompiledStepsEnv) == "true" || os.Getenv(precompiledStepsEnv) == "1") && step.Executables != nil {
		platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
		executableForPlatform, ok := (*step.Executables)[platform]
		if ok && executableForPlatform.Hash != "" && executableForPlatform.StorageURI != "" {
			log.Debugf("Downloading executable for %s", platform)
			downloadStart := time.Now()
			execPath, err := activateStepExecutable(id.IDorURI, executableForPlatform, destination)
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

func resolveStepModelLegacy(id stepid.CanonicalID) (models.StepModel, string, error) {
	stepCollection, err := stepman.ReadStepSpec(id.SteplibSource)
	if err != nil {
		return models.StepModel{}, "", fmt.Errorf("failed to read %s steplib: %s", id.SteplibSource, err)
	}

	step, version, err := queryStepMetadata(stepCollection, id.SteplibSource, id.IDorURI, id.Version)
	if err != nil {
		return models.StepModel{}, "", fmt.Errorf("failed to find step: %s", err)
	}
	return step, version, nil
}

func resolveStepModel(client steplibrary.Client, id stepid.CanonicalID, log stepman.Logger, outputYMLPath string) (models.StepInfoModel, error) {
	ctx := context.Background()
	activateResult, err := client.FetchStepMetadata(ctx, id, outputYMLPath)
	if err != nil {
		return models.StepInfoModel{}, fmt.Errorf("fetch step metadata: %s", err)
	}

	return activateResult.StepInfo, nil
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
