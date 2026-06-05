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
			Version:     v.version,
			PublishedAt: publishedAt,
			Commit:      commit,
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
func writeSpecFiles(w *writer, steps []parsedStep) error {
	ids := make([]string, len(steps))
	for i, s := range steps {
		ids[i] = s.id
	}
	if err := w.writeJSON("spec/step_ids.json", spec.StepIDs{StepIDs: ids}); err != nil {
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
