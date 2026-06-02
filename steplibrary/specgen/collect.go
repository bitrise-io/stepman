package specgen

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/spec"
	"github.com/bitrise-io/stepman/stepman"
	"gopkg.in/yaml.v2"
)

// parsedStep is the intermediate representation collected during the walk
// phase, used by the write phase to emit per-step and index files.
type parsedStep struct {
	id          string
	info        spec.StepInfo   // step-info.yml + assets/ listing
	hasInfoFile bool            // whether step-info.yml existed
	assetFiles  []string        // relative paths under assets/, sorted
	versions    []parsedVersion // sorted ascending by semver; last is latest
}

// parsedVersion is a single step version with its semver parsed once at collect
// time, so the write/index phase never re-parses the version string.
type parsedVersion struct {
	version string
	semver  models.Semver
	model   models.StepModel
}

// latest returns the highest-semver version. Only valid for steps with at least
// one version (collectSteps drops versionless steps before they reach here).
func (s parsedStep) latest() parsedVersion { return s.versions[len(s.versions)-1] }

func readSteplibYML(inputFS fs.FS) (models.StepCollectionModel, error) {
	bytes, err := fs.ReadFile(inputFS, "steplib.yml")
	if err != nil {
		return models.StepCollectionModel{}, err
	}
	var c models.StepCollectionModel
	if err := yaml.Unmarshal(bytes, &c); err != nil {
		return models.StepCollectionModel{}, err
	}
	return c, nil
}

func readStepGroupInfo(inputFS fs.FS, path string) (spec.StepInfo, bool, error) {
	bytes, err := fs.ReadFile(inputFS, path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return spec.StepInfo{}, false, nil
		}
		return spec.StepInfo{}, false, err
	}
	var sgi models.StepGroupInfoModel
	if err := yaml.Unmarshal(bytes, &sgi); err != nil {
		return spec.StepInfo{}, true, err
	}
	out := spec.StepInfo{
		Maintainer:  sgi.Maintainer,
		Deprecation: nil,
		AssetURLs:   nil,
	}
	if sgi.RemovalDate != "" || sgi.DeprecateNotes != "" {
		out.Deprecation = &spec.Deprecation{
			RemovalDate: sgi.RemovalDate,
			Notes:       sgi.DeprecateNotes,
		}
	}
	return out, true, nil
}

func listAssets(inputFS fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(inputFS, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
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

func parseStepYML(inputFS fs.FS, path string) (models.StepModel, error) {
	bytes, err := fs.ReadFile(inputFS, path)
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
	// Mirror the canonical V1 parse pipeline (stepman.ParseStepDefinition):
	// Normalize + FillMissingDefaults. Without this, optional fields the V1
	// spec.json fills (IsAlwaysRun, IsSkippable, IsRequiresAdminUser, Timeout,
	// empty-string metadata) would serialize as null in the V2 step.json, so
	// the same step.yml would yield different output across V1 and V2.
	if err := step.FillMissingDefaults(); err != nil {
		return models.StepModel{}, fmt.Errorf("fill missing defaults: %w", err)
	}
	return step, nil
}

func collectStep(inputFS fs.FS, id string, log stepman.Logger) (parsedStep, error) {
	s := parsedStep{
		id:          id,
		info:        spec.StepInfo{Maintainer: "", Deprecation: nil, AssetURLs: nil},
		hasInfoFile: false,
		assetFiles:  nil,
		versions:    nil,
	}
	stepDir := "steps/" + id

	info, hasInfo, err := readStepGroupInfo(inputFS, stepDir+"/step-info.yml")
	if err != nil {
		return s, fmt.Errorf("read step-info.yml for %s: %w", id, err)
	}
	s.info = info
	s.hasInfoFile = hasInfo

	assetFiles, err := listAssets(inputFS, stepDir+"/assets")
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

	subEntries, err := fs.ReadDir(inputFS, stepDir)
	if err != nil {
		return s, fmt.Errorf("read %s: %w", stepDir, err)
	}
	for _, sub := range subEntries {
		if !sub.IsDir() || sub.Name() == "assets" {
			continue
		}
		sv, err := models.ParseSemver(sub.Name())
		if err != nil {
			log.Warnf("step %s: version dir %q is not semver, skipping", id, sub.Name())
			continue
		}
		model, err := parseStepYML(inputFS, stepDir+"/"+sub.Name()+"/step.yml")
		if err != nil {
			return s, fmt.Errorf("parse %s/%s: %w", id, sub.Name(), err)
		}
		s.versions = append(s.versions, parsedVersion{version: sub.Name(), semver: sv, model: model})
	}

	sort.Slice(s.versions, func(i, j int) bool {
		return models.CmpSemver(s.versions[i].semver, s.versions[j].semver) < 0
	})
	return s, nil
}

func collectSteps(inputFS fs.FS, log stepman.Logger) ([]parsedStep, error) {
	entries, err := fs.ReadDir(inputFS, "steps")
	if err != nil {
		return nil, fmt.Errorf("read steps: %w", err)
	}

	var out []parsedStep
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := collectStep(inputFS, e.Name(), log)
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
