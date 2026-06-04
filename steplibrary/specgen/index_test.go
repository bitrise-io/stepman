package specgen

import (
	"path/filepath"
	"testing"

	"github.com/bitrise-io/stepman/steplibrary/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndex_per_step_latest_pointer(t *testing.T) {
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

func TestIndex_single_version_latest_pointer(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	// bash-step has exactly one version (1.0.0) in a single major.
	var latest spec.LatestPointer
	readJSON(t, filepath.Join(out, "spec/steps/bash-step/latest.json"), &latest)

	assert.Equal(t, "bash-step", latest.StepID, "StepID")
	assert.Equal(t, "1.0.0", latest.Latest, "Latest")
	assert.Equal(t, map[string]string{"1": "1.0.0"}, latest.LatestByMajor, "LatestByMajor")
}

func TestIndex_versions_newest_first(t *testing.T) {
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

func TestIndex_versions_has_executable(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var versions spec.Versions
	readJSON(t, filepath.Join(out, "spec/steps/multi-platform-step/versions.json"), &versions)

	require.Len(t, versions.Versions, 1, "Versions")
	assert.True(t, versions.Versions[0].HasExecutable, "Versions[0].HasExecutable")
}

func TestIndex_catalog_entry(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var catalog spec.Catalog
	readJSON(t, filepath.Join(out, "spec/latest_versions.json"), &catalog)

	assert.Equal(t, fixedTime, catalog.GeneratedAt, "GeneratedAt")
	assert.Equal(t, "deadbeefcafef00d", catalog.SteplibCommitSHA, "SteplibCommitSHA")
	require.Len(t, catalog.Steps, 5, "Steps")

	hello, ok := catalog.Steps["hello-step"]
	require.True(t, ok, "hello-step present")
	assert.Equal(t, "2.0.0", hello.LatestVersion, "hello-step LatestVersion")
	assert.Equal(t, "Hello Step", hello.Title, "hello-step Title")
	assert.Equal(t, "Says hello, breaking change.", hello.Summary, "hello-step Summary")
	assert.Equal(t, "bitrise", hello.Maintainer, "hello-step Maintainer")
	assert.Nil(t, hello.Deprecation, "hello-step Deprecation")
	assert.False(t, hello.HasExecutable, "hello-step HasExecutable")
	// asset_urls are INVENTORY-ROOT-RELATIVE.
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
	assert.Equal(t, "2025-04-11", deprecated.Deprecation.RemovalDate, "deprecated-step RemovalDate")
}

func TestIndex_catalog_no_info_and_bash_entries(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var catalog spec.Catalog
	readJSON(t, filepath.Join(out, "spec/latest_versions.json"), &catalog)

	noInfo, ok := catalog.Steps["no-info-step"]
	require.True(t, ok, "no-info-step present")
	assert.Equal(t, "1.0.0", noInfo.LatestVersion, "no-info-step LatestVersion")
	assert.Empty(t, noInfo.Maintainer, "no-info-step Maintainer")
	assert.Nil(t, noInfo.Deprecation, "no-info-step Deprecation")
	assert.Empty(t, noInfo.AssetURLs, "no-info-step AssetURLs")
	assert.False(t, noInfo.HasExecutable, "no-info-step HasExecutable")

	bash, ok := catalog.Steps["bash-step"]
	require.True(t, ok, "bash-step present")
	assert.Equal(t, "1.0.0", bash.LatestVersion, "bash-step LatestVersion")
	assert.Equal(t, "bitrise", bash.Maintainer, "bash-step Maintainer")
	assert.Nil(t, bash.Deprecation, "bash-step Deprecation")
	assert.False(t, bash.HasExecutable, "bash-step HasExecutable")
}

func TestIndex_catalog_asset_url(t *testing.T) {
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
