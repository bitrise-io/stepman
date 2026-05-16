package steplibrary

import (
	"errors"
	"fmt"
	"slices"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/activator/result"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
)

type Steplib struct {
	log           stepman.Logger
	steplibURI    string
	isOfflineMode bool
	api           API
	fileManager   fileutil.FileManager
}

type ActivateOutputPaths struct {
	YMLPath, CodePath string
}

func New(log stepman.Logger, steplibURI string, isOfflineMode bool, fileManager fileutil.FileManager) *Steplib {
	return &Steplib{
		log:           log,
		steplibURI:    steplibURI,
		isOfflineMode: isOfflineMode,
		api:           MockAPI{},
		fileManager:   fileManager,
	}
}

func (s *Steplib) Activate(stepID, version string, outputPaths ActivateOutputPaths) (result.ActivatedStep, error) {
	stepInfo, resolved, err := s.getStepVersionInfo(stepID, version)

	var sourceYMLPath, stepSourceZIPPath, execPath string
	if err == nil {
		sourceYMLPath, err = s.api.GetStepYMLPath(resolved)
	}
	// ToDo: precompiled binary
	if err == nil {
		stepSourceZIPPath, err = s.api.GetStepSourceZIPPath(resolved)
	}
	if err == nil {
		if err = command.UnZIP(stepSourceZIPPath, outputPaths.CodePath); err != nil {
			err = fmt.Errorf("unzip step source %s: %w", stepSourceZIPPath, err)
		}
	}
	if err == nil {
		err = s.fileManager.CopyFile(sourceYMLPath, outputPaths.YMLPath, &fileutil.CopyOptions{Overwrite: true})
	}
	if err != nil {
		return result.ActivatedStep{}, err
	}

	activationType := result.ActivationTypeSteplibSource
	if execPath != "" {
		activationType = result.ActivationTypeSteplibExecutable
	}
	return result.ActivatedStep{
		StepInfo:         stepInfo,
		StepYMLPath:      outputPaths.YMLPath,
		ExecutablePath:   execPath,
		ActivationType:   activationType,
		DidStepLibUpdate: false, // deprecated
	}, nil
}

func (s *Steplib) getStepVersionInfo(stepID, version string) (models.StepInfoModel, ResolvedStepVersion, error) {
	var err error
	if stepID == "" {
		err = errors.New("missing required input: step id")
	}

	var allSteps []string
	if err == nil {
		allSteps, err = s.api.GetAllStepIDs()
		if err != nil {
			err = fmt.Errorf("fetching avaialble step IDs: %w", err)
		}
	}
	if err == nil && !slices.Contains(allSteps, stepID) {
		err = fmt.Errorf("%s steplib does not contain %s step", s.steplibURI, stepID)
	}

	var versionConstraint models.VersionConstraint
	if err == nil {
		versionConstraint, err = models.ParseRequiredVersion(version)
		if err != nil {
			err = fmt.Errorf("invalid step `%s` version constraint: %w", stepID, err)
		}
	}
	if err == nil && versionConstraint.VersionLockType == models.InvalidVersionConstraint {
		err = fmt.Errorf("invalid step `%s` version constraint: %s", stepID, version)
	}

	var latestVersions StepVersionsLatest
	if err == nil {
		latestVersions, err = s.api.GetLatestStepVersions(stepID)
		if err != nil {
			err = fmt.Errorf("fetching latest versions of `%s`: %w", stepID, err)
		}
	}

	var resolvedVersion string
	if err == nil {
		switch versionConstraint.VersionLockType {
		case models.Latest:
			resolvedVersion = latestVersions.Latest
		case models.Fixed:
			resolvedVersion = versionConstraint.Version.String()
			// ToDo: check version exists, otherwise error:
			// "%s steplib does not contain %s step %s version"
		case models.MajorLocked, models.MinorLocked:
			err = fmt.Errorf("version constraint %q not yet supported in steplib v2", version)
		default:
			err = fmt.Errorf("unknown version constraint: %s", version)
		}
	}

	if err != nil {
		return models.StepInfoModel{}, ResolvedStepVersion{}, err
	}
	//nolint:exhaustruct // GroupInfo, Step and DefinitionPth aren't surfaced by the v2 API yet
	return models.StepInfoModel{
		Library:         s.steplibURI,
		ID:              stepID,
		Version:         resolvedVersion,
		OriginalVersion: version,
		LatestVersion:   latestVersions.Latest,
	}, ResolvedStepVersion{ID: stepID, Version: resolvedVersion}, nil
}
