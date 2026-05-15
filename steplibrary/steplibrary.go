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

func New(log stepman.Logger, steplibURI string, isOfflineMode bool) *Steplib {
	return &Steplib{
		log:           log,
		steplibURI:    steplibURI,
		isOfflineMode: isOfflineMode,
		api:           MockAPI{},
	}
}

func (s *Steplib) Activate(stepID, version string) (models.StepInfoModel, error) {
	if stepID == "" {
		return models.StepInfoModel{}, errors.New("missing required input: step id")
	}

	allSteps, err := s.api.GetAllStepIDs()
	if err != nil {
		return models.StepInfoModel{}, fmt.Errorf("fetching avaialble step IDs: %w", err)
	}
	if !slices.Contains(allSteps, stepID) {
		return models.StepInfoModel{}, fmt.Errorf("collection doesn't contain step: %s", stepID)
	}

	versionConstraint, err := models.ParseRequiredVersion(version)
	if err != nil {
		return models.StepInfoModel{}, fmt.Errorf("invalid step `%s` version constraint: %w", stepID, err)
	}
	if versionConstraint.VersionLockType == models.InvalidVersionConstraint {
		return models.StepInfoModel{}, fmt.Errorf("invalid step `%s` version constraint: %s", stepID, version)
	}

	latestVersions, err := s.api.GetLatestStepVersions(stepID)
	if err != nil {
		return models.StepInfoModel{}, fmt.Errorf("fetching versions of `%s`: %w", stepID, err)
	}

	var resolved string
	switch versionConstraint.VersionLockType {
	case models.Latest:
		resolved = latestVersions.Latest
	case models.Fixed:
		resolved = versionConstraint.Version.String()
	case models.MajorLocked, models.MinorLocked:
		return models.StepInfoModel{}, fmt.Errorf("version constraint %q not yet supported in steplib v2", version)
	default:
		return models.StepInfoModel{}, fmt.Errorf("unknown version constraint: %s", version)
	}

	return models.StepInfoModel{
		Library:         s.steplibURI,
		ID:              stepID,
		Version:         resolved,
		OriginalVersion: version,
		LatestVersion:   latestVersions.Latest,
	}, nil
}
