package steplibrary

import (
	"errors"
	"fmt"
)

type ResolvedStepVersion struct {
	ID, Version string
}

// StepVersionsLatest mirrors `spec/steps/<id>/latest.json` from the V2 inventory
// layout described in plan.md. Resolves Latest and MajorLocked version constraints
// in a single fetch.
type StepVersionsLatest struct {
	StepID        string            `json:"step_id"`
	Latest        string            `json:"latest"`
	LatestByMajor map[string]string `json:"latest_by_major"`
}

// StepGroupInfo mirrors `steps/<id>/step-info.json` from the V2 inventory layout
// described in plan.md. Version-independent metadata for a step.
type StepGroupInfo struct {
	Maintainer  string            `json:"maintainer"`
	Deprecation *Deprecation      `json:"deprecation,omitempty"`
	AssetURLs   map[string]string `json:"asset_urls"`
}

// Deprecation carries the removal date and migration notes when a step is being
// retired. Nil on `StepGroupInfo.Deprecation` means the step is active.
type Deprecation struct {
	RemovalDate string `json:"removal_date"`
	Notes       string `json:"notes"`
}

type API interface {
	GetAllStepIDs() ([]string, error)
	GetLatestStepVersions(id string) (StepVersionsLatest, error)
	// GetAllStepVersions returns all available versions of a step.
	// Mirrors `spec/steps/<id>/versions.json` from the V2 inventory layout;
	// the per-version metadata is dropped for now since callers only need the
	// version strings to resolve MinorLocked constraints.
	GetAllStepVersions(id string) ([]string, error)
	// GetStepGroupInfo returns version-independent step metadata
	// (maintainer, deprecation, asset URLs). Mirrors `steps/<id>/step-info.json`.
	GetStepGroupInfo(id string) (StepGroupInfo, error)
	GetStepYMLPath(step ResolvedStepVersion) (string, error)
	GetStepSourceZIPPath(step ResolvedStepVersion) (string, error)
	GetStepPrecompiledPath(step ResolvedStepVersion) (string, error)
}

type MockAPI struct {
}

func (m MockAPI) GetAllStepIDs() ([]string, error) {
	return []string{"xcode-test", "script"}, nil
}

func (m MockAPI) GetLatestStepVersions(id string) (StepVersionsLatest, error) {
	versions := map[string]StepVersionsLatest{
		"script": {
			StepID: "script",
			Latest: "3.0.0",
			LatestByMajor: map[string]string{
				"1": "1.2.0",
				"2": "2.4.1",
				"3": "3.0.0",
			},
		},
	}

	v, ok := versions[id]
	if ok {
		return v, nil
	} else {
		return StepVersionsLatest{}, errors.New("not found")
	}
}

func (m MockAPI) GetAllStepVersions(id string) ([]string, error) {
	versions := map[string][]string{
		"script": {"1.0.0", "1.1.5", "1.2.0", "2.0.0", "2.4.0", "2.4.1", "3.0.0"},
	}
	v, ok := versions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (m MockAPI) GetStepGroupInfo(id string) (StepGroupInfo, error) {
	infos := map[string]StepGroupInfo{
		"script": {
			Maintainer:  "bitrise",
			Deprecation: nil,
			AssetURLs: map[string]string{
				"icon.svg": "assets/icon.svg",
			},
		},
	}
	v, ok := infos[id]
	if !ok {
		return StepGroupInfo{}, errors.New("not found")
	}
	return v, nil
}

func (m MockAPI) GetStepYMLPath(step ResolvedStepVersion) (string, error) {
	return fmt.Sprintf("/mock/steplib/%s/%s/step.yml", step.ID, step.Version), nil
}

func (m MockAPI) GetStepSourceZIPPath(step ResolvedStepVersion) (string, error) {
	return fmt.Sprintf("/mock/steplib/%s/%s/src.zip", step.ID, step.Version), nil
}

func (m MockAPI) GetStepPrecompiledPath(step ResolvedStepVersion) (string, error) {
	return fmt.Sprintf("/mock/steplib/%s/%s/bin", step.ID, step.Version), nil
}
