package steplibrary

import (
	"errors"
	"fmt"
)

type ResolvedStepVersion struct {
	ID, Version string
}

type StepVersionsLatest struct {
	Latest string `json:"latest"`
}

type API interface {
	GetAllStepIDs() ([]string, error)
	GetLatestStepVersions(id string) (StepVersionsLatest, error)
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
			Latest: "3.0.0",
		},
	}

	v, ok := versions[id]
	if ok {
		return v, nil
	} else {
		return StepVersionsLatest{}, errors.New("not found")
	}
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
