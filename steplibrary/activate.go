package steplibrary

import (
	"context"
	"fmt"

	"github.com/bitrise-io/stepman/activator/result"
	"gopkg.in/yaml.v2"
)

func (s *Steplib) Activate(ctx context.Context, stepID, version string, outputPaths ActivateOutputPaths) (result.ActivatedStep, error) {
	stepInfo, resolved, err := s.getStepVersionInfo(ctx, stepID, version)
	if err != nil {
		return result.ActivatedStep{}, err
	}

	stepModel, err := s.api.GetStepModel(ctx, resolved)
	if err != nil {
		return result.ActivatedStep{}, err
	}

	stepYML, err := yaml.Marshal(stepModel)
	if err != nil {
		return result.ActivatedStep{}, fmt.Errorf("marshal step model to YAML: %w", err)
	}

	if err := s.fileManager.WriteBytes(outputPaths.YMLPath, stepYML); err != nil {
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
