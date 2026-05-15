package steplibrary

import (
	"errors"
	"strings"
	"testing"
)

type discardLogger struct{}

func (discardLogger) Debugf(string, ...any) {}
func (discardLogger) Errorf(string, ...any) {}
func (discardLogger) Warnf(string, ...any)  {}
func (discardLogger) Infof(string, ...any)  {}

type fakeAPI struct {
	ids               []string
	listErr           error
	latestVersions    map[string]StepVersionsLatest
	latestVersionsErr error
}

func (f fakeAPI) GetAllStepIDs() ([]string, error) {
	return f.ids, f.listErr
}

func (f fakeAPI) GetLatestStepVersions(id string) (StepVersionsLatest, error) {
	if f.latestVersionsErr != nil {
		return StepVersionsLatest{}, f.latestVersionsErr
	}
	v, ok := f.latestVersions[id]
	if !ok {
		return StepVersionsLatest{}, errors.New("not found")
	}
	return v, nil
}

func TestSteplib_Activate(t *testing.T) {
	scriptOnly := fakeAPI{
		ids:            []string{"script"},
		latestVersions: map[string]StepVersionsLatest{"script": {Latest: "3.0.0"}},
	}

	tests := []struct {
		name        string
		api         API
		stepID      string
		version     string
		wantVersion string
		wantLatest  string
		wantErr     string
	}{
		{
			name:    "empty step id returns validation error",
			api:     fakeAPI{},
			stepID:  "",
			wantErr: "missing required input",
		},
		{
			name:    "step not in collection",
			api:     fakeAPI{ids: []string{"script"}},
			stepID:  "xcode-test",
			wantErr: "collection doesn't contain step",
		},
		{
			name:    "invalid version constraint",
			api:     scriptOnly,
			stepID:  "script",
			version: "not-a-version",
			wantErr: "invalid step version",
		},
		{
			name:        "empty version resolves to latest",
			api:         scriptOnly,
			stepID:      "script",
			version:     "",
			wantVersion: "3.0.0",
			wantLatest:  "3.0.0",
		},
		{
			name:        "fixed version resolves to requested",
			api:         scriptOnly,
			stepID:      "script",
			version:     "1.2.3",
			wantVersion: "1.2.3",
			wantLatest:  "3.0.0",
		},
		{
			name:    "major-locked not yet supported",
			api:     scriptOnly,
			stepID:  "script",
			version: "1",
			wantErr: "not yet supported",
		},
		{
			name:    "minor-locked not yet supported",
			api:     scriptOnly,
			stepID:  "script",
			version: "1.2",
			wantErr: "not yet supported",
		},
		{
			name:    "list error propagates",
			api:     fakeAPI{listErr: errors.New("boom")},
			stepID:  "script",
			wantErr: "fetching steps failed",
		},
		{
			name: "latest versions error propagates",
			api: fakeAPI{
				ids:               []string{"script"},
				latestVersionsErr: errors.New("kaboom"),
			},
			stepID:  "script",
			wantErr: "fetching versions for script failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Steplib{
				log:        discardLogger{},
				steplibURI: "https://github.com/bitrise-io/bitrise-steplib.git",
				api:        tt.api,
			}
			got, err := s.Activate(tt.stepID, tt.version)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Activate(%q, %q) = nil error, want error containing %q", tt.stepID, tt.version, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Activate(%q, %q) error = %q, want substring %q", tt.stepID, tt.version, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Activate(%q, %q) unexpected error: %v", tt.stepID, tt.version, err)
			}
			if got.ID != tt.stepID {
				t.Errorf("ID = %q, want %q", got.ID, tt.stepID)
			}
			if got.Version != tt.wantVersion {
				t.Errorf("Version = %q, want %q", got.Version, tt.wantVersion)
			}
			if got.LatestVersion != tt.wantLatest {
				t.Errorf("LatestVersion = %q, want %q", got.LatestVersion, tt.wantLatest)
			}
			if got.OriginalVersion != tt.version {
				t.Errorf("OriginalVersion = %q, want %q", got.OriginalVersion, tt.version)
			}
		})
	}
}
