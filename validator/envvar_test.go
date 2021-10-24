package validator

import (
	"testing"

	"github.com/bitrise-io/envman/models"
	"github.com/bitrise-io/go-utils/pointers"
)

var validEnvVar = models.EnvironmentItemModel{
	"key": "",
}

var validOpts = models.EnvironmentItemOptionsModel{
	Title: pointers.NewStringPtr("Test envvar"),
}

func TestEnvVarValidator_Validate(t *testing.T) {
	tests := []struct {
		name         string
		envVar       func() models.EnvironmentItemModel
		validateOpts bool
		wantErr      string
	}{
		{
			name: "minimal valid envvar",
			envVar: func() models.EnvironmentItemModel {
				return validEnvVar
			},
			validateOpts: false,
			wantErr:      "",
		},
		{
			name: "minimal valid envvar opts",
			envVar: func() models.EnvironmentItemModel {
				envvar := validEnvVar
				envvar[models.OptionsKey] = validOpts
				return envvar
			},
			validateOpts: true,
			wantErr:      "",
		},
		{
			name: "title not set",
			envVar: func() models.EnvironmentItemModel {
				envvar := validEnvVar
				opts := validOpts
				opts.Title = nil
				envvar[models.OptionsKey] = opts
				return envvar
			},
			validateOpts: true,
			wantErr:      "key: missing property: title",
		},
		{
			name: "sensitive envvar's is_expand is false",
			envVar: func() models.EnvironmentItemModel {
				envvar := validEnvVar
				opts := validOpts
				opts.IsSensitive = pointers.NewBoolPtr(true)
				opts.IsExpand = pointers.NewBoolPtr(false)
				envvar[models.OptionsKey] = opts
				return envvar
			},
			validateOpts: true,
			wantErr:      "key: invalid property: is_expand: should be true if is_sensitive is true",
		},
		{
			name: "envvar value is required if value_options ste",
			envVar: func() models.EnvironmentItemModel {
				envvar := validEnvVar
				opts := validOpts
				opts.ValueOptions = []string{"option1", "option2"}
				envvar[models.OptionsKey] = opts
				return envvar
			},
			validateOpts: true,
			wantErr:      "key: missing value: should have a value if value_options set",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := EnvVarValidator{}
			gotErr := v.Validate(tt.envVar(), tt.validateOpts)
			if tt.wantErr == "" && gotErr != nil {
				t.Errorf("unexpected error: %v", gotErr)
			}
			if tt.wantErr != "" && gotErr == nil {
				t.Errorf("expected error: %s, got nil", tt.wantErr)
			}
			if tt.wantErr != "" && gotErr != nil && gotErr.Error() != tt.wantErr {
				t.Errorf("expected error: %s, got: %s", tt.wantErr, gotErr)
			}
		})
	}
}
