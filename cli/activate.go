package cli

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/bitrise-io/bitrise/toolkits"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
	"github.com/urfave/cli"
)

var errStepNotAvailableOfflineMode error = fmt.Errorf("step not available in offline mode")

var activateCommand = cli.Command{
	Name:  "activate",
	Usage: "Copy the step with specified --id, and --version, into provided path. If --version flag is not set, the latest version of the step will be used. If --copyyml flag is set, step.yml will be copied to the given path.",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   CollectionKey + ", " + collectionKeyShort,
			Usage:  "Collection of step.",
			EnvVar: CollectionPathEnvKey,
		},
		cli.StringFlag{
			Name:  IDKey + ", " + idKeyShort,
			Usage: "Step id.",
		},
		cli.StringFlag{
			Name:  VersionKey + ", " + versionKeyShort,
			Usage: "Step version.",
		},
		cli.StringFlag{
			Name:  PathKey + ", " + pathKeyShort,
			Usage: "Path where the step will copied.",
		},
		cli.StringFlag{
			Name:  CopyYMLKey + ", " + copyYMLKeyShort,
			Usage: "Path where the activated step's step.yml will be copied.",
		},
		cli.BoolFlag{
			Name:  UpdateKey + ", " + updateKeyShort,
			Usage: "If flag is set, and collection doesn't contains the specified step, the collection will updated.",
		},
	},
	Action: func(c *cli.Context) error {
		if err := activate(c); err != nil {
			failf("Command failed: %s", err)
		}
		return nil
	},
}

func activate(c *cli.Context) error {
	stepLibURI := c.String(CollectionKey)
	if stepLibURI == "" {
		return fmt.Errorf("no steplib specified")
	}

	id := c.String(IDKey)
	if id == "" {
		return fmt.Errorf("no step ID specified")
	}

	path := c.String(PathKey)
	if path == "" {
		return fmt.Errorf("no destination path specified")
	}

	version := c.String(VersionKey)
	copyYML := c.String(CopyYMLKey)
	update := c.Bool(UpdateKey)
	logger := log.NewDefaultLogger(false)
	isOfflineMode := false

	output, err := Activate(stepLibURI, id, version, path, copyYML, update, logger, isOfflineMode)
	if err != nil {
		return err
	}

	logger.Printf("%v+", output)

	return nil
}

// Activate ...
func Activate(stepLibURI, id, version, destinationDir, destinationStepYML string, updateLibrary bool, log stepman.Logger, isOfflineMode bool) (toolkits.ActivatedStep, error) {
	output := toolkits.ActivatedStep{}

	stepLib, err := stepman.ReadStepSpec(stepLibURI)
	if err != nil {
		return output, fmt.Errorf("failed to read %s steplib: %s", stepLibURI, err)
	}

	step, version, err := queryStep(stepLib, stepLibURI, id, version, updateLibrary, log)
	if err != nil {
		return output, fmt.Errorf("failed to find step: %s", err)
	}

	stepExecutor, err := activateStep(stepLib, stepLibURI, id, version, step, log, isOfflineMode, destinationDir)
	if err != nil {
		if err == errStepNotAvailableOfflineMode {
			availableVersions := listCachedStepVersion(log, stepLib, stepLibURI, id)
			versionList := "Other versions available in the local cache:"
			for _, version := range availableVersions {
				versionList = versionList + fmt.Sprintf("\n- %s", version)
			}

			errMsg := fmt.Sprintf("version is not available in the local cache and $BITRISE_BETA_OFFLINE_MODE is set. %s", versionList)
			return toolkits.ActivatedStep{}, fmt.Errorf("failed to download step: %s", errMsg)
		}

		return output, fmt.Errorf("failed to download step: %s", err)
	}

	if destinationStepYML != "" {
		if err := copyStepYML(stepLibURI, id, version, destinationStepYML); err != nil {
			return output, fmt.Errorf("copy step.yml failed: %s", err)
		}
	}

	return toolkits.ActivatedStep{
		StepExecutor: stepExecutor,
		StepYMLPath:  destinationStepYML,
	}, nil
}

func queryStep(stepLib models.StepCollectionModel, stepLibURI string, id, version string, updateLibrary bool, log stepman.Logger) (models.StepModel, string, error) {
	step, stepFound, versionFound := stepLib.GetStep(id, version)
	if (!stepFound || !versionFound) && updateLibrary {
		var err error
		stepLib, err = stepman.UpdateLibrary(stepLibURI, log)
		if err != nil {
			return models.StepModel{}, "", fmt.Errorf("failed to update %s steplib: %s", stepLibURI, err)
		}

		step, stepFound, versionFound = stepLib.GetStep(id, version)
	}
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

func getExecutableFromCache(executablePath, checkSumPath string) error {
	for _, path := range []string{executablePath, checkSumPath} {
		exist, err := pathutil.IsPathExists(path)
		if err != nil {
			return fmt.Errorf("failed to check if path (%s) exist: %w", path, err)
		}

		if !exist {
			return fmt.Errorf("executable not found in cache, path (%s) missing", path)
		}
	}

	return checkChecksum(executablePath, checkSumPath)
}

func activateStep(stepLib models.StepCollectionModel, stepLibURI, id, version string, step models.StepModel, log stepman.Logger, isOfflineMode bool, destinationDir string) (toolkits.StepExecutor, error) {
	route, found := stepman.ReadRoute(stepLibURI)
	if !found {
		return nil, fmt.Errorf("no route found for %s steplib", stepLibURI)
	}

	stepBinDir := stepman.GetStepBinDirPathForVersion(route, id, version)
	stepBinDirExist, err := pathutil.IsPathExists(stepBinDir)
	if err != nil {
		return nil, fmt.Errorf("failed to check if %s path exist: %s", stepBinDir, err)
	}
	executablePath := stepman.GetStepExecutablePathForVersion(route, id, version)
	if stepBinDirExist {
		// is precompiled uncompressed step version in cache?
		checkSumPath := stepman.GetStepExecutableChecksumPathForVersion(route, id, version)

		err := getExecutableFromCache(executablePath, checkSumPath)
		if err == nil {
			return toolkits.NewExecutableStepExecutor(executablePath), nil
		}
		log.Debugf("[Stepman] %s", err)

		// is precompiled binary patch in cache?
		fromPatchVersion := stepLib.Steps[id].LatestVersionNumber
		fromPatchExecutablePath := stepman.GetStepExecutablePathForVersion(route, id, fromPatchVersion)
		binaryPatchPath := stepman.GetStepCompressedExecutablePathForVersion(fromPatchVersion, route, id, version)

		err = uncompressStepFromCache(fromPatchExecutablePath, binaryPatchPath, executablePath, checkSumPath)
		if err == nil {
			return toolkits.NewExecutableStepExecutor(executablePath), nil
		}
		log.Debugf("[Stepman] %s", err)
	}

	stepCacheDir := stepman.GetStepCacheDirPath(route, id, version)
	if exist, err := pathutil.IsPathExists(stepCacheDir); err != nil {
		return nil, fmt.Errorf("failed to check if %s path exist: %s", stepCacheDir, err)
	} else if exist { // version specific source cache exists
		if isOfflineMode && destinationDir == "" {
			return nil, nil
		}
		return toolkits.NewSteplibStepExecutor(stepCacheDir, step, executablePath, destinationDir)
	}

	// version specific source cache not exists
	if isOfflineMode {
		return nil, errStepNotAvailableOfflineMode
	}

	if err := stepman.DownloadStep(stepLibURI, stepLib, id, version, step.Source.Commit, log); err != nil {
		return nil, fmt.Errorf("download failed: %s", err)
	}

	return toolkits.NewSteplibStepExecutor(stepCacheDir, step, executablePath, destinationDir)
}

func listCachedStepVersion(log stepman.Logger, stepLib models.StepCollectionModel, stepLibURI, stepID string) []string {
	versions := []models.Semver{}

	for version, step := range stepLib.Steps[stepID].Versions {
		_, err := activateStep(stepLib, stepLibURI, stepID, version, step, log, true, "")
		if err != nil {
			continue
		}

		v, err := models.ParseSemver(version)
		if err != nil {
			log.Warnf("failed to parse version (%s): %s", version, err)
		}

		versions = append(versions, v)
	}

	slices.SortFunc(versions, models.LessSemver)

	versionsStr := make([]string, len(versions))
	for i, v := range versions {
		versionsStr[i] = v.String()
	}

	return versionsStr
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
