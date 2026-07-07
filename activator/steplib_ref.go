package activator

import (
	"path/filepath"

	"github.com/bitrise-io/stepman/activator/steplib"
	"github.com/bitrise-io/stepman/stepid"
	"github.com/bitrise-io/stepman/stepman"
)

func ActivateSteplibRefStep(
	log stepman.Logger,
	id stepid.CanonicalID,
	activatedStepDir string,
	workDir string,
	didStepLibUpdateInWorkflow bool,
	isOfflineMode bool,
) (ActivatedStep, error) {
	stepYMLPath := filepath.Join(workDir, "current_step.yml")
	//nolint:exhaustruct // missing fields are added down below based on activation result
	activationResult := ActivatedStep{
		StepYMLPath:      stepYMLPath,
		DidStepLibUpdate: false,
	}

	resolvedStep, err := steplib.ActivateStep(id, activatedStepDir, stepYMLPath, log, didStepLibUpdateInWorkflow, isOfflineMode)
	activationResult.StepInfo = resolvedStep.StepInfo
	activationResult.DidStepLibUpdate = resolvedStep.DidStepLibUpdate
	activationResult.ExecutablePath = resolvedStep.ExecPath
	if resolvedStep.ExecPath != "" {
		activationResult.ActivationType = ActivationTypeSteplibExecutable
	} else {
		activationResult.ActivationType = ActivationTypeSteplibSource
	}
	if err != nil {
		return activationResult, err
	}

	return activationResult, nil
}
