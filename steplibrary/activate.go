package steplibrary

import (
	"context"
	"fmt"

	"github.com/bitrise-io/stepman/activator/result"
	"github.com/bitrise-io/stepman/models"
	"gopkg.in/yaml.v2"
)

func (s *Steplib) Activate(ctx context.Context, stepID, version string, outputPaths ActivateOutputPaths) (result.ActivatedStep, error) {
	stepInfo, resolved, err := s.getStepVersionInfo(ctx, stepID, version)

	var stepModel models.StepModel
	if err == nil {
		stepModel, err = s.api.GetStepModel(ctx, resolved)
	}

	var stepYML []byte
	if err == nil {
		stepYML, err = yaml.Marshal(stepModel)
		if err != nil {
			err = fmt.Errorf("marshal step model to YAML: %w", err)
		}
	}
	if err == nil {
		err = s.fileManager.WriteBytes(outputPaths.YMLPath, stepYML)
	}
	if err != nil {
		return result.ActivatedStep{}, err
	}

	return result.ActivatedStep{
		StepInfo:         stepInfo,
		StepYMLPath:      outputPaths.YMLPath,
		ExecutablePath:   "",
		ActivationType:   result.ActivationTypeSteplibSource,
		DidStepLibUpdate: false, // deprecated
	}, nil
}
