package steplibrary

import (
	"context"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/pointers"
	"github.com/bitrise-io/stepman/models"
)

// FakeAPI is an in-memory API implementation used as a base for test fakes
// that embed it and selectively override methods.
type FakeAPI struct{}

func (m FakeAPI) GetAllStepIDs(_ context.Context) ([]string, error) {
	return []string{"xcode-test", "script"}, nil
}

func (m FakeAPI) GetLatestStepVersions(_ context.Context, id string) (StepVersionsLatest, error) {
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

func (m FakeAPI) GetAllStepVersions(_ context.Context, id string) ([]string, error) {
	versions := map[string][]string{
		"script": {"1.0.0", "1.1.5", "1.2.0", "2.0.0", "2.4.0", "2.4.1", "3.0.0"},
	}
	v, ok := versions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (m FakeAPI) GetStepGroupInfo(_ context.Context, id string) (StepGroupInfo, error) {
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

func (m FakeAPI) GetStepModel(_ context.Context, step ResolvedStepVersion) (models.StepModel, error) {
	if step.ID != "script" {
		return models.StepModel{}, errors.New("not found")
	}
	//nolint:exhaustruct // mock returns a minimal StepModel; downstream consumers don't need the full shape here
	return models.StepModel{
		Title:   pointers.NewStringPtr("Script"),
		Summary: pointers.NewStringPtr("Runs a shell script."),
	}, nil
}

func (m FakeAPI) GetStepSourceZIPPath(_ context.Context, step ResolvedStepVersion) (string, error) {
	return fmt.Sprintf("/mock/steplib/%s/%s/src.zip", step.ID, step.Version), nil
}
