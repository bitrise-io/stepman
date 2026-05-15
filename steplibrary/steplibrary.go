package steplibrary

import (
	"errors"
	"fmt"
	"slices"

	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
)

type Steplib struct {
	log           stepman.Logger
	steplibURI    string
	isOfflineMode bool
	api           API
}

// ActivatedStep is the steplib v2 activation result.
// Callers (e.g. the activator package) convert it to their own representation.
type ActivatedStep struct {
	StepInfo       models.StepInfoModel
	StepYMLPath    string
	ExecutablePath string
}

func New(log stepman.Logger, steplibURI string, isOfflineMode bool) *Steplib {
	return &Steplib{
		log:           log,
		steplibURI:    steplibURI,
		isOfflineMode: isOfflineMode,
		api:           MockAPI{},
	}
}

func (s *Steplib) Activate(stepID, version, targetYMLPath string) (ActivatedStep, error) {
	stepInfo, resolved, err := s.getStepVersionInfo(stepID, version)
	if err != nil {
		return ActivatedStep{}, err
	}

	ymlPath, err := s.api.GetStepYMLPath(resolved)
	if err != nil {
		return ActivatedStep{}, fmt.Errorf("fetching step.yml path for `%s@%s`: %w", resolved.ID, resolved.Version, err)
	}

	execPath, err := s.api.GetStepPrecompiledPath(resolved)
	if err != nil {
		return ActivatedStep{}, fmt.Errorf("fetching precompiled binary for `%s@%s`: %w", resolved.ID, resolved.Version, err)
	}

	// TODO: actually copy/extract the fetched step.yml to targetYMLPath instead of just exposing the source path.
	_ = targetYMLPath
	return ActivatedStep{
		StepInfo:       stepInfo,
		StepYMLPath:    ymlPath,
		ExecutablePath: execPath,
	}, nil
}

func (s *Steplib) getStepVersionInfo(stepID, version string) (models.StepInfoModel, ResolvedStepVersion, error) {
	if stepID == "" {
		return models.StepInfoModel{}, ResolvedStepVersion{}, errors.New("missing required input: step id")
	}

	allSteps, err := s.api.GetAllStepIDs()
	if err != nil {
		return models.StepInfoModel{}, ResolvedStepVersion{}, fmt.Errorf("fetching avaialble step IDs: %w", err)
	}
	if !slices.Contains(allSteps, stepID) {
		return models.StepInfoModel{}, ResolvedStepVersion{}, fmt.Errorf("%s steplib does not contain %s step", s.steplibURI, stepID)
	}

	versionConstraint, err := models.ParseRequiredVersion(version)
	if err != nil {
		return models.StepInfoModel{}, ResolvedStepVersion{}, fmt.Errorf("invalid step `%s` version constraint: %w", stepID, err)
	}
	if versionConstraint.VersionLockType == models.InvalidVersionConstraint {
		return models.StepInfoModel{}, ResolvedStepVersion{}, fmt.Errorf("invalid step `%s` version constraint: %s", stepID, version)
	}

	latestVersions, err := s.api.GetLatestStepVersions(stepID)
	if err != nil {
		return models.StepInfoModel{}, ResolvedStepVersion{}, fmt.Errorf("fetching latest versions of `%s`: %w", stepID, err)
	}

	var resolvedVersion string
	switch versionConstraint.VersionLockType {
	case models.Latest:
		resolvedVersion = latestVersions.Latest
	case models.Fixed:
		resolvedVersion = versionConstraint.Version.String()
		// ToDo: check version exists, otherwise error:
		// "%s steplib does not contain %s step %s version"
	case models.MajorLocked, models.MinorLocked:
		return models.StepInfoModel{}, ResolvedStepVersion{}, fmt.Errorf("version constraint %q not yet supported in steplib v2", version)
	default:
		return models.StepInfoModel{}, ResolvedStepVersion{}, fmt.Errorf("unknown version constraint: %s", version)
	}

	return models.StepInfoModel{
		Library:         s.steplibURI,
		ID:              stepID,
		Version:         resolvedVersion,
		OriginalVersion: version,
		LatestVersion:   latestVersions.Latest,
	}, ResolvedStepVersion{ID: stepID, Version: resolvedVersion}, nil
}
