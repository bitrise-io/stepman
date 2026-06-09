package indexgen

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/steplibindex"
)

// ValidationError is a single consistency violation found by Validate.
// Path is the slash-separated path of the file the violation belongs to,
// or "" for tree-level issues. Msg explains what's wrong.
type ValidationError struct {
	Path string
	Msg  string
}

func (e ValidationError) Error() string {
	if e.Path == "" {
		return e.Msg
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Msg)
}

// Validate walks the V2 inventory tree rooted at inventoryFS and returns the
// list of consistency violations. An empty slice means the tree is internally
// consistent.
//
// Intended uses:
//   - Pre-deploy CI gate: run against a freshly generated tree before
//     publishing to the V2 host. Fail the build on any violation.
//   - Generator test smoke check: run a generated tree through Validate to
//     catch cross-file inconsistencies that per-file assertions would miss.
//
// A filesystem traversal failure (e.g. an unreadable directory) is itself
// reported as a violation, so callers only need to check whether the returned
// slice is empty.
func Validate(inventoryFS fs.FS) []ValidationError {
	v := &validator{fs: inventoryFS, seen: map[string]bool{}, errs: nil}
	v.run()
	return v.errs
}

type validator struct {
	fs   fs.FS
	seen map[string]bool // paths the validator has consumed; remainder is stale
	errs []ValidationError
}

func (v *validator) flag(p, msg string, args ...any) {
	v.errs = append(v.errs, ValidationError{Path: p, Msg: fmt.Sprintf(msg, args...)})
}

func (v *validator) consume(p string) { v.seen[p] = true }

func (v *validator) run() {
	var meta steplibindex.Meta
	if v.readJSON(steplibindex.MetaPathFS(), &meta) {
		v.checkMeta(meta)
	}

	var stepIDs steplibindex.StepIDs
	haveIDs := v.readJSON(steplibindex.StepIDsPathFS(), &stepIDs)

	if haveIDs {
		if len(stepIDs.StepIDs) == 0 {
			v.flag(steplibindex.StepIDsPathFS(), "step_ids is empty: a steplib must contain at least one step")
		}
		v.checkStepIDsSorted(stepIDs)
		for _, id := range stepIDs.StepIDs {
			v.checkStep(id)
		}
	}

	v.checkNoStaleFiles()

	// Sort violations by (Path, Msg) so the error output is deterministic
	// across runs. Without this, map-iteration order leaks into the
	// validator's output and breaks any consumer that diffs error logs
	// (golden files, CI dashboards, etc.).
	sort.Slice(v.errs, func(i, j int) bool {
		if v.errs[i].Path != v.errs[j].Path {
			return v.errs[i].Path < v.errs[j].Path
		}
		return v.errs[i].Msg < v.errs[j].Msg
	})
}

// readJSON marks the file as consumed and parses it into `into`. Returns
// false (and logs a violation) on missing/unreadable/invalid JSON.
func (v *validator) readJSON(p string, into any) bool {
	v.consume(p)
	bytes, err := fs.ReadFile(v.fs, p)
	if err != nil {
		v.flag(p, "missing or unreadable: %s", err)
		return false
	}
	if err := json.Unmarshal(bytes, into); err != nil {
		v.flag(p, "invalid JSON: %s", err)
		return false
	}
	return true
}

func (v *validator) checkMeta(m steplibindex.Meta) {
	if m.FormatVersion != steplibindex.FormatVersion {
		v.flag(steplibindex.MetaPathFS(), "format_version is %d, expected %d", m.FormatVersion, steplibindex.FormatVersion)
	}
	if m.UpdatedAt.IsZero() {
		v.flag(steplibindex.MetaPathFS(), "updated_at is zero")
	}
}

func (v *validator) checkStepIDsSorted(ids steplibindex.StepIDs) {
	if !sort.StringsAreSorted(ids.StepIDs) {
		v.flag(steplibindex.StepIDsPathFS(), "step_ids is not sorted lexicographically")
	}
	// Duplicate detection.
	seen := make(map[string]bool, len(ids.StepIDs))
	for _, id := range ids.StepIDs {
		if seen[id] {
			v.flag(steplibindex.StepIDsPathFS(), "duplicate step id %q", id)
		}
		seen[id] = true
	}
}

func (v *validator) checkStep(id string) {
	latestPath := steplibindex.LatestPointerPathFS(id)
	versionsPath := steplibindex.VersionsPathFS(id)

	var latest steplibindex.LatestPointer
	haveLatest := v.readJSON(latestPath, &latest)

	var versions steplibindex.Versions
	haveVersions := v.readJSON(versionsPath, &versions)

	if haveLatest && latest.StepID != id {
		v.flag(latestPath, "step_id is %q, expected %q", latest.StepID, id)
	}
	if haveVersions && versions.StepID != id {
		v.flag(versionsPath, "step_id is %q, expected %q", versions.StepID, id)
	}

	// Cross-check pointers against the versions list.
	declaredVersions := map[string]bool{}
	if haveVersions {
		for _, ver := range versions.Versions {
			declaredVersions[ver] = true
		}
	}
	if haveLatest && haveVersions {
		if !declaredVersions[latest.Latest] {
			v.flag(latestPath, "latest %q is not in %s", latest.Latest, versionsPath)
		}
		for major, ver := range latest.LatestByMajor {
			if !declaredVersions[ver] {
				v.flag(latestPath, "latest_by_major[%q]=%q is not in versions.json", major, ver)
			}
			if !strings.HasPrefix(ver, major+".") {
				v.flag(latestPath, "latest_by_major[%q]=%q has a different major", major, ver)
			}
		}
	}

	// Every declared version must have its step.json on disk.
	if haveVersions {
		for _, ver := range versions.Versions {
			v.checkStepJSON(id, ver)
		}
	}

	v.checkStepInfo(id)
}

func (v *validator) checkStepJSON(id, version string) {
	p := steplibindex.StepJSONPathFS(id, version)
	var step models.StepModel
	if !v.readJSON(p, &step) {
		return
	}
	if step.Source == nil {
		v.flag(p, "missing source")
		return
	}
	if step.Source.Git == "" {
		v.flag(p, "missing source.git")
	}
	if step.Source.Commit == "" {
		v.flag(p, "missing source.commit")
	}
}

func (v *validator) checkStepInfo(id string) {
	p := steplibindex.StepInfoPathFS(id)
	if _, err := fs.Stat(v.fs, p); err != nil {
		// step-info.json is mandatory: the generator writes it for every step.
		v.consume(p)
		v.flag(p, "missing or unreadable: %s", err)
		return
	}
	var info steplibindex.StepInfo
	if !v.readJSON(p, &info) {
		return
	}
	stepDir := steplibindex.StepDirFS(id)
	for _, rel := range info.AssetURLs {
		// asset_urls are step-dir-relative (e.g. "assets/icon.svg"); resolve each
		// against the step's own dir, after rejecting anything that isn't a clean
		// step-relative reference.
		if problem := validateAssetURL(rel, stepDir); problem != "" {
			v.flag(p, "asset_urls entry %q %s; must be step-relative", rel, problem)
			continue
		}
		assetPath := path.Join(stepDir, rel)
		if _, err := fs.Stat(v.fs, assetPath); err != nil {
			v.flag(p, "asset_urls entry %q points to %q which does not exist", rel, assetPath)
			continue
		}
		v.consume(assetPath)
	}
}

// validateAssetURL reports why rel is not a valid step-relative asset reference,
// or "" if it is fine. The single rule — relative, scheme-less, and resolving
// within stepDir — rejects absolute URLs, absolute paths, and parent-directory
// traversal alike, so a bad entry can't slip through one gap while another is
// guarded.
func validateAssetURL(rel, stepDir string) string {
	switch {
	case strings.Contains(rel, "://"):
		return "is an absolute URL"
	case path.IsAbs(rel):
		return "is an absolute path"
	}
	// path.Join cleans the result, collapsing any "../"; if it no longer sits
	// under stepDir the entry escaped the step's own directory.
	if resolved := path.Join(stepDir, rel); resolved != stepDir && !strings.HasPrefix(resolved, stepDir+"/") {
		return "escapes the step directory"
	}
	return ""
}

// checkNoStaleFiles walks v2/steps and v2/index once each and flags any file
// the validator did not consume above. This catches "left-over files from a
// previous generation" — e.g., a step that was later removed, or a stray
// file from a generator bug.
func (v *validator) checkNoStaleFiles() {
	roots := []string{
		path.Join(steplibindex.VersionDir(), steplibindex.StepsRootFS),
		path.Join(steplibindex.VersionDir(), steplibindex.IndexRootFS),
	}
	for _, root := range roots {
		if walkErr := fs.WalkDir(v.fs, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				// A missing root (e.g. v2/steps when there are no steps) isn't a
				// traversal failure — the empty steplib is already flagged via the
				// step_ids check. Any other walk error is reported, not swallowed.
				if !errors.Is(err, fs.ErrNotExist) {
					v.flag(p, "walk failed: %s", err)
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !v.seen[p] {
				v.flag(p, "unexpected file under %s/", root)
			}
			return nil
		}); walkErr != nil {
			// The callback handles each entry's error and always returns nil, so
			// this only fires if that ever changes — don't let it be dropped.
			v.flag(root, "walk aborted: %s", walkErr)
		}
	}
}
