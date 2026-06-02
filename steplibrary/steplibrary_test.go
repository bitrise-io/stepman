package steplibrary

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/steplibrary/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSteplib_Activate(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source-step")
	writeSeedDir(t, sourceDir)

	givenScriptOnly := fakeAPI{
		ids: []string{"script"},
		latestVersions: map[string]spec.LatestPointer{
			"script": {
				StepID: "script",
				Latest: "3.0.0",
				LatestByMajor: map[string]string{
					"1": "1.2.0",
					"2": "2.4.1",
					"3": "3.0.0",
				},
			},
		},
		allVersions: map[string][]string{
			"script": {"1.0.0", "1.1.5", "1.2.0", "2.0.0", "2.4.0", "2.4.1", "3.0.0"},
		},
	}

	cases := map[string]struct {
		api         API
		stepID      string
		version     string
		wantVersion string
		wantLatest  string
		wantErr     string
	}{
		"empty step id returns validation error": {
			api:     fakeAPI{},
			stepID:  "",
			wantErr: "missing required input",
		},
		"step not in collection": {
			api:     fakeAPI{ids: []string{"script"}},
			stepID:  "xcode-test",
			wantErr: "steplib does not contain xcode-test step",
		},
		"invalid version constraint": {
			api:     givenScriptOnly,
			stepID:  "script",
			version: "not-a-version",
			wantErr: "invalid step version constraint",
		},
		"empty version resolves to latest": {
			api:         givenScriptOnly,
			stepID:      "script",
			version:     "",
			wantVersion: "3.0.0",
			wantLatest:  "3.0.0",
		},
		"fixed version resolves to requested": {
			api:         givenScriptOnly,
			stepID:      "script",
			version:     "1.2.3",
			wantVersion: "1.2.3",
			wantLatest:  "3.0.0",
		},
		"major-locked resolves via latest_by_major": {
			api:         givenScriptOnly,
			stepID:      "script",
			version:     "2",
			wantVersion: "2.4.1",
			wantLatest:  "3.0.0",
		},
		"major-locked with unknown major errors": {
			api:     givenScriptOnly,
			stepID:  "script",
			version: "99",
			wantErr: "does not contain script step with major version 99",
		},
		"minor-locked resolves to highest matching patch": {
			api:         givenScriptOnly,
			stepID:      "script",
			version:     "1.1",
			wantVersion: "1.1.5",
			wantLatest:  "3.0.0",
		},
		"minor-locked with no matching minor errors": {
			api:     givenScriptOnly,
			stepID:  "script",
			version: "1.9",
			wantErr: "no version matches 1.9.x",
		},
		"list error propagates": {
			api:     fakeAPI{listErr: errors.New("boom")},
			stepID:  "script",
			wantErr: "fetching avaialble step IDs",
		},
		"latest versions error propagates": {
			api: fakeAPI{
				ids:               []string{"script"},
				latestVersionsErr: errors.New("kaboom"),
			},
			stepID:  "script",
			wantErr: "fetching latest versions of `script`",
		},
		"group info error propagates": {
			api: fakeAPI{
				ids: []string{"script"},
				latestVersions: map[string]spec.LatestPointer{
					"script": {StepID: "script", Latest: "3.0.0"},
				},
				groupInfoErr: errors.New("infoboom"),
			},
			stepID:  "script",
			wantErr: "fetching group info of `script`",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := &Steplib{
				log:              discardLogger{},
				steplibURI:       "https://github.com/bitrise-io/bitrise-steplib.git",
				api:              tc.api,
				fileManager:      fileutil.NewFileManager(),
				fetchSourceDirFn: func(_ context.Context, _ ResolvedStepVersion) (string, error) { return sourceDir, nil },
			}
			outDir := t.TempDir()
			outPaths := ActivateOutputPaths{
				YMLPath:  filepath.Join(outDir, "current_step.yml"),
				CodePath: filepath.Join(outDir, "code"),
			}
			got, gotErr := s.Activate(context.Background(), tc.stepID, tc.version, outPaths)
			if tc.wantErr != "" {
				require.Errorf(t, gotErr, "Activate(%q, %q)", tc.stepID, tc.version)
				assert.Containsf(t, gotErr.Error(), tc.wantErr, "Activate(%q, %q) error message", tc.stepID, tc.version)
				return
			}
			require.NoErrorf(t, gotErr, "Activate(%q, %q)", tc.stepID, tc.version)
			assert.Equal(t, tc.stepID, got.StepInfo.ID, "StepInfo.ID")
			assert.Equal(t, tc.wantVersion, got.StepInfo.Version, "StepInfo.Version")
			assert.Equal(t, tc.wantLatest, got.StepInfo.LatestVersion, "StepInfo.LatestVersion")
			assert.Equal(t, tc.version, got.StepInfo.OriginalVersion, "StepInfo.OriginalVersion")
			assert.Equal(t, "bitrise", got.StepInfo.GroupInfo.Maintainer, "StepInfo.GroupInfo.Maintainer")
			assert.Equal(t, "assets/icon.svg", got.StepInfo.GroupInfo.AssetURLs["icon.svg"], "StepInfo.GroupInfo.AssetURLs[icon.svg]")
		})
	}
}

func TestToStepGroupInfoModel(t *testing.T) {
	t.Run("active step has empty deprecation fields", func(t *testing.T) {
		got := toStepGroupInfoModel(spec.StepInfo{
			Maintainer:  "bitrise",
			Deprecation: nil,
			AssetURLs:   map[string]string{"icon.svg": "assets/icon.svg"},
		})
		assert.Equal(t, "bitrise", got.Maintainer, "Maintainer")
		assert.Empty(t, got.RemovalDate, "RemovalDate")
		assert.Empty(t, got.DeprecateNotes, "DeprecateNotes")
		assert.Equal(t, "assets/icon.svg", got.AssetURLs["icon.svg"], "AssetURLs[icon.svg]")
	})

	t.Run("deprecated step flattens nested fields", func(t *testing.T) {
		got := toStepGroupInfoModel(spec.StepInfo{
			Maintainer: "community",
			Deprecation: &spec.Deprecation{
				RemovalDate: "2025-12-31",
				Notes:       "Replaced by `new-step`.",
			},
			AssetURLs: nil,
		})
		assert.Equal(t, "2025-12-31", got.RemovalDate, "RemovalDate")
		assert.Equal(t, "Replaced by `new-step`.", got.DeprecateNotes, "DeprecateNotes")
		assert.Equal(t, "community", got.Maintainer, "Maintainer")
	})
}
