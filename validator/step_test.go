package validator

import (
	"testing"
	"time"

	envmanModels "github.com/bitrise-io/envman/models"
	"github.com/bitrise-io/go-utils/pointers"
	"github.com/bitrise-io/stepman/models"
)

var validStep = models.StepModel{
	Title:       pointers.NewStringPtr("title"),
	Summary:     pointers.NewStringPtr("summary"),
	Website:     pointers.NewStringPtr("website"),
	PublishedAt: pointers.NewTimePtr(time.Date(2012, time.January, 1, 0, 0, 0, 0, time.UTC)),
	Source: &models.StepSourceModel{
		Git:    "https://github.com/bitrise-io/test-step.git",
		Commit: "1e1482141079fc12def64d88cb7825b8f1cb1dc3",
	},
}

func TestStepValidator_Validate(t *testing.T) {
	tests := []struct {
		name    string
		step    func() models.StepModel
		wantErr string
	}{
		{
			name: "minimal valid step",
			step: func() models.StepModel {
				return validStep
			},
			wantErr: "",
		},
		{
			name: "title not set",
			step: func() models.StepModel {
				step := validStep
				step.Title = nil
				return step
			},
			wantErr: "missing property: title",
		},
		{
			name: "title is empty",
			step: func() models.StepModel {
				step := validStep
				step.Title = pointers.NewStringPtr("")
				return step
			},
			wantErr: "missing property: title",
		},
		{
			name: "summary not set",
			step: func() models.StepModel {
				step := validStep
				step.Summary = nil
				return step
			},
			wantErr: "missing property: summary",
		},
		{
			name: "summary is empty",
			step: func() models.StepModel {
				step := validStep
				step.Summary = pointers.NewStringPtr("")
				return step
			},
			wantErr: "missing property: summary",
		},
		{
			name: "website not set",
			step: func() models.StepModel {
				step := validStep
				step.Website = nil
				return step
			},
			wantErr: "missing property: website",
		},
		{
			name: "website is empty",
			step: func() models.StepModel {
				step := validStep
				step.Website = pointers.NewStringPtr("")
				return step
			},
			wantErr: "missing property: website",
		},
		{
			name: "timeout is less than 0",
			step: func() models.StepModel {
				step := validStep
				step.Timeout = pointers.NewIntPtr(-1)
				return step
			},
			wantErr: "invalid property: timeout: less than 0",
		},
		{
			name: "published_at not set",
			step: func() models.StepModel {
				step := validStep
				step.PublishedAt = nil
				return step
			},
			wantErr: "missing property: published_at",
		},
		{
			name: "published_at is zero",
			step: func() models.StepModel {
				step := validStep
				step.PublishedAt = pointers.NewTimePtr(time.Time{})
				return step
			},
			wantErr: "missing property: published_at",
		},
		{
			name: "source not set",
			step: func() models.StepModel {
				step := validStep
				step.Source = nil
				return step
			},
			wantErr: "missing property: source",
		},
		{
			name: "source.git does not end with .git",
			step: func() models.StepModel {
				source := *validStep.Source
				source.Git = "https://github.com/bitrise-io/test-step"

				step := validStep
				step.Source = &source
				return step
			},
			wantErr: "invalid property: source.git: should end with .git",
		},
		{
			name: "source.git does not start with http or https",
			step: func() models.StepModel {
				source := *validStep.Source
				source.Git = "git@github.com:bitrise-io/test-step.git"

				step := validStep
				step.Source = &source
				return step
			},
			wantErr: "invalid property: source.git: should start with http:// or https://",
		},
		{
			name: "source.commit is empty",
			step: func() models.StepModel {
				source := *validStep.Source
				source.Commit = ""

				step := validStep
				step.Source = &source
				return step
			},
			wantErr: "missing property: source.commit",
		},
		{
			name: "invalid input",
			step: func() models.StepModel {
				step := validStep
				step.Inputs = []envmanModels.EnvironmentItemModel{
					{
						"opts": "value",
					},
				}
				return step
			},
			wantErr: "invalid property: inputs: map[opts:value]: no environment key found, keys: [opts]",
		},
		{
			name: "invalid output",
			step: func() models.StepModel {
				step := validStep
				step.Outputs = []envmanModels.EnvironmentItemModel{
					{
						"opts": "value",
					},
				}
				return step
			},
			wantErr: "invalid property: outputs: map[opts:value]: no environment key found, keys: [opts]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := StepValidator{
				envVarValidator: NewEnvVarValidator(),
			}
			gotErr := v.Validate(tt.step())
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
