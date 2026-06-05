package specgen

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollect_step_info_and_asset_copy(t *testing.T) {
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

func TestCollect_asset_permissions_preserved(t *testing.T) {
	// Embedded fixtures can't carry real file permissions, so drive the
	// generator from an in-memory FS where the asset's mode is set explicitly
	// (and distinct from the writer's 0644 default), then assert the copy
	// preserves it.
	const mode = os.FileMode(0o640)
	inputFS := fstest.MapFS{
		"steplib.yml":                     {Data: []byte("format_version: '0.9.0'\nsteplib_source: 'https://example.com'\n")},
		"steps/perm-step/1.0.0/step.yml":  {Data: []byte("title: Perm Step\n")},
		"steps/perm-step/assets/icon.svg": {Data: []byte("<svg/>"), Mode: mode},
	}

	out := t.TempDir()
	_, gotErr := GenerateFromSteplibClone(inputFS, out, Options{GeneratedAt: fixedTime}, testLogger{t})
	require.NoError(t, gotErr, "GenerateFromSteplibClone")

	dstInfo, err := os.Stat(filepath.Join(out, "steps/perm-step/assets/icon.svg"))
	require.NoError(t, err, "stat copied asset")
	assert.Equal(t, mode, dstInfo.Mode().Perm(), "copied asset preserves source file mode")
}

func TestCollect_deprecated_step(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var info spec.StepInfo
	readJSON(t, filepath.Join(out, "steps/deprecated-step/step-info.json"), &info)

	assert.Equal(t, "bitrise", info.Maintainer, "Maintainer")
	require.NotNil(t, info.Deprecation, "Deprecation")
	assert.Equal(t, "2025-04-11", info.Deprecation.RemovalDate, "RemovalDate")
	assert.Contains(t, info.Deprecation.Notes, "key-based caching", "Notes")
	assert.Empty(t, info.AssetURLs, "no assets dir → no asset_urls")
}

func TestCollect_no_info_step_skips_step_info_file(t *testing.T) {
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

func TestCollect_step_info_written_for_assets_only_step(t *testing.T) {
	// step-info.json should be written when a step has assets but no step-info.yml.
	inputFS := fstest.MapFS{
		"steplib.yml":                           {Data: []byte("format_version: '0.9.0'\nsteplib_source: 'https://example.com'\n")},
		"steps/asset-only-step/1.0.0/step.yml":  {Data: []byte("title: Asset Only\n")},
		"steps/asset-only-step/assets/icon.svg": {Data: []byte("<svg/>"), Mode: 0o644},
	}
	out := t.TempDir()
	_, gotErr := GenerateFromSteplibClone(inputFS, out, Options{GeneratedAt: fixedTime}, testLogger{t})
	require.NoError(t, gotErr, "GenerateFromSteplibClone")

	var info spec.StepInfo
	readJSON(t, filepath.Join(out, "steps/asset-only-step/step-info.json"), &info)
	assert.Equal(t, map[string]string{"icon.svg": "assets/icon.svg"}, info.AssetURLs, "AssetURLs")
	assert.Empty(t, info.Maintainer, "Maintainer")
	assert.Nil(t, info.Deprecation, "Deprecation")
}

func TestCollect_invalid_version_dir_skipped(t *testing.T) {
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

	_, statErr := os.Stat(filepath.Join(out, "steps/my-step/1.0.0/step.json"))
	assert.NoError(t, statErr, "valid version written")
	_, statErr = os.Stat(filepath.Join(out, "steps/my-step/not-a-semver/step.json"))
	assert.True(t, os.IsNotExist(statErr), "non-semver version dir skipped")
}

func TestCollect_multi_platform_executables(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var step models.StepModel
	readJSON(t, filepath.Join(out, "steps/multi-platform-step/3.2.1/step.json"), &step)

	require.NotNil(t, step.Executables, "Executables")
	require.Len(t, *step.Executables, 4, "Executables len")

	darwinArm := (*step.Executables)["darwin-arm64"]
	assert.Equal(t,
		"steps/deploy-to-bitrise-io/2.23.2/bin/deploy-to-bitrise-io-darwin-arm64",
		darwinArm.StorageURI,
		"darwin-arm64 StorageURI",
	)
	assert.Equal(t,
		"sha256-316b1ae22a53e06199b68a3ddf008345aa9e3690abcd57243085a56ccdc57159",
		darwinArm.Hash,
		"darwin-arm64 Hash",
	)

	linuxAmd := (*step.Executables)["linux-amd64"]
	assert.Equal(t,
		"steps/deploy-to-bitrise-io/2.23.2/bin/deploy-to-bitrise-io-linux-amd64",
		linuxAmd.StorageURI,
		"linux-amd64 StorageURI",
	)
}

func TestCollect_bash_step_has_no_executables(t *testing.T) {
	out := runGenerateFromSteplibClone(t)

	var step models.StepModel
	readJSON(t, filepath.Join(out, "steps/bash-step/1.0.0/step.json"), &step)

	// The Script step ships no precompiled binary, so activation builds from
	// source (Executables nil). It also declares no toolkit, and the generator
	// preserves that verbatim — like V1's parse pipeline (Normalize +
	// FillMissingDefaults), it never synthesizes a default toolkit. The bash +
	// step.sh default is applied at run time (toolkits.ToolkitForStep defaults to
	// BashToolkit, which uses step.sh when no entry file is set), not baked into
	// step.json.
	assert.Nil(t, step.Executables, "Executables")
	assert.Nil(t, step.Toolkit, "Toolkit")
	require.NotNil(t, step.Title, "Title")
	assert.Equal(t, "Script", *step.Title, "Title")
}
