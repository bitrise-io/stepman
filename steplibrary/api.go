package steplibrary

import "errors"

type StepVersionsLatest struct {
	Latest string `json:"latest"`
}

type API interface {
	GetAllStepIDs() ([]string, error)
	GetLatestStepVersions(id string) (StepVersionsLatest, error)
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
