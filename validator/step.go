package validator

import (
	"strings"
	"time"

	"github.com/bitrise-io/stepman/models"
)

type StepValidator struct {
	envVarValidator EnvVarValidator
}

func NewStepValidator(envVarValidator EnvVarValidator) StepValidator {
	return StepValidator{envVarValidator: envVarValidator}
}

func (v StepValidator) Validate(step models.StepModel) error {
	// AuditBeforeShare
	if step.Title == nil || *step.Title == "" {
		return NewMissingPropertyError("title")
	}
	if step.Summary == nil || *step.Summary == "" {
		return NewMissingPropertyError("summary")
	}
	if step.Website == nil || *step.Website == "" {
		return NewMissingPropertyError("website")
	}
	if step.Timeout != nil && *step.Timeout < 0 {
		return NewInvalidPropertyError("timeout", "less than 0")
	}
	// AuditBeforeShare

	// +Audit
	if step.PublishedAt == nil || step.PublishedAt.Equal(time.Time{}) {
		return NewMissingPropertyError("published_at")
	}

	if step.Source == nil {
		return NewMissingPropertyError("source")
	} else if step.Source.Git == "" {
		return NewMissingPropertyError("source.git")
	} else if !strings.HasPrefix(step.Source.Git, "http://") && !strings.HasPrefix(step.Source.Git, "https://") {
		return NewInvalidPropertyError("source.git", "should start with http:// or https://")
	} else if !strings.HasSuffix(step.Source.Git, ".git") {
		return NewInvalidPropertyError("source.git", "should end with .git")
	} else if step.Source.Commit == "" {
		return NewMissingPropertyError("source.commit")
	}
	// +Audit

	for _, input := range step.Inputs {
		err := v.envVarValidator.Validate(input, true)
		if err != nil {
			return NewInvalidPropertyError("inputs", err.Error())
		}
	}

	for _, output := range step.Outputs {
		err := v.envVarValidator.Validate(output, true)
		if err != nil {
			return NewInvalidPropertyError("outputs", err.Error())
		}
	}

	return nil
}
