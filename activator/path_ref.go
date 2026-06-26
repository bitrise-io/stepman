package activator

import (
	"fmt"
	"path/filepath"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/activator/steplib"
	"github.com/bitrise-io/stepman/stepid"
	"github.com/bitrise-io/stepman/stepman"
)

func ActivatePathRefStep(
	log stepman.Logger,
	id stepid.CanonicalID,
	activatedStepDir string,
	workDir string,
) (steplib.ActivatedStep, error) {
	log.Debugf("Local step found: (path:%s)", id.IDorURI)
	// id.IDorURI is a path to the step dir in this case
	stepAbsLocalPth, err := pathutil.AbsPath(id.IDorURI)
	if err != nil {
		return steplib.ActivatedStep{}, err
	}

	exist, err := pathutil.IsDirExists(stepAbsLocalPth)
	if err != nil {
		return steplib.ActivatedStep{}, fmt.Errorf("check if a directory exists at %s: %w", stepAbsLocalPth, err)
	} else if !exist {
		return steplib.ActivatedStep{}, fmt.Errorf("the provided directory doesn't exist: %s", stepAbsLocalPth)
	}

	log.Debugf("stepAbsLocalPth: %s, stepDir:%s", stepAbsLocalPth, activatedStepDir)

	origStepYMLPth := filepath.Join(stepAbsLocalPth, "step.yml")
	exist, err = pathutil.IsPathExists(origStepYMLPth)
	if err != nil {
		return steplib.ActivatedStep{}, fmt.Errorf("check if step.yml exists at %s: %w", origStepYMLPth, err)
	} else if !exist {
		return steplib.ActivatedStep{}, fmt.Errorf("step.yml doesn't exist at %s", origStepYMLPth)
	}

	activatedStepYMLPath := filepath.Join(workDir, "current_step.yml")
	if err := command.CopyFile(origStepYMLPth, activatedStepYMLPath); err != nil {
		return steplib.ActivatedStep{}, err
	}

	if err := command.CopyDir(stepAbsLocalPth, activatedStepDir, true); err != nil {
		return steplib.ActivatedStep{}, err
	}

	stepInfo, err := stepman.QueryStepInfoFromPath(stepAbsLocalPth)
	if err != nil {
		return steplib.ActivatedStep{}, err
	}

	return steplib.ActivatedStep{
		StepInfo:         stepInfo,
		StepYMLPath:      activatedStepYMLPath,
		DidStepLibUpdate: false,
		ActivationType:   steplib.ActivationTypePathRef,
		ExecutablePath:   "",
	}, nil
}
