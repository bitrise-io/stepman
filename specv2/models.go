// Package specv2 contains the Go types and generator for the V2 step library
// inventory layout described in STEP-2374-plan.md.
//
// The V2 layout splits the inventory into two prefixes:
//   - steps/  — source of truth, self-contained per step, immutable per version
//   - spec/   — derived index files, regeneratable from steps/, short-TTL
//
// All files are JSON. Types here are the canonical on-disk shapes; the
// generator (Generate) walks an input bitrise-steplib clone and produces a
// V2 tree under an output directory.
package specv2

import (
	"time"

	envmanModels "github.com/bitrise-io/envman/v2/models"
	"github.com/bitrise-io/stepman/models"
)

// FormatVersion is the on-disk schema version. Bump major on breaking changes.
const FormatVersion = "2.0.0"

// MetaJSON is the inventory-level metadata file at the inventory root.
type MetaJSON struct {
	FormatVersion         string                         `json:"format_version"`
	UpdatedAt             time.Time                      `json:"updated_at"`
	SteplibCommitSHA      string                         `json:"steplib_commit_sha,omitempty"`
	SteplibSource         string                         `json:"steplib_source,omitempty"`
	DownloadLocations     []models.DownloadLocationModel `json:"download_locations,omitempty"`
	AssetsDownloadBaseURI string                         `json:"assets_download_base_uri,omitempty"`
}

// StepInfoJSON is the per-step metadata file at steps/<id>/step-info.json.
// Holds facts that span versions: maintainer, deprecation, asset list.
// Asset URLs are RELATIVE to the file's own location for self-containment.
type StepInfoJSON struct {
	Maintainer  string            `json:"maintainer,omitempty"`
	Deprecation *DeprecationJSON  `json:"deprecation"`
	AssetURLs   map[string]string `json:"asset_urls,omitempty"`
}

// DeprecationJSON carries the removal_date and notes for a deprecated step.
// nil at the StepInfoJSON.Deprecation field indicates an active step.
type DeprecationJSON struct {
	RemovalDate string `json:"removal_date,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// StepJSON is the per-version step manifest at steps/<id>/<version>/step.json.
// Replaces step.yml. Immutable once published.
type StepJSON struct {
	FormatVersion string `json:"format_version"`
	ID            string `json:"id"`
	Version       string `json:"version"`

	Title       string `json:"title,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`

	Website       string `json:"website,omitempty"`
	SourceCodeURL string `json:"source_code_url,omitempty"`
	SupportURL    string `json:"support_url,omitempty"`

	Source      *models.StepSourceModel   `json:"source,omitempty"`
	Executables map[string]ExecutableJSON `json:"executables,omitempty"`

	HostOsTags      []string `json:"host_os_tags,omitempty"`
	ProjectTypeTags []string `json:"project_type_tags,omitempty"`
	TypeTags        []string `json:"type_tags,omitempty"`

	Toolkit             *models.StepToolkitModel `json:"toolkit,omitempty"`
	Deps                *models.DepsModel        `json:"deps,omitempty"`
	Dependencies        []models.DependencyModel `json:"dependencies,omitempty"`
	IsRequiresAdminUser *bool                    `json:"is_requires_admin_user,omitempty"`
	IsAlwaysRun         *bool                    `json:"is_always_run,omitempty"`
	IsSkippable         *bool                    `json:"is_skippable,omitempty"`
	RunIf               string                   `json:"run_if,omitempty"`
	Timeout             *int                     `json:"timeout,omitempty"`
	NoOutputTimeout     *int                     `json:"no_output_timeout,omitempty"`
	Meta                map[string]any           `json:"meta,omitempty"`

	ExecutionContainer models.ContainerReference   `json:"execution_container,omitempty"`
	ServiceContainers  []models.ContainerReference `json:"service_containers,omitempty"`

	Inputs  []envmanModels.EnvironmentItemModel `json:"inputs,omitempty"`
	Outputs []envmanModels.EnvironmentItemModel `json:"outputs,omitempty"`
}

// ExecutableJSON describes a single prebuilt binary for a platform.
// The Location field accepts either an absolute URL (resolved at generation
// time from today's bitrise-steplib-storage bucket) or a path relative to
// the step version directory. Clients sniff by "http://" / "https://" prefix.
type ExecutableJSON struct {
	Location string `json:"location"`
	Hash     string `json:"hash"`
}

// StepIDsJSON is spec/step_ids.json: bare list of valid step IDs.
type StepIDsJSON struct {
	FormatVersion string   `json:"format_version"`
	StepIDs       []string `json:"step_ids"`
}

// LatestVersionsJSON is spec/latest_versions.json: fat catalog for browse views.
// Single fetch gives WFE / Integrations Page / `stepman list` everything they
// need to render a catalog without per-step round trips.
type LatestVersionsJSON struct {
	FormatVersion    string                  `json:"format_version"`
	GeneratedAt      time.Time               `json:"generated_at"`
	SteplibCommitSHA string                  `json:"steplib_commit_sha,omitempty"`
	Steps            map[string]CatalogEntry `json:"steps"`
}

// CatalogEntry is one step's entry in LatestVersionsJSON.Steps. Asset URLs
// are pre-resolved to absolute URLs here (unlike StepInfoJSON) so consumers
// can render without knowing the inventory base URL.
type CatalogEntry struct {
	LatestVersion   string            `json:"latest_version"`
	PublishedAt     *time.Time        `json:"published_at,omitempty"`
	Title           string            `json:"title,omitempty"`
	Summary         string            `json:"summary,omitempty"`
	Maintainer      string            `json:"maintainer,omitempty"`
	TypeTags        []string          `json:"type_tags,omitempty"`
	ProjectTypeTags []string          `json:"project_type_tags,omitempty"`
	HostOsTags      []string          `json:"host_os_tags,omitempty"`
	Website         string            `json:"website,omitempty"`
	SourceCodeURL   string            `json:"source_code_url,omitempty"`
	SupportURL      string            `json:"support_url,omitempty"`
	AssetURLs       map[string]string `json:"asset_urls,omitempty"`
	HasExecutable   bool              `json:"has_executable"`
	Deprecation     *DeprecationJSON  `json:"deprecation"`
}

// AllStepVersionsJSON is spec/all_step_versions.json: step_id → list of versions.
// Bare index, no per-version metadata; use spec/steps/<id>/versions.json for that.
type AllStepVersionsJSON struct {
	FormatVersion string              `json:"format_version"`
	Steps         map[string][]string `json:"steps"`
}

// LatestPointerJSON is spec/steps/<id>/latest.json: per-step latest pointers.
// Answers Latest and MajorLocked constraints in a single small fetch.
type LatestPointerJSON struct {
	StepID        string            `json:"step_id"`
	Latest        string            `json:"latest"`
	LatestByMajor map[string]string `json:"latest_by_major,omitempty"`
}

// VersionsJSON is spec/steps/<id>/versions.json: per-step version list with
// the metadata stepman needs for MinorLocked resolution and binary-availability
// checks. Ordered newest-first.
type VersionsJSON struct {
	StepID   string         `json:"step_id"`
	Latest   string         `json:"latest"`
	Versions []VersionEntry `json:"versions"`
}

// VersionEntry is a single entry in VersionsJSON.Versions.
type VersionEntry struct {
	Version       string     `json:"version"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	HasExecutable bool       `json:"has_executable"`
	Commit        string     `json:"commit,omitempty"`
}
