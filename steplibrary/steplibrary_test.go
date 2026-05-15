package steplibrary

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/fileutil"
)

type discardLogger struct{}

func (discardLogger) Debugf(string, ...any) {}
func (discardLogger) Errorf(string, ...any) {}
func (discardLogger) Warnf(string, ...any)  {}
func (discardLogger) Infof(string, ...any)  {}

type fakeAPI struct {
	MockAPI
	ids               []string
	listErr           error
	latestVersions    map[string]StepVersionsLatest
	latestVersionsErr error
	ymlSourcePath     string
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

func (f fakeAPI) GetStepYMLPath(step ResolvedStepVersion) (string, error) {
	if f.ymlSourcePath != "" {
		return f.ymlSourcePath, nil
	}
	return f.MockAPI.GetStepYMLPath(step)
}

func TestSteplib_Activate(t *testing.T) {
	tmpDir := t.TempDir()
	sourceYML := filepath.Join(tmpDir, "source-step.yml")
	if err := os.WriteFile(sourceYML, []byte("# stub step.yml\n"), 0o644); err != nil {
		t.Fatalf("seed source step.yml: %v", err)
	}

	scriptOnly := fakeAPI{
		ids:            []string{"script"},
		latestVersions: map[string]StepVersionsLatest{"script": {Latest: "3.0.0"}},
		ymlSourcePath:  sourceYML,
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
			wantErr: "steplib does not contain xcode-test step",
		},
		{
			name:    "invalid version constraint",
			api:     scriptOnly,
			stepID:  "script",
			version: "not-a-version",
			wantErr: "invalid step `script` version constraint",
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
			wantErr: "fetching avaialble step IDs",
		},
		{
			name: "latest versions error propagates",
			api: fakeAPI{
				ids:               []string{"script"},
				latestVersionsErr: errors.New("kaboom"),
			},
			stepID:  "script",
			wantErr: "fetching latest versions of `script`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Steplib{
				log:         discardLogger{},
				steplibURI:  "https://github.com/bitrise-io/bitrise-steplib.git",
				api:         tt.api,
				fileManager: fileutil.NewFileManager(),
			}
			outDir := t.TempDir()
			outPaths := ActivateOutputPaths{
				YMLPath:  filepath.Join(outDir, "current_step.yml"),
				CodePath: filepath.Join(outDir, "code"),
			}
			got, err := s.Activate(tt.stepID, tt.version, outPaths)
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
			if got.StepInfo.ID != tt.stepID {
				t.Errorf("StepInfo.ID = %q, want %q", got.StepInfo.ID, tt.stepID)
			}
			if got.StepInfo.Version != tt.wantVersion {
				t.Errorf("StepInfo.Version = %q, want %q", got.StepInfo.Version, tt.wantVersion)
			}
			if got.StepInfo.LatestVersion != tt.wantLatest {
				t.Errorf("StepInfo.LatestVersion = %q, want %q", got.StepInfo.LatestVersion, tt.wantLatest)
			}
			if got.StepInfo.OriginalVersion != tt.version {
				t.Errorf("StepInfo.OriginalVersion = %q, want %q", got.StepInfo.OriginalVersion, tt.version)
			}
		})
	}
}
