package validate

import (
	"errors"
	"fmt"
	"path"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/stepman"
)

// IfStepLibNotExistLocally ...
func IfStepLibNotExistLocally(uri string) error {
	if uri == "" {
		return errors.New("stepLib git URI not specified")
	}

	// For sharing we need a clean StepLib
	if route, found := stepman.ReadRoute(uri); found {
		pth := stepman.GetLibraryBaseDirPath(route)
		return fmt.Errorf("stepLib found locally at: %s", pth)
	}

	return nil
}

// IfStepNotExistInSteplib ...
func IfStepNotExistInSteplib(id, version, steplibURI string) error {
	route, found := stepman.ReadRoute(steplibURI)
	if !found {
		return fmt.Errorf("no route found for StepLib (%s)", steplibURI)
	}

	stepDir := stepman.GetStepCollectionDirPath(route, id, version)
	stepYMLPth := path.Join(stepDir, "step.yml")
	if exist, err := pathutil.IsPathExists(stepYMLPth); err != nil {
		return fmt.Errorf("failed to check if step version exist, err: %s", err)
	} else if exist {
		return errors.New("step version already exist")
	}

	return nil
}
