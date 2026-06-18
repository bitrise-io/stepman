package steplibrary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSteplib_Activate_Precompiled(t *testing.T) {
	t.Setenv(PrecompiledStepsExperimentEnv, "true")
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

	api := newFakeAPI()
	api.stepModel = map[string]models.StepModel{"script": stepModel}

	dl := &fakeFetcher{payload: payload}

	outDir := t.TempDir()
	client := &Client{
		log:         testLogger{t},
		steplibURI:  "https://github.com/bitrise-io/bitrise-steplib.git",
		api:         api,
		fileManager: fileutil.NewFileManager(),
		fetcher:     dl,
	}

	got, gotErr := client.Activate(context.Background(), "script", "", ActivateOutputPaths{
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

	api := newFakeAPI()
	api.stepModel = map[string]models.StepModel{"script": stepModel}

	outDir := t.TempDir()
	client := &Client{
		log:         testLogger{t},
		steplibURI:  "https://github.com/bitrise-io/bitrise-steplib.git",
		api:         api,
		fileManager: fileutil.NewFileManager(),
		fetcher:     &fakeFetcher{payload: payload},
		source:      stubSource{dir: sourceDir},
	}

	got, gotErr := client.Activate(context.Background(), "script", "", ActivateOutputPaths{
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

func TestBuildPrecompiledURLs(t *testing.T) {
	tests := []struct {
		name        string
		bases       []string
		executable  models.Executable
		want        []string
		wantErrText string
	}{
		{
			name:       "default list: GCS first, gateway second",
			bases:      PrecompiledStepsDefaultStorageURLs,
			executable: models.Executable{StorageURI: "steps/step1.tar.gz"},
			want: []string{
				"https://storage.googleapis.com/bitrise-steplib-storage/steps/step1.tar.gz",
				"https://storage-gateway.services.bitrise.io/steps/step1.tar.gz",
			},
		},
		{
			name:       "normalization: trailing slashes, leading StorageURI slash, spaces, empties",
			bases:      []string{"", " https://a.example.com/// ", "", " https://b.example.com///"},
			executable: models.Executable{StorageURI: "/steps/step3.tar.gz"},
			want: []string{
				"https://a.example.com/steps/step3.tar.gz",
				"https://b.example.com/steps/step3.tar.gz",
			},
		},
		{
			name:        "http URL is rejected",
			bases:       []string{"http://a.example.com"},
			executable:  models.Executable{StorageURI: "steps/step5.tar.gz"},
			wantErrText: "http URL is unsupported, please use https: http://a.example.com/steps/step5.tar.gz",
		},
		{
			name:        "all-empty list yields a configuration error",
			bases:       []string{"", "", ""},
			executable:  models.Executable{StorageURI: "steps/step6.tar.gz"},
			wantErrText: "no storage URLs configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildPrecompiledURLs(tt.bases, tt.executable)
			if tt.wantErrText != "" {
				require.EqualError(t, err, tt.wantErrText)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDownloadFromURLs(t *testing.T) {
	body := []byte("compiled-binary")
	hash := sha256OfBytes(body)
	fetcher := httpfetch.NewWithClient(http.DefaultClient)

	serveBytes := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(body)
		}))
	}
	serveStatus := func(code int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
	}

	t.Run("primary succeeds, secondary is not called", func(t *testing.T) {
		primary := serveBytes()
		defer primary.Close()
		secondaryHits := 0
		secondary := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			secondaryHits++
		}))
		defer secondary.Close()

		dest := filepath.Join(t.TempDir(), "bin")
		err := downloadFromURLs(context.Background(), fetcher, testLogger{t}, dest, hash, []string{primary.URL, secondary.URL})
		require.NoError(t, err)
		assert.Zero(t, secondaryHits, "secondary should not be hit when primary succeeds")

		got, err := os.ReadFile(dest)
		require.NoError(t, err)
		assert.Equal(t, body, got, "downloaded content")
	})

	t.Run("primary 404 falls back to secondary", func(t *testing.T) {
		primary := serveStatus(http.StatusNotFound)
		defer primary.Close()
		secondary := serveBytes()
		defer secondary.Close()

		dest := filepath.Join(t.TempDir(), "bin")
		err := downloadFromURLs(context.Background(), fetcher, testLogger{t}, dest, hash, []string{primary.URL, secondary.URL})
		require.NoError(t, err)

		got, err := os.ReadFile(dest)
		require.NoError(t, err)
		assert.Equal(t, body, got, "downloaded content from secondary")
	})

	t.Run("primary hash mismatch falls back to secondary", func(t *testing.T) {
		primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("corrupted"))
		}))
		defer primary.Close()
		secondary := serveBytes()
		defer secondary.Close()

		dest := filepath.Join(t.TempDir(), "bin")
		err := downloadFromURLs(context.Background(), fetcher, testLogger{t}, dest, hash, []string{primary.URL, secondary.URL})
		require.NoError(t, err)

		got, err := os.ReadFile(dest)
		require.NoError(t, err)
		assert.Equal(t, body, got, "fell back to secondary's valid content")
	})

	t.Run("all URLs fail and the error lists each one", func(t *testing.T) {
		primary := serveStatus(http.StatusNotFound)
		defer primary.Close()
		secondary := serveStatus(http.StatusForbidden)
		defer secondary.Close()

		dest := filepath.Join(t.TempDir(), "bin")
		err := downloadFromURLs(context.Background(), fetcher, testLogger{t}, dest, hash, []string{primary.URL, secondary.URL})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to download executable")
		assert.Contains(t, err.Error(), primary.URL)
		assert.Contains(t, err.Error(), secondary.URL)
	})

	t.Run("hash mismatch is an error", func(t *testing.T) {
		srv := serveBytes()
		defer srv.Close()

		dest := filepath.Join(t.TempDir(), "bin")
		err := downloadFromURLs(context.Background(), fetcher, testLogger{t}, dest, "sha256-deadbeef", []string{srv.URL})
		require.Error(t, err)
		assert.NoFileExists(t, dest, "no file should land on hash mismatch")
	})
}
