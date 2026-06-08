package steplibindex

import (
	"net/url"
	"path"
)

// Paths to every file in the V2 inventory tree. There must be exactly one
// source of truth for the layout so that the generator, the HTTP reader, and
// the validator never drift.
//
// Two parallel families are exposed because the two consumption modes are
// genuinely different:
//
//   - Filesystem paths (FS-suffixed) are slash-separated, relative to the
//     inventory root, and suitable for fs.FS lookups, path.Join, and the
//     generator's writer.
//   - URL paths (URL-suffixed) are absolute (leading slash) and have
//     url.PathEscape applied to the dynamic id/version segments so that
//     unusual but valid step IDs (e.g. ones containing reserved URL chars)
//     never break the HTTP reader.

// Top-level directories inside the inventory tree.
//
// Note: the helpers below that build index/steps/<id>/... paths use the
// inline literal "steps" rather than referencing StepsRootFS. This is
// deliberate: the "steps" segment under index/ is the per-step *index* dir
// (a different namespace from the top-level source-of-truth steps/ tree),
// and reusing StepsRootFS there would conflate the two. The name collision
// is in the V2 layout itself; the constants do not paper over it.
const (
	StepsRootFS = "steps"
	IndexRootFS = "index"
)

// MetaPathFS returns the inventory-relative path of v2/meta.json.
func MetaPathFS() string { return path.Join(VersionDir(), "meta.json") }

// MetaPathURL returns the URL form of MetaPathFS.
func MetaPathURL() string { return "/" + MetaPathFS() }

// StepIDsPathFS returns the inventory-relative path of v2/index/step_ids.json.
func StepIDsPathFS() string { return path.Join(VersionDir(), IndexRootFS, "step_ids.json") }

// StepIDsPathURL returns the URL form of StepIDsPathFS.
func StepIDsPathURL() string { return "/" + StepIDsPathFS() }

// LatestPointerPathFS returns the inventory-relative path of
// v2/index/steps/<id>/latest.json.
func LatestPointerPathFS(stepID string) string {
	return path.Join(VersionDir(), IndexRootFS, "steps", stepID, "latest.json")
}

// LatestPointerPathURL returns the URL form of LatestPointerPathFS with the
// step ID URL-escaped.
func LatestPointerPathURL(stepID string) string {
	return "/" + path.Join(VersionDir(), IndexRootFS, "steps", url.PathEscape(stepID), "latest.json")
}

// VersionsPathFS returns the inventory-relative path of
// v2/index/steps/<id>/versions.json.
func VersionsPathFS(stepID string) string {
	return path.Join(VersionDir(), IndexRootFS, "steps", stepID, "versions.json")
}

// VersionsPathURL returns the URL form of VersionsPathFS with the step ID
// URL-escaped.
func VersionsPathURL(stepID string) string {
	return "/" + path.Join(VersionDir(), IndexRootFS, "steps", url.PathEscape(stepID), "versions.json")
}

// StepInfoPathFS returns the inventory-relative path of
// v2/steps/<id>/step-info.json.
func StepInfoPathFS(stepID string) string {
	return path.Join(VersionDir(), StepsRootFS, stepID, "step-info.json")
}

// StepInfoPathURL returns the URL form of StepInfoPathFS with the step ID
// URL-escaped.
func StepInfoPathURL(stepID string) string {
	return "/" + path.Join(VersionDir(), StepsRootFS, url.PathEscape(stepID), "step-info.json")
}

// StepJSONPathFS returns the inventory-relative path of
// v2/steps/<id>/<version>/step.json.
func StepJSONPathFS(stepID, version string) string {
	return path.Join(VersionDir(), StepsRootFS, stepID, version, "step.json")
}

// StepJSONPathURL returns the URL form of StepJSONPathFS with the step ID
// and version URL-escaped.
func StepJSONPathURL(stepID, version string) string {
	return "/" + path.Join(VersionDir(), StepsRootFS, url.PathEscape(stepID), url.PathEscape(version), "step.json")
}

// StepAssetDirFS returns the inventory-relative path of v2/steps/<id>/assets.
func StepAssetDirFS(stepID string) string {
	return path.Join(VersionDir(), StepsRootFS, stepID, "assets")
}

// StepAssetPathFS returns the inventory-relative path of
// v2/steps/<id>/assets/<file>.
func StepAssetPathFS(stepID, file string) string {
	return path.Join(VersionDir(), StepsRootFS, stepID, "assets", file)
}

// StepDirFS returns the inventory-relative path of the v2/steps/<id>/ directory
// (the per-step subtree that holds step-info.json, assets/, and per-version
// dirs).
func StepDirFS(stepID string) string {
	return path.Join(VersionDir(), StepsRootFS, stepID)
}

// IndexStepDirFS returns the inventory-relative path of the
// v2/index/steps/<id>/ directory (the per-step subtree of derived index files).
func IndexStepDirFS(stepID string) string {
	return path.Join(VersionDir(), IndexRootFS, "steps", stepID)
}
