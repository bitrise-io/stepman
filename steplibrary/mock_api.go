package steplibrary

import (
	"context"
	"errors"
	"fmt"
)

// MockAPI is an in-memory API implementation used as the default in `New` and
// as a base for test fakes that embed it.
type MockAPI struct{}

func (m MockAPI) GetAllStepIDs(_ context.Context) ([]string, error) {
	return []string{"xcode-test", "script"}, nil
}

func (m MockAPI) GetLatestStepVersions(_ context.Context, id string) (StepVersionsLatest, error) {
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
	if !ok {
		return StepVersionsLatest{}, errors.New("not found")
	}
	return v, nil
}

func (m MockAPI) GetAllStepVersions(_ context.Context, id string) ([]string, error) {
	versions := map[string][]string{
		"script": {"1.0.0", "1.1.5", "1.2.0", "2.0.0", "2.4.0", "2.4.1", "3.0.0"},
	}
	v, ok := versions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (m MockAPI) GetStepGroupInfo(_ context.Context, id string) (StepGroupInfo, error) {
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

func (m MockAPI) GetStepYMLPath(_ context.Context, step ResolvedStepVersion) (string, error) {
	return fmt.Sprintf("/mock/steplib/%s/%s/step.yml", step.ID, step.Version), nil
}

func (m MockAPI) GetStepSourceZIPPath(_ context.Context, step ResolvedStepVersion) (string, error) {
	return fmt.Sprintf("/mock/steplib/%s/%s/src.zip", step.ID, step.Version), nil
}

func (m MockAPI) GetStepPrecompiledPath(_ context.Context, step ResolvedStepVersion) (string, error) {
	return fmt.Sprintf("/mock/steplib/%s/%s/bin", step.ID, step.Version), nil
}
