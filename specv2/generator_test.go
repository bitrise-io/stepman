package specv2

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/bitrise-io/stepman/models"
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

// runGenerate runs the generator against the checked-in testdata using a
// fresh temp output dir. Returns the output dir for assertions.
func runGenerate(t *testing.T) string {
	t.Helper()
	out := t.TempDir()
	_, err := Generate(
		"testdata/input",
		out,
		Options{GeneratedAt: fixedTime, SteplibCommitSHA: "deadbeefcafef00d"},
		testLogger{t},
	)
	require.NoError(t, err)
	return out
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	bytes, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	require.NoError(t, json.Unmarshal(bytes, into), "unmarshal %s", path)
}

func TestGenerator_meta(t *testing.T) {
	out := runGenerate(t)

	var meta MetaJSON
	readJSON(t, filepath.Join(out, "meta.json"), &meta)

	assert.Equal(t, FormatVersion, meta.FormatVersion)
	assert.Equal(t, 2, meta.FormatVersion)
	assert.Equal(t, fixedTime, meta.UpdatedAt)
	assert.Equal(t, "deadbeefcafef00d", meta.SteplibCommitSHA)
	assert.Equal(t, "https://github.com/example/test-steplib.git", meta.SteplibSource)
	require.Len(t, meta.DownloadLocations, 2)
	assert.Equal(t, "zip", meta.DownloadLocations[0].Type)
	assert.Equal(t, "https://archives.example.com/", meta.DownloadLocations[0].Src)
	assert.Equal(t, "git", meta.DownloadLocations[1].Type)
}

func TestGenerator_step_ids_sorted(t *testing.T) {
	out := runGenerate(t)

	var ids StepIDsJSON
	readJSON(t, filepath.Join(out, "spec/step_ids.json"), &ids)

	want := []string{"bash-step", "deprecated-step", "hello-step", "multi-platform-step", "no-info-step"}
	assert.Equal(t, want, ids.StepIDs)
	assert.True(t, sort.StringsAreSorted(ids.StepIDs))
}

func TestGenerator_normal_step_info_and_asset_copy(t *testing.T) {
	out := runGenerate(t)

	var info StepInfoJSON
	readJSON(t, filepath.Join(out, "steps/hello-step/step-info.json"), &info)

	assert.Equal(t, "bitrise", info.Maintainer)
	assert.Nil(t, info.Deprecation)
	assert.Equal(t, map[string]string{"icon.svg": "assets/icon.svg"}, info.AssetURLs)

	// Asset file copied
	_, err := os.Stat(filepath.Join(out, "steps/hello-step/assets/icon.svg"))
	assert.NoError(t, err)
}

func TestGenerator_deprecated_step(t *testing.T) {
	out := runGenerate(t)

	var info StepInfoJSON
	readJSON(t, filepath.Join(out, "steps/deprecated-step/step-info.json"), &info)

	assert.Equal(t, "community", info.Maintainer)
	require.NotNil(t, info.Deprecation)
	assert.Equal(t, "2026-12-31", info.Deprecation.RemovalDate)
	assert.Contains(t, info.Deprecation.Notes, "Replaced by hello-step")
	// no assets dir → no asset_urls
	assert.Empty(t, info.AssetURLs)
}

func TestGenerator_no_info_step_skips_step_info_file(t *testing.T) {
	out := runGenerate(t)

	// step-info.json must NOT exist for a step without step-info.yml AND no assets.
	_, err := os.Stat(filepath.Join(out, "steps/no-info-step/step-info.json"))
	assert.True(t, os.IsNotExist(err), "step-info.json should not exist for no-info-step; got err=%v", err)

	// But step.json must exist.
	_, err = os.Stat(filepath.Join(out, "steps/no-info-step/1.0.0/step.json"))
	assert.NoError(t, err)
}

func TestGenerator_multi_platform_executables(t *testing.T) {
	out := runGenerate(t)

	// step.json is a models.StepModel marshaled as JSON — same shape as V1 step.yml.
	var step models.StepModel
	readJSON(t, filepath.Join(out, "steps/multi-platform-step/3.2.1/step.json"), &step)

	require.NotNil(t, step.Executables)
	require.Len(t, *step.Executables, 2)

	// StorageURI is preserved verbatim from V1 step.yml — a relative path.
	// The client (today's activator) is responsible for resolving it against
	// the configured binary storage base.
	darwinArm := (*step.Executables)["darwin-arm64"]
	assert.Equal(t,
		"steps/multi-platform-step/3.2.1/bin/multi-platform-step-darwin-arm64",
		darwinArm.StorageURI,
	)
	assert.Equal(t,
		"sha256-1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa",
		darwinArm.Hash,
	)

	linuxAmd := (*step.Executables)["linux-amd64"]
	assert.Equal(t,
		"steps/multi-platform-step/3.2.1/bin/multi-platform-step-linux-amd64",
		linuxAmd.StorageURI,
	)
}

func TestGenerator_bash_step_has_no_executables(t *testing.T) {
	out := runGenerate(t)

	var step models.StepModel
	readJSON(t, filepath.Join(out, "steps/bash-step/1.0.0/step.json"), &step)

	assert.Nil(t, step.Executables)
	require.NotNil(t, step.Toolkit)
	require.NotNil(t, step.Toolkit.Bash)
	assert.Equal(t, "step.sh", step.Toolkit.Bash.EntryFile)
}

func TestGenerator_per_step_latest_pointer(t *testing.T) {
	out := runGenerate(t)

	var latest LatestPointerJSON
	readJSON(t, filepath.Join(out, "spec/steps/hello-step/latest.json"), &latest)

	assert.Equal(t, "hello-step", latest.StepID)
	assert.Equal(t, "2.0.0", latest.Latest)
	assert.Equal(t, map[string]string{
		"1": "1.1.0",
		"2": "2.0.0",
	}, latest.LatestByMajor)
}

func TestGenerator_per_step_versions_newest_first(t *testing.T) {
	out := runGenerate(t)

	var versions VersionsJSON
	readJSON(t, filepath.Join(out, "spec/steps/hello-step/versions.json"), &versions)

	assert.Equal(t, "hello-step", versions.StepID)
	assert.Equal(t, "2.0.0", versions.Latest)
	require.Len(t, versions.Versions, 3)
	assert.Equal(t, "2.0.0", versions.Versions[0].Version)
	assert.Equal(t, "1.1.0", versions.Versions[1].Version)
	assert.Equal(t, "1.0.0", versions.Versions[2].Version)

	// has_executable is false for all hello-step versions (it's a bash step).
	for _, v := range versions.Versions {
		assert.False(t, v.HasExecutable, "version %s should not have an executable", v.Version)
	}
	// commit + published_at are populated.
	assert.Equal(t, "cccc3333cccc3333cccc3333cccc3333cccc3333", versions.Versions[0].Commit)
	require.NotNil(t, versions.Versions[0].PublishedAt)
	assert.Equal(t, 2025, versions.Versions[0].PublishedAt.Year())
}

func TestGenerator_per_step_versions_has_executable(t *testing.T) {
	out := runGenerate(t)

	var versions VersionsJSON
	readJSON(t, filepath.Join(out, "spec/steps/multi-platform-step/versions.json"), &versions)

	require.Len(t, versions.Versions, 1)
	assert.True(t, versions.Versions[0].HasExecutable)
}

func TestGenerator_catalog_entry(t *testing.T) {
	out := runGenerate(t)

	var catalog LatestVersionsJSON
	readJSON(t, filepath.Join(out, "spec/latest_versions.json"), &catalog)

	assert.Equal(t, fixedTime, catalog.GeneratedAt)
	assert.Equal(t, "deadbeefcafef00d", catalog.SteplibCommitSHA)
	require.Len(t, catalog.Steps, 5)

	hello, ok := catalog.Steps["hello-step"]
	require.True(t, ok)
	assert.Equal(t, "2.0.0", hello.LatestVersion)
	assert.Equal(t, "Hello Step", hello.Title)
	assert.Equal(t, "Says hello, breaking change.", hello.Summary)
	assert.Equal(t, "bitrise", hello.Maintainer)
	assert.Nil(t, hello.Deprecation)
	assert.False(t, hello.HasExecutable)
	// asset_urls in the catalog are INVENTORY-ROOT-RELATIVE — consumers
	// resolve them against the inventory base URL they fetched the
	// catalog from (no hosting URL baked into the payload).
	assert.Equal(t,
		"steps/hello-step/assets/icon.svg",
		hello.AssetURLs["icon.svg"],
	)

	multi := catalog.Steps["multi-platform-step"]
	assert.True(t, multi.HasExecutable)
	assert.Equal(t, "3.2.1", multi.LatestVersion)

	deprecated := catalog.Steps["deprecated-step"]
	require.NotNil(t, deprecated.Deprecation)
	assert.Equal(t, "2026-12-31", deprecated.Deprecation.RemovalDate)
}

func TestGenerator_stats(t *testing.T) {
	out := t.TempDir()
	stats, err := Generate(
		"testdata/input",
		out,
		Options{GeneratedAt: fixedTime},
		testLogger{t},
	)
	require.NoError(t, err)

	assert.Equal(t, 5, stats.StepCount)
	// hello-step:3 + deprecated:1 + multi-platform:1 + bash:1 + no-info:1
	assert.Equal(t, 7, stats.VersionCount)
	assert.Positive(t, stats.FilesWritten)
	assert.Positive(t, stats.BytesWritten)
}

func TestCatalogAssetURL(t *testing.T) {
	assert.Equal(t,
		"steps/git-clone/assets/icon.svg",
		catalogAssetURL("git-clone", "assets/icon.svg"),
	)
	// Multi-component step IDs.
	assert.Equal(t,
		"steps/some-very-long-step-id/assets/icon.svg",
		catalogAssetURL("some-very-long-step-id", "assets/icon.svg"),
	)
}

func ExampleGenerate() {
	tmp, err := os.MkdirTemp("", "specv2-example-")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	stats, err := Generate("testdata/input", tmp, Options{GeneratedAt: fixedTime}, exampleLogger{})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("steps=%d versions=%d", stats.StepCount, stats.VersionCount)
	// Output: steps=5 versions=7
}

type exampleLogger struct{}

func (exampleLogger) Debugf(string, ...any) {}
func (exampleLogger) Infof(string, ...any)  {}
func (exampleLogger) Warnf(string, ...any)  {}
func (exampleLogger) Errorf(string, ...any) {}
