package specgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/bitrise-io/stepman/internal/specfixtures"
	"github.com/bitrise-io/stepman/steplibrary/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedTime is used everywhere so tests don't depend on time.Now.
var fixedTime = time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

type testLogger struct{ t *testing.T }

func (l testLogger) Debugf(format string, v ...any) { l.t.Logf("DEBUG "+format, v...) }
func (l testLogger) Infof(format string, v ...any)  { l.t.Logf("INFO "+format, v...) }
func (l testLogger) Warnf(format string, v ...any)  { l.t.Logf("WARN "+format, v...) }
func (l testLogger) Errorf(format string, v ...any) { l.t.Logf("ERROR "+format, v...) }

type exampleLogger struct{}

func (exampleLogger) Debugf(string, ...any) {}
func (exampleLogger) Infof(string, ...any)  {}
func (exampleLogger) Warnf(string, ...any)  {}
func (exampleLogger) Errorf(string, ...any) {}

// runGenerateFromSteplibClone runs the generator against the checked-in
// testdata using a fresh temp output dir. Returns the output dir for assertions.
func runGenerateFromSteplibClone(t *testing.T) string {
	t.Helper()
	out := t.TempDir()
	_, gotErr := generateFromSteplibClone(
		specfixtures.SteplibClone(),
		out,
		Options{GeneratedAt: fixedTime, SteplibCommitSHA: "deadbeefcafef00d"},
		testLogger{t},
	)
	require.NoError(t, gotErr, "generateFromSteplibClone")
	return out
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	bytes, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	require.NoError(t, json.Unmarshal(bytes, into), "unmarshal %s", path)
}

func TestGenerator_meta(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var meta spec.Meta
	readJSON(t, filepath.Join(out, "meta.json"), &meta)

	assert.Equal(t, spec.FormatVersion, meta.FormatVersion, "FormatVersion")
	assert.Equal(t, 2, meta.FormatVersion, "FormatVersion literal")
	assert.Equal(t, fixedTime, meta.UpdatedAt, "UpdatedAt")
	assert.Equal(t, "deadbeefcafef00d", meta.SteplibCommitSHA, "SteplibCommitSHA")
	assert.Equal(t, "https://github.com/example/test-steplib.git", meta.SteplibSource, "SteplibSource")
	require.Len(t, meta.DownloadLocations, 2, "DownloadLocations")
	assert.Equal(t, "zip", meta.DownloadLocations[0].Type, "DownloadLocations[0].Type")
	assert.Equal(t, "https://archives.example.com/", meta.DownloadLocations[0].Src, "DownloadLocations[0].Src")
	assert.Equal(t, "git", meta.DownloadLocations[1].Type, "DownloadLocations[1].Type")
}

func TestGenerator_step_ids_sorted(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var ids spec.StepIDs
	readJSON(t, filepath.Join(out, "spec/step_ids.json"), &ids)

	want := []string{"bash-step", "deprecated-step", "hello-step", "multi-platform-step"}
	assert.Equal(t, want, ids.StepIDs, "step IDs")
	assert.True(t, sort.StringsAreSorted(ids.StepIDs), "step IDs are sorted")
}

func TestGenerator_stats(t *testing.T) {
	out := t.TempDir()
	stats, gotErr := generateFromSteplibClone(
		specfixtures.SteplibClone(),
		out,
		Options{GeneratedAt: fixedTime},
		testLogger{t},
	)
	require.NoError(t, gotErr, "generateFromSteplibClone")

	assert.Equal(t, 4, stats.StepCount, "StepCount")
	// hello-step:3 + deprecated:1 + multi-platform:1 + bash:1
	assert.Equal(t, 6, stats.VersionCount, "VersionCount")
	// step-level: bash(2) + deprecated(2) + hello(5) + multi-platform(3) = 12
	// spec/:      step_ids + 4×(latest+versions) = 9
	// meta.json:  1
	assert.Equal(t, 22, stats.FilesWritten, "FilesWritten")
	assert.Positive(t, stats.BytesWritten, "BytesWritten")
}

func TestGenerator_authored_files_are_owner_only(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	// Files and dirs the generator authors get owner-only perms (no group/other).
	// Copied assets keep their source perms and are covered separately.
	metaInfo, err := os.Stat(filepath.Join(out, "meta.json"))
	require.NoError(t, err, "stat meta.json")
	assert.Equal(t, os.FileMode(0o600), metaInfo.Mode().Perm(), "authored file is 0600")

	dirInfo, err := os.Stat(filepath.Join(out, "spec"))
	require.NoError(t, err, "stat spec/ dir")
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "authored dir is 0700")
}

func TestGenerator_publish_replaces_existing_tree(t *testing.T) {
	out := t.TempDir()
	// Seed a stale file that a correct (atomic, wholesale) publish must drop.
	stale := filepath.Join(out, "spec", "stale.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(stale), 0o755), "seed stale dir")
	require.NoError(t, os.WriteFile(stale, []byte("{}"), 0o644), "seed stale file")

	_, err := generateFromSteplibClone(
		specfixtures.SteplibClone(),
		out,
		Options{GeneratedAt: fixedTime, SteplibCommitSHA: ""},
		testLogger{t},
	)
	require.NoError(t, err, "generate")

	_, statErr := os.Stat(stale)
	assert.True(t, os.IsNotExist(statErr), "stale file should be gone after wholesale replace; got err=%v", statErr)
	_, statErr = os.Stat(filepath.Join(out, "meta.json"))
	assert.NoError(t, statErr, "meta.json present after publish")
}

func TestWithDefaults_fills_zero_generated_at(t *testing.T) {
	before := time.Now()
	opts := withDefaults(Options{SteplibCommitSHA: "abc"})
	after := time.Now()

	assert.WithinRange(t, opts.GeneratedAt, before, after, "GeneratedAt defaulted to now")
	assert.Equal(t, "abc", opts.SteplibCommitSHA, "non-zero fields must not be overwritten")
}

func TestWithDefaults_preserves_non_zero_generated_at(t *testing.T) {
	opts := withDefaults(Options{GeneratedAt: fixedTime})
	assert.Equal(t, fixedTime, opts.GeneratedAt, "GeneratedAt preserved")
}

func Example_generateFromSteplibClone() {
	tmp, err := os.MkdirTemp("", "specv2-example-")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	stats, err := generateFromSteplibClone(specfixtures.SteplibClone(), tmp, Options{GeneratedAt: fixedTime}, exampleLogger{})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("steps=%d versions=%d", stats.StepCount, stats.VersionCount)
	// Output: steps=4 versions=6
}
