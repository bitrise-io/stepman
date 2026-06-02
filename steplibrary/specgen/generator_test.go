package specgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"testing/fstest"
	"time"

	"github.com/bitrise-io/stepman/models"
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

// runGenerateFromSteplibClone runs the generator against the checked-in
// testdata using a fresh temp output dir. Returns the output dir for assertions.
func runGenerateFromSteplibClone(t *testing.T) string {
	t.Helper()
	out := t.TempDir()
	_, gotErr := GenerateFromSteplibClone(
		os.DirFS("testdata/input"),
		out,
		Options{GeneratedAt: fixedTime, SteplibCommitSHA: "deadbeefcafef00d"},
		testLogger{t},
	)
	require.NoError(t, gotErr, "GenerateFromSteplibClone")
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

	want := []string{"bash-step", "deprecated-step", "hello-step", "multi-platform-step", "no-info-step"}
	assert.Equal(t, want, ids.StepIDs, "step IDs")
	assert.True(t, sort.StringsAreSorted(ids.StepIDs), "step IDs are sorted")
}

func TestGenerator_normal_step_info_and_asset_copy(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var info spec.StepInfo
	readJSON(t, filepath.Join(out, "steps/hello-step/step-info.json"), &info)

	assert.Equal(t, "bitrise", info.Maintainer, "Maintainer")
	assert.Nil(t, info.Deprecation, "Deprecation")
	assert.Equal(t, map[string]string{"icon.svg": "assets/icon.svg"}, info.AssetURLs, "AssetURLs")

	// Asset file copied.
	_, gotErr := os.Stat(filepath.Join(out, "steps/hello-step/assets/icon.svg"))
	assert.NoError(t, gotErr, "asset file copied")
}

func TestGenerator_deprecated_step(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var info spec.StepInfo
	readJSON(t, filepath.Join(out, "steps/deprecated-step/step-info.json"), &info)

	assert.Equal(t, "community", info.Maintainer, "Maintainer")
	require.NotNil(t, info.Deprecation, "Deprecation")
	assert.Equal(t, "2026-12-31", info.Deprecation.RemovalDate, "RemovalDate")
	assert.Contains(t, info.Deprecation.Notes, "Replaced by hello-step", "Notes")
	assert.Empty(t, info.AssetURLs, "no assets dir → no asset_urls")
}

func TestGenerator_no_info_step_skips_step_info_file(t *testing.T) {
	inputFS := fstest.MapFS{
		"steplib.yml":                       {Data: []byte("format_version: '0.9.0'\n")},
		"steps/no-info-step/1.0.0/step.yml": {Data: []byte("title: No Info\n")},
	}
	out := t.TempDir()
	_, gotErr := GenerateFromSteplibClone(inputFS, out, Options{GeneratedAt: fixedTime}, testLogger{t})
	require.NoError(t, gotErr, "GenerateFromSteplibClone")

	// step-info.json must NOT exist: no step-info.yml and no assets.
	_, statErr := os.Stat(filepath.Join(out, "steps/no-info-step/step-info.json"))
	assert.True(t, os.IsNotExist(statErr), "step-info.json should not exist; got err=%v", statErr)

	// step.json must still be written.
	_, statErr = os.Stat(filepath.Join(out, "steps/no-info-step/1.0.0/step.json"))
	assert.NoError(t, statErr, "step.json written")
}

func TestGenerator_multi_platform_executables(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	// step.json is a models.StepModel marshaled as JSON — same shape as V1 step.yml.
	var step models.StepModel
	readJSON(t, filepath.Join(out, "steps/multi-platform-step/3.2.1/step.json"), &step)

	require.NotNil(t, step.Executables, "Executables")
	require.Len(t, *step.Executables, 2, "Executables")

	// StorageURI is preserved verbatim from V1 step.yml — a relative path.
	// The client (today's activator) is responsible for resolving it against
	// the configured binary storage base.
	darwinArm := (*step.Executables)["darwin-arm64"]
	assert.Equal(t,
		"steps/multi-platform-step/3.2.1/bin/multi-platform-step-darwin-arm64",
		darwinArm.StorageURI,
		"darwin-arm64 StorageURI",
	)
	assert.Equal(t,
		"sha256-1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa",
		darwinArm.Hash,
		"darwin-arm64 Hash",
	)

	linuxAmd := (*step.Executables)["linux-amd64"]
	assert.Equal(t,
		"steps/multi-platform-step/3.2.1/bin/multi-platform-step-linux-amd64",
		linuxAmd.StorageURI,
		"linux-amd64 StorageURI",
	)
}

func TestGenerator_bash_step_has_no_executables(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var step models.StepModel
	readJSON(t, filepath.Join(out, "steps/bash-step/1.0.0/step.json"), &step)

	assert.Nil(t, step.Executables, "Executables")
	require.NotNil(t, step.Toolkit, "Toolkit")
	require.NotNil(t, step.Toolkit.Bash, "Toolkit.Bash")
	assert.Equal(t, "step.sh", step.Toolkit.Bash.EntryFile, "Toolkit.Bash.EntryFile")
}

func TestGenerator_per_step_latest_pointer(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var latest spec.LatestPointer
	readJSON(t, filepath.Join(out, "spec/steps/hello-step/latest.json"), &latest)

	assert.Equal(t, "hello-step", latest.StepID, "StepID")
	assert.Equal(t, "2.0.0", latest.Latest, "Latest")
	assert.Equal(t, map[string]string{
		"1": "1.1.0",
		"2": "2.0.0",
	}, latest.LatestByMajor, "LatestByMajor")
}

func TestGenerator_per_step_versions_newest_first(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var versions spec.Versions
	readJSON(t, filepath.Join(out, "spec/steps/hello-step/versions.json"), &versions)

	assert.Equal(t, "hello-step", versions.StepID, "StepID")
	assert.Equal(t, "2.0.0", versions.Latest, "Latest")
	require.Len(t, versions.Versions, 3, "Versions")
	assert.Equal(t, "2.0.0", versions.Versions[0].Version, "Versions[0]")
	assert.Equal(t, "1.1.0", versions.Versions[1].Version, "Versions[1]")
	assert.Equal(t, "1.0.0", versions.Versions[2].Version, "Versions[2]")

	// has_executable is false for all hello-step versions (it's a bash step).
	for _, v := range versions.Versions {
		assert.False(t, v.HasExecutable, "version %s should not have an executable", v.Version)
	}
	// commit + published_at are populated.
	assert.Equal(t, "cccc3333cccc3333cccc3333cccc3333cccc3333", versions.Versions[0].Commit, "Versions[0].Commit")
	require.NotNil(t, versions.Versions[0].PublishedAt, "Versions[0].PublishedAt")
	assert.Equal(t, 2025, versions.Versions[0].PublishedAt.Year(), "Versions[0].PublishedAt year")
}

func TestGenerator_per_step_versions_has_executable(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var versions spec.Versions
	readJSON(t, filepath.Join(out, "spec/steps/multi-platform-step/versions.json"), &versions)

	require.Len(t, versions.Versions, 1, "Versions")
	assert.True(t, versions.Versions[0].HasExecutable, "Versions[0].HasExecutable")
}

func TestGenerator_catalog_entry(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var catalog spec.Catalog
	readJSON(t, filepath.Join(out, "spec/latest_versions.json"), &catalog)

	assert.Equal(t, fixedTime, catalog.GeneratedAt, "GeneratedAt")
	assert.Equal(t, "deadbeefcafef00d", catalog.SteplibCommitSHA, "SteplibCommitSHA")
	require.Len(t, catalog.Steps, 5, "Steps")

	hello, ok := catalog.Steps["hello-step"]
	require.True(t, ok, "hello-step present in catalog")
	assert.Equal(t, "2.0.0", hello.LatestVersion, "hello-step LatestVersion")
	assert.Equal(t, "Hello Step", hello.Title, "hello-step Title")
	assert.Equal(t, "Says hello, breaking change.", hello.Summary, "hello-step Summary")
	assert.Equal(t, "bitrise", hello.Maintainer, "hello-step Maintainer")
	assert.Nil(t, hello.Deprecation, "hello-step Deprecation")
	assert.False(t, hello.HasExecutable, "hello-step HasExecutable")
	// asset_urls in the catalog are INVENTORY-ROOT-RELATIVE — consumers
	// resolve them against the inventory base URL they fetched the
	// catalog from (no hosting URL baked into the payload).
	assert.Equal(t,
		"steps/hello-step/assets/icon.svg",
		hello.AssetURLs["icon.svg"],
		"hello-step AssetURLs[icon.svg]",
	)

	multi := catalog.Steps["multi-platform-step"]
	assert.True(t, multi.HasExecutable, "multi-platform-step HasExecutable")
	assert.Equal(t, "3.2.1", multi.LatestVersion, "multi-platform-step LatestVersion")

	deprecated := catalog.Steps["deprecated-step"]
	require.NotNil(t, deprecated.Deprecation, "deprecated-step Deprecation")
	assert.Equal(t, "2026-12-31", deprecated.Deprecation.RemovalDate, "deprecated-step RemovalDate")
}

func TestGenerator_stats(t *testing.T) {
	out := t.TempDir()
	stats, gotErr := GenerateFromSteplibClone(
		os.DirFS("testdata/input"),
		out,
		Options{GeneratedAt: fixedTime},
		testLogger{t},
	)
	require.NoError(t, gotErr, "GenerateFromSteplibClone")

	assert.Equal(t, 5, stats.StepCount, "StepCount")
	// hello-step:3 + deprecated:1 + multi-platform:1 + bash:1 + no-info:1
	assert.Equal(t, 7, stats.VersionCount, "VersionCount")
	// step-level: bash(2) + deprecated(2) + hello(5) + multi-platform(3) + no-info(1) = 13
	// spec/:      step_ids + latest_versions + 5×(latest+versions) = 12
	// meta.json:  1
	assert.Equal(t, 26, stats.FilesWritten, "FilesWritten")
	assert.Positive(t, stats.BytesWritten, "BytesWritten")
}

func TestGenerator_asset_permissions_preserved(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	src := "testdata/input/steps/hello-step/assets/icon.svg"
	dst := filepath.Join(out, "steps/hello-step/assets/icon.svg")

	srcInfo, err := os.Stat(src)
	require.NoError(t, err, "stat source asset")
	dstInfo, err := os.Stat(dst)
	require.NoError(t, err, "stat copied asset")

	assert.Equal(t, srcInfo.Mode(), dstInfo.Mode(), "copied asset should preserve source file permissions")
}

func TestGenerator_step_info_written_for_assets_only_step(t *testing.T) {
	// step-info.json should be written when a step has assets but no step-info.yml.
	inputFS := fstest.MapFS{
		"steplib.yml":                           {Data: []byte("format_version: '0.9.0'\nsteplib_source: 'https://example.com'\n")},
		"steps/asset-only-step/1.0.0/step.yml":  {Data: []byte("title: Asset Only\n")},
		"steps/asset-only-step/assets/icon.svg": {Data: []byte("<svg/>"), Mode: 0o644},
	}
	out := t.TempDir()
	_, gotErr := GenerateFromSteplibClone(inputFS, out, Options{GeneratedAt: fixedTime}, testLogger{t})
	require.NoError(t, gotErr, "GenerateFromSteplibClone")

	// step-info.json must exist even without step-info.yml because there are assets.
	var info spec.StepInfo
	readJSON(t, filepath.Join(out, "steps/asset-only-step/step-info.json"), &info)
	assert.Equal(t, map[string]string{"icon.svg": "assets/icon.svg"}, info.AssetURLs, "AssetURLs")
	assert.Empty(t, info.Maintainer, "Maintainer")
	assert.Nil(t, info.Deprecation, "Deprecation")
}

func TestGenerator_invalid_version_dir_skipped(t *testing.T) {
	inputFS := fstest.MapFS{
		"steplib.yml":                            {Data: []byte("format_version: '0.9.0'\n")},
		"steps/my-step/1.0.0/step.yml":           {Data: []byte("title: My Step\n")},
		"steps/my-step/not-a-semver/step.yml":    {Data: []byte("title: Should be skipped\n")},
		"steps/my-step/also-not-semver/step.yml": {Data: []byte("title: Also skipped\n")},
	}
	out := t.TempDir()
	stats, gotErr := GenerateFromSteplibClone(inputFS, out, Options{GeneratedAt: fixedTime}, testLogger{t})
	require.NoError(t, gotErr, "GenerateFromSteplibClone")

	assert.Equal(t, 1, stats.StepCount, "StepCount")
	assert.Equal(t, 1, stats.VersionCount, "VersionCount")

	// Only the valid version is written.
	_, statErr := os.Stat(filepath.Join(out, "steps/my-step/1.0.0/step.json"))
	assert.NoError(t, statErr, "valid version written")
	_, statErr = os.Stat(filepath.Join(out, "steps/my-step/not-a-semver/step.json"))
	assert.True(t, os.IsNotExist(statErr), "non-semver version dir skipped")
}

func TestGenerator_single_version_latest_pointer(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	// bash-step has exactly one version (1.0.0) in a single major.
	var latest spec.LatestPointer
	readJSON(t, filepath.Join(out, "spec/steps/bash-step/latest.json"), &latest)

	assert.Equal(t, "bash-step", latest.StepID, "StepID")
	assert.Equal(t, "1.0.0", latest.Latest, "Latest")
	assert.Equal(t, map[string]string{"1": "1.0.0"}, latest.LatestByMajor, "LatestByMajor")
}

func TestGenerator_catalog_no_info_and_bash_entries(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var catalog spec.Catalog
	readJSON(t, filepath.Join(out, "spec/latest_versions.json"), &catalog)

	noInfo, ok := catalog.Steps["no-info-step"]
	require.True(t, ok, "no-info-step present in catalog")
	assert.Equal(t, "1.0.0", noInfo.LatestVersion, "no-info-step LatestVersion")
	assert.Empty(t, noInfo.Maintainer, "no-info-step Maintainer")
	assert.Nil(t, noInfo.Deprecation, "no-info-step Deprecation")
	assert.Empty(t, noInfo.AssetURLs, "no-info-step AssetURLs")
	assert.False(t, noInfo.HasExecutable, "no-info-step HasExecutable")

	bash, ok := catalog.Steps["bash-step"]
	require.True(t, ok, "bash-step present in catalog")
	assert.Equal(t, "1.0.0", bash.LatestVersion, "bash-step LatestVersion")
	assert.Equal(t, "community", bash.Maintainer, "bash-step Maintainer")
	assert.Nil(t, bash.Deprecation, "bash-step Deprecation")
	assert.False(t, bash.HasExecutable, "bash-step HasExecutable")
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

func TestCatalogAssetURL(t *testing.T) {
	assert.Equal(t,
		"steps/git-clone/assets/icon.svg",
		catalogAssetURL("git-clone", "assets/icon.svg"),
		"single-component step ID",
	)
	assert.Equal(t,
		"steps/some-very-long-step-id/assets/icon.svg",
		catalogAssetURL("some-very-long-step-id", "assets/icon.svg"),
		"multi-component step ID",
	)
}

func ExampleGenerateFromSteplibClone() {
	tmp, err := os.MkdirTemp("", "specv2-example-")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	stats, err := GenerateFromSteplibClone(os.DirFS("testdata/input"), tmp, Options{GeneratedAt: fixedTime}, exampleLogger{})
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
