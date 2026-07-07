package activator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/stepid"
	"github.com/stretchr/testify/require"
)

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

				got, gotErr := ActivateSteplibRefStep(logger, tt.id, stepYMLCopyPth, tmpDir, tt.didStepLibUpdateInWorkflow, tt.isOfflineMode)
				if gotErr != nil {
					if !tt.wantErr {
						b.Errorf("ActivateSteplibRefStep() failed: %v", gotErr)
					}
					return
				}
				if tt.wantErr {
					b.Fatal("ActivateSteplibRefStep() succeeded unexpectedly")
				}

				require.Equal(b, tmpDir+"/current_step.yml", got.StepYMLPath)
				require.Equal(b, ActivationTypeSteplibSource, got.ActivationType)
				require.Equal(b, !tt.didStepLibUpdateInWorkflow, got.DidStepLibUpdate)
				require.Equal(b, tt.id.IDorURI, got.StepInfo.ID)
			}
		})
	}
}
