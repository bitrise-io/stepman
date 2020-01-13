package models

import (
	"reflect"
	"testing"

	envmanModels "github.com/bitrise-io/envman/models"
	"github.com/bitrise-io/go-utils/pointers"
	"github.com/google/go-cmp/cmp"
)

func Test_parseRequiredVersion(t *testing.T) {
	tests := []struct {
		name            string
		requiredVersion string
		want            versionConstraint
		wantErr         bool
	}{
		{
			name:            "fixed version",
			requiredVersion: "1.2.3",
			want: versionConstraint{
				versionLockType: fixed,
				version: semver{
					major: 1,
					minor: 2,
					patch: 3,
				},
			},
		},
		{
			name:            "locked minor version",
			requiredVersion: "1.2.x",
			want: versionConstraint{
				versionLockType: minorLocked,
				version: semver{
					major: 1,
					minor: 2,
				},
			},
		},
		{
			name:            "locked minor version (omitted)",
			requiredVersion: "1.2",
			want: versionConstraint{
				versionLockType: minorLocked,
				version: semver{
					major: 1,
					minor: 2,
				},
			},
		},
		{
			name:            "locked minor version (omitted) - Invalid due to trailing dot",
			requiredVersion: "1.2.",
			want:            versionConstraint{},
			wantErr:         true,
		},
		{
			name:            "locked major version",
			requiredVersion: "1.x.x",
			want: versionConstraint{
				versionLockType: majorLocked,
				version: semver{
					major: 1,
				},
			},
		},
		{
			name:            "locked major version - Invalid due to omitted patch version",
			requiredVersion: "1.x",
			want:            versionConstraint{},
			wantErr:         true,
		},
		{
			name:            "locked major version (omitted)",
			requiredVersion: "1",
			want: versionConstraint{
				versionLockType: majorLocked,
				version: semver{
					major: 1,
				},
			},
		},
		{
			name:            "locked major version (omitted) - Invalid due to trailing dot",
			requiredVersion: "1.",
			want:            versionConstraint{},
			wantErr:         true,
		},
		{
			name:            "locked major version (omitted) - Invalid due to omitted minor version",
			requiredVersion: "1..x",
			want:            versionConstraint{},
			wantErr:         true,
		},
		{
			name:            "Invalid major version, x",
			requiredVersion: "x.x.x",
			want:            versionConstraint{},
			wantErr:         true,
		},
		{
			name:            "No version specified",
			requiredVersion: "",
			want:            versionConstraint{},
			wantErr:         true,
		},
		{
			name:            "Invalid major version, negative number",
			requiredVersion: "-1",
			want:            versionConstraint{},
			wantErr:         true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRequiredVersion(tt.requiredVersion)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRequiredVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseRequiredVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_latestMatchingRequiredVersion(t *testing.T) {
	step := StepModel{
		Title:         pointers.NewStringPtr("name 1"),
		Description:   pointers.NewStringPtr("desc 1"),
		Website:       pointers.NewStringPtr("web/1"),
		SourceCodeURL: pointers.NewStringPtr("fork/1"),
		Source: &StepSourceModel{
			Git: "https://git.url",
		},
		HostOsTags:          []string{"osx"},
		ProjectTypeTags:     []string{"ios"},
		TypeTags:            []string{"test"},
		IsRequiresAdminUser: pointers.NewBoolPtr(DefaultIsRequiresAdminUser),
		Inputs: []envmanModels.EnvironmentItemModel{
			envmanModels.EnvironmentItemModel{
				"KEY_1": "Value 1",
			},
			envmanModels.EnvironmentItemModel{
				"KEY_2": "Value 2",
			},
		},
		Outputs: []envmanModels.EnvironmentItemModel{
			envmanModels.EnvironmentItemModel{
				"KEY_3": "Value 3",
			},
		},
	}
	stepGroup := StepGroupModel{
		Versions: map[string]StepModel{
			"1.0.0": step,
			"1.1.0": step,
			"1.1.1": step,
			"1.2.0": step,
			"2.0.0": step,
		},
		LatestVersionNumber: "2.0.0",
	}

	tests := []struct {
		name            string
		requiredVersion versionConstraint
		stepVersions    StepGroupModel
		want            StepVersionModel
		want1           bool
	}{
		{
			name: "Fix version",
			requiredVersion: versionConstraint{
				versionLockType: fixed,
				version: semver{
					major: 1,
					minor: 0,
					patch: 0,
				},
			},
			stepVersions: stepGroup,
			want: StepVersionModel{
				Step:                   step,
				Version:                "1.0.0",
				LatestAvailableVersion: "2.0.0",
			},
			want1: true,
		},
		{
			name: "Lock Minor version",
			requiredVersion: versionConstraint{
				versionLockType: minorLocked,
				version: semver{
					major: 1,
					minor: 1,
				},
			},
			stepVersions: stepGroup,
			want: StepVersionModel{
				Step:                   step,
				Version:                "1.1.1",
				LatestAvailableVersion: "2.0.0",
			},
			want1: true,
		},
		{
			name: "Lock Major ersion",
			requiredVersion: versionConstraint{
				versionLockType: majorLocked,
				version: semver{
					major: 1,
				},
			},
			stepVersions: stepGroup,
			want: StepVersionModel{
				Step:                   step,
				Version:                "1.2.0",
				LatestAvailableVersion: "2.0.0",
			},
			want1: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := latestMatchingRequiredVersion(tt.requiredVersion, tt.stepVersions)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("latestMatchingRequiredVersion() got = %+v, want +%v,\n Diff: %s", got, tt.want, cmp.Diff(tt.want1, got))
			}
			if got1 != tt.want1 {
				t.Errorf("latestMatchingRequiredVersion() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
