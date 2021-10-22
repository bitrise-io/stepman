package validator

import (
	"fmt"

	"github.com/bitrise-io/envman/models"
)

type EnvVarValidator struct {
}

func NewEnvVarValidator() EnvVarValidator {
	return EnvVarValidator{}
}

func (v EnvVarValidator) Validate(envVar models.EnvironmentItemModel, validateOpts bool) error {
	key, value, err := envVar.GetKeyValuePair()
	if err != nil {
		return fmt.Errorf("%v: %s", envVar, err)
	}
	opts, err := envVar.GetOptions()
	if err != nil {
		return fmt.Errorf("%s: %s", key, err)
	}

	if !validateOpts {
		return nil
	}

	if opts.Title == nil || *opts.Title == "" {
		return NewMissingEnvVarPropertyError(key, "title")
	}

	if opts.IsSensitive != nil && *opts.IsSensitive {
		if opts.IsExpand != nil && !*opts.IsExpand {
			return NewInvalidEnvVarPropertyError(key, "is_expand", "should be true if is_sensitive is true")
		}
	}

	if len(opts.ValueOptions) > 0 && value == "" {
		return fmt.Errorf("%s: %s", key, "missing value: should have a value if value_options set")
	}

	return nil
}
