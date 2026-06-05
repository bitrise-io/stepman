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

