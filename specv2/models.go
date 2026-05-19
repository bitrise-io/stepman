// Package specv2 contains the Go types and generator for the V2 step library
// inventory layout described in STEP-2374-plan.md.
//
// The V2 layout splits the inventory into two prefixes:
//   - steps/  — source of truth, self-contained per step, immutable per version
//   - spec/   — derived index files, regeneratable from steps/, short-TTL
//
// All files are JSON. Per-version step manifests (steps/<id>/<v>/step.json)
// are V1's models.StepModel marshaled as JSON — same shape, different
// encoding. The other types below describe the new inventory-level and
// index files that have no V1 equivalent.
package specv2

import (
	"time"

	"github.com/bitrise-io/stepman/models"
)

// FormatVersion is the on-disk schema version recorded in meta.json. It is a
// single major-only integer: bump only on breaking changes (renamed or
// removed fields, changed semantics). Additive changes (new optional fields)
// DO NOT bump — consumers ignore unknown JSON keys.
//
// Per V1 convention, format_version is declared only at the inventory root;
// per-step and per-version files inherit it transitively.
const FormatVersion = 2

// MetaJSON is the inventory-level metadata file at the inventory root, and
// the only file that carries format_version.
type MetaJSON struct {
	FormatVersion     int                            `json:"format_version"`
	UpdatedAt         time.Time                      `json:"updated_at"`
	SteplibCommitSHA  string                         `json:"steplib_commit_sha,omitempty"`
	SteplibSource     string                         `json:"steplib_source,omitempty"`
	DownloadLocations []models.DownloadLocationModel `json:"download_locations,omitempty"`
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

// The per-version step manifest at steps/<id>/<version>/step.json is
// models.StepModel marshaled as JSON. models.StepModel already carries
// `json:"…"` tags alongside its `yaml:"…"` tags, so no specv2-specific
// struct is needed — the inventory's per-version file is the V1 step.yml,
// re-encoded.
//
// This preserves field-for-field compatibility with V1's audit/runtime
// code paths (activator/, cli/, toolkits/, etc.), which already operate on
// models.StepModel. A consumer that wants to read a V2 step.json calls
// json.Unmarshal into a models.StepModel; today's yaml.Unmarshal callers
// can swap parsers without any other change.

// StepIDsJSON is spec/step_ids.json: bare list of valid step IDs.
type StepIDsJSON struct {
	StepIDs []string `json:"step_ids"`
}

// LatestVersionsJSON is spec/latest_versions.json: fat catalog for browse views.
// Single fetch gives WFE / Integrations Page / `stepman list` everything they
// need to render a catalog without per-step round trips.
type LatestVersionsJSON struct {
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
	Steps map[string][]string `json:"steps"`
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
