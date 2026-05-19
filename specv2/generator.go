package specv2

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
	"gopkg.in/yaml.v2"
)

// Options control generator behavior. Zero values are filled with sensible
// defaults; callers (CLI / tests) override what they need.
type Options struct {
	// GeneratedAt is written to meta.json and latest_versions.json.
	// Tests should set this for deterministic output.
	GeneratedAt time.Time
	// SteplibCommitSHA, if set, is written to meta.json and latest_versions.json.
	SteplibCommitSHA string
}

// Stats summarizes a successful generation.
type Stats struct {
	StepCount    int
	VersionCount int
	FilesWritten int
	BytesWritten int64
	Duration     time.Duration
}

// Generate reads a bitrise-steplib clone at inputDir and writes the V2 inventory
// tree to outputDir. It is destructive in the sense that it writes files; it
// does NOT delete existing files outside the paths it owns.
func Generate(inputDir, outputDir string, opts Options, log stepman.Logger) (Stats, error) {
	start := time.Now()
	opts = withDefaults(opts)

	steplibYML, err := readSteplibYML(inputDir)
	if err != nil {
		return Stats{}, fmt.Errorf("read steplib.yml: %w", err)
	}

	steps, err := collectSteps(inputDir, log)
	if err != nil {
		return Stats{}, err
	}

	w := &writer{outputDir: outputDir, fileCount: 0, byteCount: 0}

	for _, s := range steps {
		if err := writeStepFiles(w, inputDir, s); err != nil {
			return Stats{}, fmt.Errorf("write step %s: %w", s.id, err)
		}
	}

	if err := writeSpecFiles(w, steps, opts); err != nil {
		return Stats{}, fmt.Errorf("write spec files: %w", err)
	}

	meta := MetaJSON{
		FormatVersion:     FormatVersion,
		UpdatedAt:         opts.GeneratedAt,
		SteplibCommitSHA:  opts.SteplibCommitSHA,
		SteplibSource:     steplibYML.SteplibSource,
		DownloadLocations: steplibYML.DownloadLocations,
	}
	if err := w.writeJSON("meta.json", meta); err != nil {
		return Stats{}, fmt.Errorf("write meta.json: %w", err)
	}

	versionCount := 0
	for _, s := range steps {
		versionCount += len(s.versions)
	}
	return Stats{
		StepCount:    len(steps),
		VersionCount: versionCount,
		FilesWritten: w.fileCount,
		BytesWritten: w.byteCount,
		Duration:     time.Since(start),
	}, nil
}

// withDefaults fills zero-valued options.
func withDefaults(o Options) Options {
	if o.GeneratedAt.IsZero() {
		o.GeneratedAt = time.Now().UTC()
	}
	return o
}

// parsedStep is the intermediate representation collected during the walk
// phase, used by the write phase to emit per-step and index files.
type parsedStep struct {
	id          string
	info        StepInfoJSON // step-info.yml + assets/ listing
	hasInfoFile bool         // whether step-info.yml existed
	assetFiles  []string     // relative paths under assets/, sorted
	versions    map[string]models.StepModel
	versionList []string // sorted ascending by semver
	latest      string   // highest semver in versionList
}

func collectSteps(inputDir string, log stepman.Logger) ([]parsedStep, error) {
	stepsDir := filepath.Join(inputDir, "steps")
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", stepsDir, err)
	}

	var out []parsedStep
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := collectStep(stepsDir, e.Name(), log)
		if err != nil {
			return nil, err
		}
		if len(s.versions) == 0 {
			log.Warnf("step %s has no parseable versions, skipping", s.id)
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

func collectStep(stepsDir, id string, log stepman.Logger) (parsedStep, error) {
	s := parsedStep{
		id:          id,
		info:        StepInfoJSON{Maintainer: "", Deprecation: nil, AssetURLs: nil},
		hasInfoFile: false,
		assetFiles:  nil,
		versions:    map[string]models.StepModel{},
		versionList: nil,
		latest:      "",
	}
	stepDir := filepath.Join(stepsDir, id)

	info, hasInfo, err := readStepInfo(filepath.Join(stepDir, "step-info.yml"))
	if err != nil {
		return s, fmt.Errorf("read step-info.yml for %s: %w", id, err)
	}
	s.info = info
	s.hasInfoFile = hasInfo

	assetFiles, err := listAssets(filepath.Join(stepDir, "assets"))
	if err != nil {
		return s, fmt.Errorf("list assets for %s: %w", id, err)
	}
	s.assetFiles = assetFiles
	if len(assetFiles) > 0 {
		if s.info.AssetURLs == nil {
			s.info.AssetURLs = make(map[string]string, len(assetFiles))
		}
		for _, f := range assetFiles {
			s.info.AssetURLs[f] = "assets/" + f
		}
	}

	subEntries, err := os.ReadDir(stepDir)
	if err != nil {
		return s, fmt.Errorf("read %s: %w", stepDir, err)
	}
	for _, sub := range subEntries {
		if !sub.IsDir() {
			continue
		}
		if sub.Name() == "assets" {
			continue
		}
		if _, err := models.ParseSemver(sub.Name()); err != nil {
			log.Warnf("step %s: version dir %q is not semver, skipping", id, sub.Name())
			continue
		}
		stepYML := filepath.Join(stepDir, sub.Name(), "step.yml")
		step, err := parseStepYML(stepYML)
		if err != nil {
			return s, fmt.Errorf("parse %s/%s: %w", id, sub.Name(), err)
		}
		s.versions[sub.Name()] = step
	}

	s.versionList = sortedSemver(s.versions)
	if len(s.versionList) > 0 {
		s.latest = s.versionList[len(s.versionList)-1]
	}
	return s, nil
}

func readSteplibYML(inputDir string) (models.StepCollectionModel, error) {
	pth := filepath.Join(inputDir, "steplib.yml")
	bytes, err := os.ReadFile(pth)
	if err != nil {
		return models.StepCollectionModel{}, err
	}
	var c models.StepCollectionModel
	if err := yaml.Unmarshal(bytes, &c); err != nil {
		return models.StepCollectionModel{}, err
	}
	return c, nil
}

func readStepInfo(path string) (StepInfoJSON, bool, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StepInfoJSON{}, false, nil
		}
		return StepInfoJSON{}, false, err
	}
	var sgi models.StepGroupInfoModel
	if err := yaml.Unmarshal(bytes, &sgi); err != nil {
		return StepInfoJSON{}, true, err
	}
	out := StepInfoJSON{
		Maintainer:  sgi.Maintainer,
		Deprecation: nil,
		AssetURLs:   nil,
	}
	if sgi.RemovalDate != "" || sgi.DeprecateNotes != "" {
		out.Deprecation = &DeprecationJSON{
			RemovalDate: sgi.RemovalDate,
			Notes:       sgi.DeprecateNotes,
		}
	}
	return out, true, nil
}

func listAssets(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

func parseStepYML(path string) (models.StepModel, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return models.StepModel{}, err
	}
	var step models.StepModel
	if err := yaml.Unmarshal(bytes, &step); err != nil {
		return models.StepModel{}, fmt.Errorf("yaml unmarshal: %w", err)
	}
	if err := step.Normalize(); err != nil {
		return models.StepModel{}, fmt.Errorf("normalize: %w", err)
	}
	return step, nil
}

// sortedSemver returns the keys of m sorted ascending by semver. Keys that
// don't parse as semver are silently dropped (collectStep already warned).
func sortedSemver(m map[string]models.StepModel) []string {
	parsed := make([]models.Semver, 0, len(m))
	keyByStr := make(map[string]string, len(m))
	for k := range m {
		v, err := models.ParseSemver(k)
		if err != nil {
			continue
		}
		parsed = append(parsed, v)
		keyByStr[v.String()] = k
	}
	sort.Slice(parsed, func(i, j int) bool { return models.CmpSemver(parsed[i], parsed[j]) < 0 })
	out := make([]string, 0, len(parsed))
	for _, v := range parsed {
		out = append(out, keyByStr[v.String()])
	}
	return out
}

// ---------------------------------------------------------------------------
// step-level writes (steps/<id>/...)
// ---------------------------------------------------------------------------

func writeStepFiles(w *writer, inputDir string, s parsedStep) error {
	if s.hasInfoFile || len(s.assetFiles) > 0 {
		if err := w.writeJSON(filepath.Join("steps", s.id, "step-info.json"), s.info); err != nil {
			return err
		}
	}
	for _, f := range s.assetFiles {
		src := filepath.Join(inputDir, "steps", s.id, "assets", f)
		dst := filepath.Join("steps", s.id, "assets", f)
		if err := w.copyFile(src, dst); err != nil {
			return fmt.Errorf("copy asset %s: %w", src, err)
		}
	}
	for _, v := range s.versionList {
		step := s.versions[v]
		if err := w.writeJSON(filepath.Join("steps", s.id, v, "step.json"), step); err != nil {
			return err
		}
	}
	return nil
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ---------------------------------------------------------------------------
// spec/ writes (derived index files)
// ---------------------------------------------------------------------------

func writeSpecFiles(w *writer, steps []parsedStep, opts Options) error {
	ids := make([]string, len(steps))
	for i, s := range steps {
		ids[i] = s.id
	}
	if err := w.writeJSON("spec/step_ids.json", StepIDsJSON{StepIDs: ids}); err != nil {
		return err
	}

	allVersions := make(map[string][]string, len(steps))
	for _, s := range steps {
		allVersions[s.id] = s.versionList
	}
	if err := w.writeJSON("spec/all_step_versions.json", AllStepVersionsJSON{Steps: allVersions}); err != nil {
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

func buildCatalog(steps []parsedStep, opts Options) LatestVersionsJSON {
	out := LatestVersionsJSON{
		GeneratedAt:      opts.GeneratedAt,
		SteplibCommitSHA: opts.SteplibCommitSHA,
		Steps:            make(map[string]CatalogEntry, len(steps)),
	}
	for _, s := range steps {
		latestStep := s.versions[s.latest]
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
		out.Steps[s.id] = CatalogEntry{
			LatestVersion:   s.latest,
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
	return out
}

// catalogAssetURL produces the inventory-root-relative path the catalog
// emits for a given asset. The relPath comes from step-info.json (which
// is step-dir-relative, e.g. "assets/icon.svg"); we prepend "steps/<id>/"
// so the result is anchored at the inventory root.
func catalogAssetURL(stepID, relPath string) string {
	return "steps/" + stepID + "/" + relPath
}

func buildLatestPointer(s parsedStep) LatestPointerJSON {
	byMajor := map[string]models.Semver{}
	for _, v := range s.versionList {
		sv, err := models.ParseSemver(v)
		if err != nil {
			continue
		}
		majorKey := fmt.Sprintf("%d", sv.Major)
		cur, ok := byMajor[majorKey]
		if !ok || models.CmpSemver(sv, cur) > 0 {
			byMajor[majorKey] = sv
		}
	}
	latestByMajor := make(map[string]string, len(byMajor))
	for k, v := range byMajor {
		latestByMajor[k] = v.String()
	}
	return LatestPointerJSON{
		StepID:        s.id,
		Latest:        s.latest,
		LatestByMajor: latestByMajor,
	}
}

func buildVersionsJSON(s parsedStep) VersionsJSON {
	entries := make([]VersionEntry, 0, len(s.versionList))
	// Newest-first order: walk versionList in reverse.
	for i := len(s.versionList) - 1; i >= 0; i-- {
		v := s.versionList[i]
		step := s.versions[v]
		var publishedAt *time.Time
		if step.PublishedAt != nil && !step.PublishedAt.IsZero() {
			publishedAt = step.PublishedAt
		}
		commit := ""
		if step.Source != nil {
			commit = step.Source.Commit
		}
		entries = append(entries, VersionEntry{
			Version:       v,
			PublishedAt:   publishedAt,
			HasExecutable: step.Executables != nil && len(*step.Executables) > 0,
			Commit:        commit,
		})
	}
	return VersionsJSON{
		StepID:   s.id,
		Latest:   s.latest,
		Versions: entries,
	}
}

// ---------------------------------------------------------------------------
// writer — tracks file count + byte count for Stats
// ---------------------------------------------------------------------------

type writer struct {
	outputDir string
	fileCount int
	byteCount int64
}

func (w *writer) writeJSON(relPath string, v any) error {
	bytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	full := filepath.Join(w.outputDir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, bytes, 0o644); err != nil {
		return err
	}
	w.fileCount++
	w.byteCount += int64(len(bytes))
	return nil
}

func (w *writer) copyFile(src, relDst string) error {
	dst := filepath.Join(w.outputDir, relDst)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	n, err := io.Copy(out, in)
	if err != nil {
		return err
	}
	w.fileCount++
	w.byteCount += n
	return nil
}
