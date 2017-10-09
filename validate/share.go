package validate

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/bitrise-io/go-utils/pathutil"
)

func stepParamURI(uri string) error {
	if uri == "" {
		return errors.New("step git URI not specified")
	}
	return nil
}

// StepParamVersion ...
func StepParamVersion(version string) error {
	if version == "" {
		return errors.New("step version not specified")
	}

	re := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	if find := re.FindString(version); find != version {
		return fmt.Errorf(`step version (%s) doesn't conforms to: ^[0-9]+\.[0-9]+\.[0-9]+$`, version)
	}

	return nil
}

// StepParamID ...
func StepParamID(id string) error {
	if id == "" {
		return errors.New("step id not specified")
	}

	re := regexp.MustCompile(`^[a-z0-9-]+$`)
	if find := re.FindString(id); find != id {
		return fmt.Errorf(`step id (%s) doesn't conforms to: ^[a-z0-9-]+$`, id)
	}

	return nil
}

// StepParams ...
func StepParams(uri, id, version string) error {
	if err := stepParamURI(uri); err != nil {
		return err
	}
	if err := StepParamID(id); err != nil {
		return err
	}
	if err := StepParamVersion(version); err != nil {
		return err
	}

	return nil
}

// StepRepo ...
func StepRepo(dir string) error {
	definitionPth := filepath.Join(dir, "step.yml")
	if exist, err := pathutil.IsPathExists(definitionPth); err != nil {
		return fmt.Errorf("failed to check if step definition (step.yml) exist, err: %s", err)
	} else if !exist {
		return fmt.Errorf("step definition (step.yml) not found at: %s", definitionPth)
	}

	return nil
}
