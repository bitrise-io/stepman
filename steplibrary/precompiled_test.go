package steplibrary

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSteplib_Activate_Precompiled(t *testing.T) {
	payload := []byte("#!/bin/sh\necho hello\n")
	hash := sha256OfBytes(payload)

	executables := models.Executables{
		currentPlatform(): models.Executable{
			StorageURI: "steps/script/3.0.0/bin/script-" + currentPlatform(),
			Hash:       hash,
		},
	}
	//nolint:exhaustruct // only Executables and Title matter for this test
	stepModel := models.StepModel{
		Title:       strPtr("Script"),
		Executables: &executables,
	}

	api := fakeAPI{
		ids: []string{"script"},
		latestVersions: map[string]spec.LatestPointer{
			"script": {StepID: "script", Latest: "3.0.0"},
		},
		stepModel: map[string]models.StepModel{"script": stepModel},
	}

	dl := &fakeFetcher{payload: payload}

	outDir := t.TempDir()
	s := &Steplib{
		log:         discardLogger{},
		steplibURI:  "https://github.com/bitrise-io/bitrise-steplib.git",
		api:         api,
		fileManager: fileutil.NewFileManager(),
		fetcher:     dl,
	}

	got, gotErr := s.Activate(context.Background(), "script", "", ActivateOutputPaths{
		YMLPath:  filepath.Join(outDir, "current_step.yml"),
		CodePath: filepath.Join(outDir, "code"),
	})
	require.NoError(t, gotErr, "Activate")

	wantBin := filepath.Join(outDir, "code", "script")
	assert.Equal(t, wantBin, got.ExecutablePath, "ExecutablePath")
	assert.Equal(t, "steplib_executable", string(got.ActivationType), "ActivationType")

	// Binary content matches the served payload.
	body, gotErr := os.ReadFile(wantBin)
	require.NoError(t, gotErr, "read executable")
	assert.Equal(t, payload, body, "downloaded binary content")

	// Binary is marked executable.
	info, gotErr := os.Stat(wantBin)
	require.NoError(t, gotErr, "stat executable")
	assert.NotZero(t, info.Mode().Perm()&0o111, "executable bit not set (perm=%o)", info.Mode().Perm())

	// step.yml is still written by the activation flow.
	_, gotErr = os.Stat(filepath.Join(outDir, "current_step.yml"))
	assert.NoError(t, gotErr, "step.yml not written")

	// Downloader received the storage_uri-rooted URL.
	assert.Truef(t, strings.HasSuffix(dl.gotURL, executables[currentPlatform()].StorageURI),
		"downloader URL = %q, want suffix %q", dl.gotURL, executables[currentPlatform()].StorageURI)
}

func TestSteplib_Activate_PrecompiledHashMismatch_FallsBackToSource(t *testing.T) {
	payload := []byte("bad-bytes")

	executables := models.Executables{
		currentPlatform(): models.Executable{
			StorageURI: "steps/script/3.0.0/bin/script-" + currentPlatform(),
			Hash:       "sha256-deadbeef", // intentional mismatch
		},
	}
	//nolint:exhaustruct
	stepModel := models.StepModel{
		Title:       strPtr("Script"),
		Executables: &executables,
	}

	// Seed a real source dir for the fallback path.
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source-step")
	writeSeedDir(t, sourceDir)

	api := fakeAPI{
		ids: []string{"script"},
		latestVersions: map[string]spec.LatestPointer{
			"script": {StepID: "script", Latest: "3.0.0"},
		},
		stepModel: map[string]models.StepModel{"script": stepModel},
	}

	outDir := t.TempDir()
	s := &Steplib{
		log:         discardLogger{},
		steplibURI:  "https://github.com/bitrise-io/bitrise-steplib.git",
		api:         api,
		fileManager: fileutil.NewFileManager(),
		fetcher:     &fakeFetcher{payload: payload},
		source:      stubSource{dir: sourceDir},
	}

	got, gotErr := s.Activate(context.Background(), "script", "", ActivateOutputPaths{
		YMLPath:  filepath.Join(outDir, "current_step.yml"),
		CodePath: filepath.Join(outDir, "code"),
	})
	require.NoError(t, gotErr, "Activate")
	assert.Empty(t, got.ExecutablePath, "want empty ExecutablePath (precompiled failed → source path)")
	assert.Equal(t, "steplib_source", string(got.ActivationType), "ActivationType")
}

// pointers returns a pointer to the given string. Avoids importing
// go-utils/pointers just for a single helper.
func strPtr(s string) *string { return &s }
