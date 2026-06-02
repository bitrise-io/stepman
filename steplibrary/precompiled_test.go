package steplibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/spec"
)

// fakeFetcher implements httpfetch.Client by writing a fixed byte payload on
// DownloadWithHash. Get and Download are not used by the precompiled flow.
type fakeFetcher struct {
	payload []byte
	gotURL  string
	err     error
}

func (f *fakeFetcher) Get(_ context.Context, source string) (io.ReadCloser, error) {
	return nil, errors.New("Get not used by Steplib precompiled flow")
}

func (f *fakeFetcher) Download(_ context.Context, _, _ string) error {
	return errors.New("Download not used by Steplib precompiled flow")
}

func (f *fakeFetcher) DownloadWithHash(_ context.Context, destPath, url, expectedHash string) error {
	f.gotURL = url
	if f.err != nil {
		return f.err
	}
	actual := sha256OfBytes(f.payload)
	if actual != expectedHash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actual)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destPath, f.payload, 0o644)
}

func sha256OfBytes(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256-" + hex.EncodeToString(h[:])
}

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

	got, err := s.Activate(context.Background(), "script", "", ActivateOutputPaths{
		YMLPath:  filepath.Join(outDir, "current_step.yml"),
		CodePath: filepath.Join(outDir, "code"),
	})
	if err != nil {
		t.Fatalf("Activate unexpected error: %v", err)
	}

	wantBin := filepath.Join(outDir, "code", "script")
	if got.ExecutablePath != wantBin {
		t.Errorf("ExecutablePath = %q, want %q", got.ExecutablePath, wantBin)
	}
	if got.ActivationType != "steplib_executable" {
		t.Errorf("ActivationType = %q, want %q", got.ActivationType, "steplib_executable")
	}

	// Binary content matches the served payload.
	body, err := os.ReadFile(wantBin)
	if err != nil {
		t.Fatalf("read executable: %v", err)
	}
	if string(body) != string(payload) {
		t.Errorf("downloaded body = %q, want %q", body, payload)
	}

	// Binary is marked executable.
	info, err := os.Stat(wantBin)
	if err != nil {
		t.Fatalf("stat executable: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("executable bit not set on %s (perm=%o)", wantBin, info.Mode().Perm())
	}

	// step.yml is still written by the activation flow.
	if _, err := os.Stat(filepath.Join(outDir, "current_step.yml")); err != nil {
		t.Errorf("step.yml not written: %v", err)
	}

	// Downloader received the storage_uri-rooted URL.
	if !strings.HasSuffix(dl.gotURL, executables[currentPlatform()].StorageURI) {
		t.Errorf("downloader URL = %q, want suffix %q", dl.gotURL, executables[currentPlatform()].StorageURI)
	}
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
		log:              discardLogger{},
		steplibURI:       "https://github.com/bitrise-io/bitrise-steplib.git",
		api:              api,
		fileManager:      fileutil.NewFileManager(),
		fetcher:          &fakeFetcher{payload: payload},
		fetchSourceDirFn: func(_ context.Context, _ ResolvedStepVersion) (string, error) { return sourceDir, nil },
	}

	got, err := s.Activate(context.Background(), "script", "", ActivateOutputPaths{
		YMLPath:  filepath.Join(outDir, "current_step.yml"),
		CodePath: filepath.Join(outDir, "code"),
	})
	if err != nil {
		t.Fatalf("Activate unexpected error: %v", err)
	}
	if got.ExecutablePath != "" {
		t.Errorf("ExecutablePath = %q, want empty (precompiled failed → source path)", got.ExecutablePath)
	}
	if got.ActivationType != "steplib_source" {
		t.Errorf("ActivationType = %q, want %q", got.ActivationType, "steplib_source")
	}
}

// pointers returns a pointer to the given string. Avoids importing
// go-utils/pointers just for a single helper.
func strPtr(s string) *string { return &s }
