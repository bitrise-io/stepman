package steplibrary

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/spec"
)

type discardLogger struct{}

func (discardLogger) Debugf(string, ...any) {}
func (discardLogger) Errorf(string, ...any) {}
func (discardLogger) Warnf(string, ...any)  {}
func (discardLogger) Infof(string, ...any)  {}

type fakeAPI struct {
	FakeAPI
	ids               []string
	listErr           error
	latestVersions    map[string]spec.LatestPointer
	latestVersionsErr error
	allVersions       map[string][]string
	allVersionsErr    error
	groupInfo         map[string]spec.StepInfo
	groupInfoErr      error
	stepModel         map[string]models.StepModel
}

func (f fakeAPI) GetAllStepIDs(_ context.Context) ([]string, error) {
	return f.ids, f.listErr
}

func (f fakeAPI) GetLatestStepVersions(_ context.Context, id string) (spec.LatestPointer, error) {
	if f.latestVersionsErr != nil {
		return spec.LatestPointer{}, f.latestVersionsErr
	}
	v, ok := f.latestVersions[id]
	if !ok {
		return spec.LatestPointer{}, errors.New("not found")
	}
	return v, nil
}

func (f fakeAPI) GetAllStepVersions(_ context.Context, id string) ([]string, error) {
	if f.allVersionsErr != nil {
		return nil, f.allVersionsErr
	}
	v, ok := f.allVersions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (f fakeAPI) GetStepGroupInfo(ctx context.Context, id string) (spec.StepInfo, error) {
	if f.groupInfoErr != nil {
		return spec.StepInfo{}, f.groupInfoErr
	}
	if f.groupInfo != nil {
		v, ok := f.groupInfo[id]
		if !ok {
			return spec.StepInfo{}, errors.New("not found")
		}
		return v, nil
	}
	return f.FakeAPI.GetStepGroupInfo(ctx, id)
}

func (f fakeAPI) GetStepModel(ctx context.Context, step ResolvedStepVersion) (models.StepModel, error) {
	if f.stepModel != nil {
		v, ok := f.stepModel[step.ID]
		if !ok {
			return models.StepModel{}, errors.New("not found")
		}
		return v, nil
	}
	return f.FakeAPI.GetStepModel(ctx, step)
}


func writeSeedZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create seed zip: %v", err)
	}
	w := zip.NewWriter(f)
	entry, err := w.Create("step.txt")
	if err != nil {
		_ = w.Close()
		_ = f.Close()
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write([]byte("seed\n")); err != nil {
		_ = w.Close()
		_ = f.Close()
		t.Fatalf("write zip entry: %v", err)
	}
	if err := w.Close(); err != nil {
		_ = f.Close()
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
}

func TestSteplib_Activate(t *testing.T) {
	tmpDir := t.TempDir()
	sourceZIP := filepath.Join(tmpDir, "source-step.zip")
	writeSeedZip(t, sourceZIP)

	scriptOnly := fakeAPI{
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
			wantErr: "invalid step version constraint",
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
			name:        "major-locked resolves via latest_by_major",
			api:         scriptOnly,
			stepID:      "script",
			version:     "2",
			wantVersion: "2.4.1",
			wantLatest:  "3.0.0",
		},
		{
			name:    "major-locked with unknown major errors",
			api:     scriptOnly,
			stepID:  "script",
			version: "99",
			wantErr: "does not contain script step with major version 99",
		},
		{
			name:        "minor-locked resolves to highest matching patch",
			api:         scriptOnly,
			stepID:      "script",
			version:     "1.1",
			wantVersion: "1.1.5",
			wantLatest:  "3.0.0",
		},
		{
			name:    "minor-locked with no matching minor errors",
			api:     scriptOnly,
			stepID:  "script",
			version: "1.9",
			wantErr: "no version matches 1.9.x",
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
		{
			name: "group info error propagates",
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Steplib{
				log:              discardLogger{},
				steplibURI:       "https://github.com/bitrise-io/bitrise-steplib.git",
				api:              tt.api,
				fileManager:      fileutil.NewFileManager(),
				fetchSourceZIPFn: func(_ context.Context, _ ResolvedStepVersion) (string, error) { return sourceZIP, nil },
			}
			outDir := t.TempDir()
			outPaths := ActivateOutputPaths{
				YMLPath:  filepath.Join(outDir, "current_step.yml"),
				CodePath: filepath.Join(outDir, "code"),
			}
			got, err := s.Activate(context.Background(), tt.stepID, tt.version, outPaths)
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
			if got.StepInfo.GroupInfo.Maintainer != "bitrise" {
				t.Errorf("StepInfo.GroupInfo.Maintainer = %q, want %q", got.StepInfo.GroupInfo.Maintainer, "bitrise")
			}
			if got.StepInfo.GroupInfo.AssetURLs["icon.svg"] != "assets/icon.svg" {
				t.Errorf("StepInfo.GroupInfo.AssetURLs[icon.svg] = %q, want %q",
					got.StepInfo.GroupInfo.AssetURLs["icon.svg"], "assets/icon.svg")
			}
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
		if got.Maintainer != "bitrise" {
			t.Errorf("Maintainer = %q, want %q", got.Maintainer, "bitrise")
		}
		if got.RemovalDate != "" || got.DeprecateNotes != "" {
			t.Errorf("expected empty deprecation, got RemovalDate=%q, DeprecateNotes=%q", got.RemovalDate, got.DeprecateNotes)
		}
		if got.AssetURLs["icon.svg"] != "assets/icon.svg" {
			t.Errorf("AssetURLs not carried through: %v", got.AssetURLs)
		}
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
		if got.RemovalDate != "2025-12-31" {
			t.Errorf("RemovalDate = %q, want %q", got.RemovalDate, "2025-12-31")
		}
		if got.DeprecateNotes != "Replaced by `new-step`." {
			t.Errorf("DeprecateNotes = %q, want %q", got.DeprecateNotes, "Replaced by `new-step`.")
		}
		if got.Maintainer != "community" {
			t.Errorf("Maintainer = %q, want %q", got.Maintainer, "community")
		}
	})
}
