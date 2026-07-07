package steplib

import (
	"testing"

	"github.com/bitrise-io/stepman/stepid"
)

func Test_prepareStepLibForActivation(t *testing.T) {
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
			got, _, err := prepareStepLibForActivation(testLogger{t}, tt.stepIDData, false, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("prepareStepLibForActivation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got.Version != tt.wantVersion {
				t.Errorf("prepareStepLibForActivation() got = %v, want %v", got.Version, tt.wantVersion)
			}
		})
	}
}

// testLogger is a stepman.Logger backed by *testing.T.
type testLogger struct {
	t *testing.T
}

func (l testLogger) Debugf(format string, v ...any) { l.t.Logf(format, v...) }
func (l testLogger) Infof(format string, v ...any)  { l.t.Logf(format, v...) }
func (l testLogger) Warnf(format string, v ...any)  { l.t.Logf(format, v...) }
func (l testLogger) Errorf(format string, v ...any) { l.t.Logf(format, v...) }
