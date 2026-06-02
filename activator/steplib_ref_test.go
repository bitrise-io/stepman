package activator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/activator/result"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSteplibV2URL(t *testing.T) {
	cases := map[string]struct {
		flagValue  string
		steplibURI string
		wantURL    string
		wantUseV2  bool
	}{
		"flag off keeps the legacy path": {
			flagValue:  "",
			steplibURI: bitriseV1SteplibURL,
			wantUseV2:  false,
		},
		"flag=true rewrites the official V1 URL to V2": {
			flagValue:  "true",
			steplibURI: bitriseV1SteplibURL,
			wantURL:    bitriseV2SteplibURL,
			wantUseV2:  true,
		},
		"flag=1 rewrites the official V1 URL to V2": {
			flagValue:  "1",
			steplibURI: bitriseV1SteplibURL,
			wantURL:    bitriseV2SteplibURL,
			wantUseV2:  true,
		},
		"non-.git URL is used directly as a V2 base": {
			flagValue:  "true",
			steplibURI: "https://my-cdn.example/steplib/",
			wantURL:    "https://my-cdn.example/steplib/",
			wantUseV2:  true,
		},
		"alt-steplib .git URL keeps the legacy path": {
			flagValue:  "true",
			steplibURI: "https://github.com/acme/custom-steplib.git",
			wantUseV2:  false,
		},
		"unexpected flag value keeps the legacy path": {
			flagValue:  "yes",
			steplibURI: bitriseV1SteplibURL,
			wantUseV2:  false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(useSteplibV2, tc.flagValue)
			gotURL, gotUseV2 := resolveSteplibV2URL(tc.steplibURI)
			assert.Equal(t, tc.wantUseV2, gotUseV2, "useV2")
			assert.Equal(t, tc.wantURL, gotURL, "v2URL")
		})
	}
}

func Test_activateStepLibStep(t *testing.T) {
	tests := []struct {
		name        string
		stepIDData  stepid.CanonicalID
		wantVersion string
		wantErr     bool
	}{
		{
			name: "Major version lock",
			stepIDData: stepid.CanonicalID{
				SteplibSource: "https://github.com/bitrise-io/bitrise-steplib.git",
				IDorURI:       "xcode-archive",
				Version:       "1",
			},
			wantVersion: "1.10.1",
			wantErr:     false,
		},
		{
			name: "Major version lock (long form)",
			stepIDData: stepid.CanonicalID{
				SteplibSource: "https://github.com/bitrise-io/bitrise-steplib.git",
				IDorURI:       "xcode-archive",
				Version:       "1.x.x",
			},
			wantVersion: "1.10.1",
			wantErr:     false,
		},
		{
			name: "Minor version lock",
			stepIDData: stepid.CanonicalID{
				SteplibSource: "https://github.com/bitrise-io/bitrise-steplib.git",
				IDorURI:       "xcode-archive",
				Version:       "2.3",
			},
			wantVersion: "2.3.7",
			wantErr:     false,
		},
		{
			name: "Minor version lock (long form)",
			stepIDData: stepid.CanonicalID{
				SteplibSource: "https://github.com/bitrise-io/bitrise-steplib.git",
				IDorURI:       "xcode-archive",
				Version:       "2.3.x",
			},
			wantVersion: "2.3.7",
			wantErr:     false,
		},
		{
			name: "Patch version lock",
			stepIDData: stepid.CanonicalID{
				SteplibSource: "https://github.com/bitrise-io/bitrise-steplib.git",
				IDorURI:       "xcode-archive",
				Version:       "2.3.2",
			},
			wantVersion: "2.3.2",
			wantErr:     false,
		},
		{
			name: "Invalid version lock",
			stepIDData: stepid.CanonicalID{
				SteplibSource: "https://github.com/bitrise-io/bitrise-steplib.git",
				IDorURI:       "xcode-archive",
				Version:       "1.2.3.4",
			},
			wantVersion: "",
			wantErr:     true,
		},
		{
			name: "Latest version (not supported at the moment)",
			stepIDData: stepid.CanonicalID{
				SteplibSource: "https://github.com/bitrise-io/bitrise-steplib.git",
				IDorURI:       "xcode-archive",
				Version:       "x.x.x",
			},
			wantVersion: "",
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := pathutil.NormalizedOSTempDirPath("activateStepLibStep")
			if err != nil {
				t.Errorf("failed to create tmp dir: %s", err)
			}
			stepYMLCopyPth := filepath.Join(tmpDir, "step-yml", "step.yml")

			if err := os.MkdirAll(filepath.Dir(stepYMLCopyPth), 0777); err != nil {
				t.Errorf("failed to create dir for step.yml: %s", err)
			}

			got, _, err := prepareStepLibForActivation(TestLogger[*testing.T]{t}, tt.stepIDData, false, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("activateStepLibStep() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got.Version != tt.wantVersion {
				t.Errorf("activateStepLibStep() got = %v, want %v", got.Version, tt.wantVersion)
			}
		})
	}
}

type genericLogger interface {
	Logf(format string, v ...any)
}

type TestLogger[t genericLogger] struct {
	l genericLogger
}

func (t TestLogger[l]) Debugf(format string, v ...any) {
	t.l.Logf(format, v...)
}
func (t TestLogger[l]) Errorf(format string, v ...any) {
	t.l.Logf(format, v...)
}
func (t TestLogger[l]) Warnf(format string, v ...any) {
	t.l.Logf(format, v...)
}
func (t TestLogger[l]) Infof(format string, v ...any) {
	t.l.Logf(format, v...)
}

func BenchmarkActivateSteplibRefStep(b *testing.B) {
	logger := TestLogger[*testing.B]{b}
	tests := []struct {
		name                       string
		id                         stepid.CanonicalID
		isOfflineMode              bool
		didStepLibUpdateInWorkflow bool
		shouldCleanSteplib         bool
		wantErr                    bool
	}{
		{
			name: "No steplib update, major versiom",
			id: stepid.CanonicalID{
				SteplibSource: "https://github.com/bitrise-io/bitrise-steplib.git",
				IDorURI:       "xcode-archive",
				Version:       "1",
			},
			didStepLibUpdateInWorkflow: true,
			wantErr:                    false,
		},
		{
			name: "Steplib update, major versiom",
			id: stepid.CanonicalID{
				SteplibSource: "https://github.com/bitrise-io/bitrise-steplib.git",
				IDorURI:       "xcode-archive",
				Version:       "1",
			},
			didStepLibUpdateInWorkflow: false,
			wantErr:                    false,
		},
		{
			name: "Clean steplib every time",
			id: stepid.CanonicalID{
				SteplibSource: "https://github.com/bitrise-io/bitrise-steplib.git",
				IDorURI:       "xcode-archive",
				Version:       "1",
			},
			didStepLibUpdateInWorkflow: false,
			shouldCleanSteplib:         true,
			wantErr:                    false,
		},
	}
	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			for b.Loop() {
				if tt.shouldCleanSteplib {
					err := os.RemoveAll("~/.stepman")
					require.NoError(b, err)
				}
				tmpDir, err := pathutil.NormalizedOSTempDirPath("activateStepLibStep")
				if err != nil {
					b.Errorf("failed to create tmp dir: %s", err)
				}
				stepYMLCopyPth := filepath.Join(tmpDir, "step-yml", "step.yml")

				if err := os.MkdirAll(filepath.Dir(stepYMLCopyPth), 0777); err != nil {
					b.Errorf("failed to create dir for step.yml: %s", err)
				}

				stepInfoPtr := new(models.StepInfoModel)
				got, gotErr := ActivateSteplibRefStep(logger, tt.id, stepYMLCopyPth, tmpDir, tt.didStepLibUpdateInWorkflow, tt.isOfflineMode, stepInfoPtr)
				if gotErr != nil {
					if !tt.wantErr {
						b.Errorf("ActivateSteplibRefStep() failed: %v", gotErr)
					}
					return
				}
				if tt.wantErr {
					b.Fatal("ActivateSteplibRefStep() succeeded unexpectedly")
				}

				//nolint:exhaustruct // StepInfo + ExecutablePath stay zero-valued for source activation
				want := result.ActivatedStep{
					StepYMLPath:      tmpDir + "/current_step.yml",
					ActivationType:   result.ActivationTypeSteplibSource,
					DidStepLibUpdate: !tt.didStepLibUpdateInWorkflow,
				}
				require.Equal(b, want, got)
			}
		})
	}
}
