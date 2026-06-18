package steplibrary

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/steplibindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSteplib_Activate(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source-step")
	writeSeedDir(t, sourceDir)

	givenScriptOnly := newFakeAPI()

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
			version:     "1.2.0",
			wantVersion: "1.2.0",
			wantLatest:  "3.0.0",
		},
		"fixed version not in steplib errors": {
			api:     givenScriptOnly,
			stepID:  "script",
			version: "9.9.9",
			wantErr: "does not contain script step 9.9.9 version",
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
			wantErr: "fetching available step IDs",
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
				latestVersions: map[string]steplibindex.LatestPointer{
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
			client := &Client{
				log:         testLogger{t},
				steplibURI:  "https://github.com/bitrise-io/bitrise-steplib.git",
				api:         tc.api,
				fileManager: fileutil.NewFileManager(),
				source:      stubSource{dir: sourceDir},
			}
			outDir := t.TempDir()
			outPaths := ActivateOutputPaths{
				YMLPath:  filepath.Join(outDir, "current_step.yml"),
				CodePath: filepath.Join(outDir, "code"),
			}
			got, gotErr := client.Activate(context.Background(), tc.stepID, tc.version, outPaths)
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
	cases := map[string]struct {
		given steplibindex.StepInfo
		want  models.StepGroupInfoModel
	}{
		"active step has empty deprecation fields": {
			given: steplibindex.StepInfo{
				Maintainer:  "bitrise",
				Deprecation: nil,
				AssetURLs:   []string{"assets/icon.svg"},
			},
			want: models.StepGroupInfoModel{
				Maintainer:     "bitrise",
				AssetURLs:      map[string]string{"icon.svg": "assets/icon.svg"},
				RemovalDate:    "",
				DeprecateNotes: "",
			},
		},
		"deprecated step flattens nested fields": {
			given: steplibindex.StepInfo{
				Maintainer: "community",
				Deprecation: &steplibindex.Deprecation{
					RemovalDate: "2025-12-31",
					Notes:       "Replaced by `new-step`.",
				},
				AssetURLs: nil,
			},
			want: models.StepGroupInfoModel{
				Maintainer:     "community",
				AssetURLs:      nil,
				RemovalDate:    "2025-12-31",
				DeprecateNotes: "Replaced by `new-step`.",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := toStepGroupInfoModel(tc.given)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSteplib_getStepVersionInfo(t *testing.T) {
	// newFakeAPI's "script" step exposes versions 1.0.0, 1.1.5, 1.2.0, 2.0.0,
	// 2.4.0, 2.4.1, 3.0.0 with latest 3.0.0 and latest-by-major 1→1.2.0,
	// 2→2.4.1, 3→3.0.0.
	cases := map[string]struct {
		stepID      string
		version     string
		wantVersion string
		wantErr     bool
	}{
		"latest resolves to newest version":  {stepID: "script", version: "", wantVersion: "3.0.0"},
		"fixed version that exists":          {stepID: "script", version: "2.4.1", wantVersion: "2.4.1"},
		"fixed version that is missing":      {stepID: "script", version: "2.4.9", wantErr: true},
		"major-locked picks latest of major": {stepID: "script", version: "2", wantVersion: "2.4.1"},
		"minor-locked picks highest patch":   {stepID: "script", version: "1.1", wantVersion: "1.1.5"},
		"major-locked with no such major":    {stepID: "script", version: "9", wantErr: true},
		"unknown step id":                    {stepID: "nope", version: "1.0.0", wantErr: true},
		"empty step id":                      {stepID: "", version: "1.0.0", wantErr: true},
		"invalid version constraint":         {stepID: "script", version: "1.2.3.4", wantErr: true},
	}

	client := &Client{
		log:         nil,
		steplibURI:  "https://steplib.example",
		api:         newFakeAPI(),
		fileManager: nil,
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stepInfo, resolved, gotErr := client.getStepVersionInfo(t.Context(), tc.stepID, tc.version)
			if tc.wantErr {
				require.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
			assert.Equal(t, tc.wantVersion, resolved.Version, "resolved version")
			assert.Equal(t, tc.stepID, resolved.ID, "resolved id")
			assert.Equal(t, tc.wantVersion, stepInfo.Version, "step info version")
			assert.Equal(t, tc.version, stepInfo.OriginalVersion, "step info original version")
			assert.Equal(t, "3.0.0", stepInfo.LatestVersion, "step info latest version")
			assert.Equal(t, "https://steplib.example", stepInfo.Library, "step info library")
		})
	}
}

func TestResolveMinorLocked(t *testing.T) {
	cases := map[string]struct {
		versions []string
		major    uint64
		minor    uint64
		want     string
		wantErr  bool
	}{
		"picks the highest patch in the minor": {versions: []string{"1.1.0", "1.1.5", "1.2.0"}, major: 1, minor: 1, want: "1.1.5"},
		"single matching version":              {versions: []string{"2.4.1"}, major: 2, minor: 4, want: "2.4.1"},
		"no version matches the minor":         {versions: []string{"1.0.0", "2.0.0"}, major: 1, minor: 5, wantErr: true},
		"unparseable version is an error":      {versions: []string{"not-semver"}, major: 1, minor: 0, wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := resolveMinorLocked(tc.versions, models.Semver{Major: tc.major, Minor: tc.minor, Patch: 0})
			if tc.wantErr {
				require.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
			assert.Equal(t, tc.want, got, "resolved version")
		})
	}
}
