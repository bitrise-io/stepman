package specgen

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/spec"
)

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// catalogAssetURL produces the inventory-root-relative path the catalog
// emits for a given asset. The relPath comes from step-info.json (which
// is step-dir-relative, e.g. "assets/icon.svg"); we prepend "steps/<id>/"
// so the result is anchored at the inventory root.
func catalogAssetURL(stepID, relPath string) string {
	return "steps/" + stepID + "/" + relPath
}

func buildCatalogEntry(s parsedStep) spec.CatalogEntry {
	latest := s.latest()
	latestStep := latest.model

	var publishedAt *time.Time
	if latestStep.PublishedAt != nil && !latestStep.PublishedAt.IsZero() {
		publishedAt = latestStep.PublishedAt
	}

	// Catalog asset URLs are INVENTORY-ROOT-RELATIVE. Catalog consumers
	// resolve them against the inventory base URL (i.e., the URL the
	// catalog itself was fetched from, with /spec/latest_versions.json
	// trimmed). This keeps the V2 inventory portable across hosting
	// changes — no V1-era S3 host is baked into the catalog payload.
	var assetURLs map[string]string
	if len(s.info.AssetURLs) > 0 {
		assetURLs = make(map[string]string, len(s.info.AssetURLs))
		for filename, relPath := range s.info.AssetURLs {
			assetURLs[filename] = catalogAssetURL(s.id, relPath)
		}
	}

	return spec.CatalogEntry{
		LatestVersion:   latest.version,
		PublishedAt:     publishedAt,
		Title:           derefStr(latestStep.Title),
		Summary:         derefStr(latestStep.Summary),
		Maintainer:      s.info.Maintainer,
		TypeTags:        latestStep.TypeTags,
		ProjectTypeTags: latestStep.ProjectTypeTags,
		HostOsTags:      latestStep.HostOsTags,
		Website:         derefStr(latestStep.Website),
		SourceCodeURL:   derefStr(latestStep.SourceCodeURL),
		SupportURL:      derefStr(latestStep.SupportURL),
		AssetURLs:       assetURLs,
		HasExecutable:   latestStep.Executables != nil && len(*latestStep.Executables) > 0,
		Deprecation:     s.info.Deprecation,
	}
}

func buildCatalog(steps []parsedStep, opts Options) spec.Catalog {
	out := spec.Catalog{
		GeneratedAt:      opts.GeneratedAt,
		SteplibCommitSHA: opts.SteplibCommitSHA,
		Steps:            make(map[string]spec.CatalogEntry, len(steps)),
	}
	for _, s := range steps {
		out.Steps[s.id] = buildCatalogEntry(s)
	}
	return out
}

func buildLatestPointer(s parsedStep) spec.LatestPointer {
	byMajor := map[string]models.Semver{}
	for _, v := range s.versions {
		majorKey := strconv.FormatUint(v.semver.Major, 10)
		cur, ok := byMajor[majorKey]
		if !ok || models.CmpSemver(v.semver, cur) > 0 {
			byMajor[majorKey] = v.semver
		}
	}
	latestByMajor := make(map[string]string, len(byMajor))
	for k, v := range byMajor {
		latestByMajor[k] = v.String()
	}
	return spec.LatestPointer{
		StepID:        s.id,
		Latest:        s.latest().version,
		LatestByMajor: latestByMajor,
	}
}

func buildVersionsJSON(s parsedStep) spec.Versions {
	entries := make([]spec.VersionEntry, 0, len(s.versions))
	// Newest-first order: walk the ascending-sorted versions in reverse.
	for i := len(s.versions) - 1; i >= 0; i-- {
		v := s.versions[i]
		step := v.model
		var publishedAt *time.Time
		if step.PublishedAt != nil && !step.PublishedAt.IsZero() {
			publishedAt = step.PublishedAt
		}
		commit := ""
		if step.Source != nil {
			commit = step.Source.Commit
		}
		entries = append(entries, spec.VersionEntry{
			Version:       v.version,
			PublishedAt:   publishedAt,
			HasExecutable: step.Executables != nil && len(*step.Executables) > 0,
			Commit:        commit,
		})
	}
	return spec.Versions{
		StepID:   s.id,
		Latest:   s.latest().version,
		Versions: entries,
	}
}

// writeStepFiles emits the per-step source files under steps/<id>/.
func writeStepFiles(w *writer, inputFS fs.FS, s parsedStep) error {
	if s.hasInfoFile || len(s.assetFiles) > 0 {
		if err := w.writeJSON(filepath.Join("steps", s.id, "step-info.json"), s.info); err != nil {
			return err
		}
	}
	for _, f := range s.assetFiles {
		src := "steps/" + s.id + "/assets/" + f
		dst := filepath.Join("steps", s.id, "assets", f)
		if err := w.copyFileFromFS(inputFS, src, dst); err != nil {
			return fmt.Errorf("copy asset %s: %w", src, err)
		}
	}
	for _, v := range s.versions {
		if err := w.writeJSON(filepath.Join("steps", s.id, v.version, "step.json"), v.model); err != nil {
			return err
		}
	}
	return nil
}

// writeSpecFiles emits the derived index files under spec/.
func writeSpecFiles(w *writer, steps []parsedStep, opts Options) error {
	ids := make([]string, len(steps))
	for i, s := range steps {
		ids[i] = s.id
	}
	if err := w.writeJSON("spec/step_ids.json", spec.StepIDs{StepIDs: ids}); err != nil {
		return err
	}

	catalog := buildCatalog(steps, opts)
	if err := w.writeJSON("spec/latest_versions.json", catalog); err != nil {
		return err
	}

	for _, s := range steps {
		if err := w.writeJSON(filepath.Join("spec", "steps", s.id, "latest.json"), buildLatestPointer(s)); err != nil {
			return err
		}
		if err := w.writeJSON(filepath.Join("spec", "steps", s.id, "versions.json"), buildVersionsJSON(s)); err != nil {
			return err
		}
	}
	return nil
}
