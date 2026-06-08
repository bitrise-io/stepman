package indexgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/stepman/internal/specfixtures"
	"github.com/bitrise-io/stepman/steplibrary/steplibindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runGenerateInventoryRoot generates a tree into a fresh temp dir and returns
// the inventory root: the dir CONTAINING the version dir (v2/), which is the
// root Validate expects. Tests mutate files under root and re-run Validate.
func runGenerateInventoryRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	_, gotErr := generateFromSteplibClone(
		specfixtures.SteplibClone(),
		root,
		Options{GeneratedAt: fixedTime, SteplibCommitSHA: "deadbeefcafef00d"},
		testLogger{t},
	)
	require.NoError(t, gotErr, "generateFromSteplibClone")
	return root
}

// flagMatching returns the first error whose Path and Msg both contain the
// given substrings. Returns nil if no match. Used to assert "the right
// violation fired" without coupling tests to exact message wording.
func flagMatching(errs []ValidationError, pathContains, msgContains string) *ValidationError {
	for i := range errs {
		if (pathContains == "" || strings.Contains(errs[i].Path, pathContains)) &&
			(msgContains == "" || strings.Contains(errs[i].Msg, msgContains)) {
			return &errs[i]
		}
	}
	return nil
}

// TestValidate_passesOnGeneratedOutput is the headline assertion: a freshly
// generated tree from the canonical fixtures must validate cleanly. (The
// generator already runs Validate internally before publishing, so a
// successful generate is itself proof; this re-runs it explicitly to lock the
// contract in independently.) Every targeted test below mutates this baseline
// to prove a specific check fires.
func TestValidate_passesOnGeneratedOutput(t *testing.T) {
	root := runGenerateInventoryRoot(t)

	errs := Validate(os.DirFS(root))
	assert.Empty(t, errs, "generator output should validate cleanly; got %+v", errs)
}

func TestValidate_flagsMissingMeta(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	require.NoError(t, os.Remove(filepath.Join(root, steplibindex.MetaPathFS())))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "meta.json", "missing"), "missing meta.json must be flagged; got %+v", errs)
}

func TestValidate_flagsBadFormatVersion(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	bad := []byte(`{"format_version": 99, "updated_at": "2026-05-15T12:00:00Z"}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, steplibindex.MetaPathFS()), bad, 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "meta.json", "format_version"), "bad format_version must be flagged; got %+v", errs)
}

func TestValidate_flagsEmptySteplib(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// A steplib with no steps is not valid.
	require.NoError(t, os.WriteFile(filepath.Join(root, steplibindex.StepIDsPathFS()), []byte(`{"step_ids":[]}`), 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "step_ids.json", "empty"),
		"a steplib with no steps must be flagged; got %+v", errs)
}

func TestValidate_flagsUnsortedStepIDs(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// Hand-write an unsorted step_ids.json that still references only real steps
	// so the per-step checks pass and only the sort check fires.
	bad := []byte(`{"step_ids": ["hello-step", "bash-step", "deprecated-step", "multi-platform-step"]}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, steplibindex.StepIDsPathFS()), bad, 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "step_ids.json", "not sorted"), "unsorted step_ids must be flagged; got %+v", errs)
}

func TestValidate_flagsLatestPointingAtMissingVersion(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	bad := []byte(`{"step_id": "hello-step", "latest": "99.99.99", "latest_by_major": {"99": "99.99.99"}}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, steplibindex.LatestPointerPathFS("hello-step")), bad, 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "hello-step/latest.json", "not in"),
		"latest pointing at missing version must be flagged; got %+v", errs)
}

func TestValidate_flagsLatestByMajorWrongMajor(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// hello-step has 1.0.0, 1.1.0, 2.0.0. Point major "1" at 2.0.0.
	bad := []byte(`{"step_id": "hello-step", "latest": "2.0.0", "latest_by_major": {"1": "2.0.0", "2": "2.0.0"}}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, steplibindex.LatestPointerPathFS("hello-step")), bad, 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "hello-step/latest.json", "different major"),
		"latest_by_major pointing at wrong major must be flagged; got %+v", errs)
}

func TestValidate_flagsLatestStepIDMismatch(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// step_id disagreeing with the dir name must be flagged.
	bad := []byte(`{"step_id": "wrong-id", "latest": "2.0.0", "latest_by_major": {"1": "1.1.0", "2": "2.0.0"}}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, steplibindex.LatestPointerPathFS("hello-step")), bad, 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "hello-step/latest.json", "expected"),
		"latest.json step_id mismatch must be flagged; got %+v", errs)
}

func TestValidate_flagsMissingStepJSON(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// Delete hello-step's 1.0.0/step.json while leaving the version in versions.json.
	require.NoError(t, os.Remove(filepath.Join(root, steplibindex.StepJSONPathFS("hello-step", "1.0.0"))))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "hello-step/1.0.0/step.json", "missing"),
		"missing step.json for a declared version must be flagged; got %+v", errs)
}

func TestValidate_flagsMissingStepInfo(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// step-info.json is mandatory: deleting it must be flagged (not skipped).
	require.NoError(t, os.Remove(filepath.Join(root, steplibindex.StepInfoPathFS("hello-step"))))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "hello-step/step-info.json", "missing"),
		"missing mandatory step-info.json must be flagged; got %+v", errs)
}

func TestValidate_flagsStepInfoAssetMissing(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// Delete the asset hello-step's step-info.json references.
	require.NoError(t, os.Remove(filepath.Join(root, steplibindex.StepAssetPathFS("hello-step", "icon.svg"))))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "hello-step/step-info.json", "does not exist"),
		"missing asset referenced by step-info.json must be flagged; got %+v", errs)
}

func TestValidate_flagsAbsoluteAssetURL(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// asset_urls must be step-dir-relative; an absolute URL violates the spec.
	// Keep the real relative asset too so only the absolute-URL check fires.
	bad := []byte(`{"maintainer":"bitrise","deprecation":null,"asset_urls":["assets/icon.svg","https://cdn.example/icon.svg"]}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, steplibindex.StepInfoPathFS("hello-step")), bad, 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "hello-step/step-info.json", "absolute URL"),
		"absolute asset_urls entry must be flagged; got %+v", errs)
}

func TestValidate_flagsStaleIndexFile(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// Drop a stale file into v2/index/.
	stalePath := filepath.Join(root, steplibindex.VersionDir(), steplibindex.IndexRootFS, "stale.json")
	require.NoError(t, os.WriteFile(stalePath, []byte("{}"), 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "stale.json", "unexpected"),
		"unexpected file under index/ must be flagged; got %+v", errs)
}

func TestValidate_flagsStaleStepDir(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// Drop a file under v2/steps/ for a step that step_ids.json doesn't know about.
	stalePath := filepath.Join(root, steplibindex.StepJSONPathFS("ghost-step", "1.0.0"))
	require.NoError(t, os.MkdirAll(filepath.Dir(stalePath), 0o755))
	require.NoError(t, os.WriteFile(stalePath, []byte(`{"title":"ghost"}`), 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "ghost-step", "unexpected"),
		"step dir not in step_ids.json must be flagged; got %+v", errs)
}

func TestValidate_flagsUnreferencedAsset(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	// An asset present on disk but not listed in step-info.json's asset_urls
	// must be flagged by the stale-file walk.
	extra := filepath.Join(root, steplibindex.StepAssetPathFS("hello-step", "extra.svg"))
	require.NoError(t, os.WriteFile(extra, []byte("<svg/>"), 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "hello-step/assets/extra.svg", "unexpected"),
		"an asset on disk but missing from step-info.json must be flagged; got %+v", errs)
}

func TestValidate_flagsInvalidJSON(t *testing.T) {
	root := runGenerateInventoryRoot(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, steplibindex.MetaPathFS()), []byte("not json"), 0o644))

	errs := Validate(os.DirFS(root))
	assert.NotNil(t, flagMatching(errs, "meta.json", "invalid JSON"),
		"invalid JSON must be flagged; got %+v", errs)
}
