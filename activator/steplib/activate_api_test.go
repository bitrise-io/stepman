package steplib

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepid"
	"github.com/bitrise-io/stepman/steplibrary"
	"github.com/bitrise-io/stepman/steplibrary/steplibindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise ActivateStep's V2 (API) dispatch logic hermetically: a
// steplibrary.Client is pointed at a local httptest server that serves a small,
// wire-faithful inventory (built from the real steplibindex/models structs and
// addressed via the real Path helpers, so it can't structurally drift from the
// reader). Source archives are wired back to the same server, so the whole
// activation — resolve → write step.yml → precompiled gate → source fetch — runs
// without touching the network or the git-cloned steplib.

const testSteplibURL = "https://github.com/bitrise-io/bitrise-steplib.git"

type apiTestLogger struct{ t *testing.T }

func (l apiTestLogger) Debugf(f string, a ...any) { l.t.Logf("DEBUG "+f, a...) }
func (l apiTestLogger) Errorf(f string, a ...any) { l.t.Logf("ERROR "+f, a...) }
func (l apiTestLogger) Warnf(f string, a ...any)  { l.t.Logf("WARN "+f, a...) }
func (l apiTestLogger) Infof(f string, a ...any)  { l.t.Logf("INFO "+f, a...) }

// helloStepVersions is newest-first, matching the versions.json contract.
var helloStepVersions = []string{"2.0.0", "1.1.0", "1.0.0"}

// serveHelloStepInventory writes and serves a minimal V2 inventory for
// "hello-step" and returns the server. The source archive for every version is
// served from the same server (keyed by step ID + version, the correct layout),
// so source activation resolves entirely against localhost.
func serveHelloStepInventory(t *testing.T) *httptest.Server {
	t.Helper()
	root := t.TempDir()

	writeInventoryJSON(t, root, steplibindex.StepIDsPath(),
		steplibindex.StepIDs{StepIDs: []string{"hello-step"}})

	latestPath, err := steplibindex.LatestPointerPath("hello-step")
	require.NoError(t, err)
	writeInventoryJSON(t, root, latestPath, steplibindex.LatestPointer{
		StepID:        "hello-step",
		Latest:        "2.0.0",
		LatestByMajor: map[string]string{"1": "1.1.0", "2": "2.0.0"},
	})

	versionsPath, err := steplibindex.VersionsPath("hello-step")
	require.NoError(t, err)
	writeInventoryJSON(t, root, versionsPath,
		steplibindex.Versions{StepID: "hello-step", Versions: helloStepVersions})

	infoPath, err := steplibindex.StepInfoPath("hello-step")
	require.NoError(t, err)
	writeInventoryJSON(t, root, infoPath,
		steplibindex.StepInfo{Maintainer: "bitrise", AssetURLs: []string{}})

	for _, v := range helloStepVersions {
		stepJSONPath, err := steplibindex.StepJSONPath("hello-step", v)
		require.NoError(t, err)
		title := "Hello Step " + v
		writeInventoryJSON(t, root, stepJSONPath, models.StepModel{
			Title: &title,
			Source: &models.StepSourceModel{
				Git:    "https://github.com/example/hello-step.git",
				Commit: "cccc3333cccc3333cccc3333cccc3333cccc3333",
			},
		})
	}

	srv := httptest.NewServer(http.FileServer(http.Dir(root)))
	t.Cleanup(srv.Close)

	// meta.json declares a single zip download base pointing back at this server.
	// Written after the server starts so we know its URL; http.FileServer reads
	// lazily, so it's served on the next request. Only a zip location is listed
	// (no git) so source activation must resolve against localhost, never a clone.
	writeInventoryJSON(t, root, steplibindex.MetaPath(), steplibindex.Meta{
		FormatVersion: steplibindex.FormatVersion,
		UpdatedAt:     time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
		DownloadLocations: []models.DownloadLocationModel{
			{Type: "zip", Src: srv.URL + "/archive/"},
		},
	})

	// BuildStepSourceDownloadLocations forms "<base><stepID>/<version>/step.zip",
	// so the archive for each version lives under archive/hello-step/<version>/.
	for _, v := range helloStepVersions {
		writeStepSourceZip(t, filepath.Join(root, "archive", "hello-step", v, "step.zip"))
	}

	return srv
}

func writeInventoryJSON(t *testing.T, root string, p steplibindex.Path, v any) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(p.FS()))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	data, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(full, data, 0o644))
}

// writeStepSourceZip writes a step source archive whose single entry is a marker
// file, so a successful activation can be verified by the marker's presence in
// the destination.
func writeStepSourceZip(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("activated_marker.txt")
	require.NoError(t, err)
	_, err = w.Write([]byte("activated from source"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
}

func TestActivateStep_APISource_ExactVersion(t *testing.T) {
	srv := serveHelloStepInventory(t)
	log := apiTestLogger{t}
	t.Setenv(precompiledStepsEnv, "false")

	destination := t.TempDir()
	workDir := t.TempDir()
	stepYML := filepath.Join(workDir, "current_step.yml")

	id := stepid.CanonicalID{SteplibSource: testSteplibURL, IDorURI: "hello-step", Version: "2.0.0"}

	resolved, err := ActivateStep(id, destination, stepYML, log, false, steplibrary.New(log, srv.URL))
	require.NoError(t, err)

	assert.Equal(t, "hello-step", resolved.StepInfo.ID)
	assert.Equal(t, "2.0.0", resolved.StepInfo.Version, "exact request resolves to itself")
	assert.Equal(t, "2.0.0", resolved.StepInfo.OriginalVersion)
	assert.Empty(t, resolved.ExecPath, "source activation must not yield an executable path")

	// writeStepYML placed a step.yml at the requested path.
	require.FileExists(t, stepYML, "step.yml should be written to the work dir")

	// The source archive was fetched and unpacked into the destination.
	require.FileExists(t, filepath.Join(destination, "activated_marker.txt"),
		"step source should be materialized into the destination dir")
}

func TestActivateStep_APISource_MajorLockResolves(t *testing.T) {
	srv := serveHelloStepInventory(t)
	log := apiTestLogger{t}
	t.Setenv(precompiledStepsEnv, "false")

	id := stepid.CanonicalID{SteplibSource: testSteplibURL, IDorURI: "hello-step", Version: "1"}

	resolved, err := ActivateStep(id, t.TempDir(), filepath.Join(t.TempDir(), "current_step.yml"), log, false, steplibrary.New(log, srv.URL))
	require.NoError(t, err)

	assert.Equal(t, "1.1.0", resolved.StepInfo.Version, "major lock 1 resolves to the highest 1.x")
	assert.Equal(t, "1", resolved.StepInfo.OriginalVersion)
	assert.Empty(t, resolved.ExecPath)
}

func TestActivateStep_APISource_NonexistentVersionFails(t *testing.T) {
	srv := serveHelloStepInventory(t)
	log := apiTestLogger{t}
	t.Setenv(precompiledStepsEnv, "false")

	id := stepid.CanonicalID{SteplibSource: testSteplibURL, IDorURI: "hello-step", Version: "99.99.99"}

	_, err := ActivateStep(id, t.TempDir(), filepath.Join(t.TempDir(), "current_step.yml"), log, false, steplibrary.New(log, srv.URL))
	require.Error(t, err, "a version not in the inventory must fail resolution")
}

// hello-step ships no prebuilt executables, so with the precompiled experiment
// enabled ActivateStep must still fall back to source activation.
func TestActivateStep_APIPrecompiledEnabled_FallsBackToSource(t *testing.T) {
	srv := serveHelloStepInventory(t)
	log := apiTestLogger{t}
	t.Setenv(precompiledStepsEnv, "true")

	destination := t.TempDir()
	id := stepid.CanonicalID{SteplibSource: testSteplibURL, IDorURI: "hello-step", Version: "2.0.0"}

	resolved, err := ActivateStep(id, destination, filepath.Join(t.TempDir(), "current_step.yml"), log, false, steplibrary.New(log, srv.URL))
	require.NoError(t, err)

	assert.Empty(t, resolved.ExecPath, "a step without executables must fall back to source")
	require.FileExists(t, filepath.Join(destination, "activated_marker.txt"))
}
