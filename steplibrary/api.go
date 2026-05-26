package steplibrary

import (
	"context"

	"github.com/bitrise-io/stepman/models"
)

type ResolvedStepVersion struct {
	ID, Version string
}

// StepVersionsLatest mirrors `spec/steps/<id>/latest.json` from the V2 inventory
// layout. Resolves Latest and MajorLocked version constraints in a single fetch.
type StepVersionsLatest struct {
	StepID        string            `json:"step_id"`
	Latest        string            `json:"latest"`
	LatestByMajor map[string]string `json:"latest_by_major"`
}

// StepGroupInfo mirrors `steps/<id>/step-info.json` from the V2 inventory layout.
// Version-independent metadata for a step.
type StepGroupInfo struct {
	Maintainer  string            `json:"maintainer"`
	Deprecation *Deprecation      `json:"deprecation,omitempty"`
	AssetURLs   map[string]string `json:"asset_urls"`
}

// Deprecation carries the removal date and migration notes when a step is being
// retired. Nil on `StepGroupInfo.Deprecation` means the step is active.
type Deprecation struct {
	RemovalDate string `json:"removal_date"`
	Notes       string `json:"notes"`
}

type API interface {
	GetAllStepIDs(ctx context.Context) ([]string, error)
	GetLatestStepVersions(ctx context.Context, id string) (StepVersionsLatest, error)
	// GetAllStepVersions returns all available versions of a step.
	// Mirrors `spec/steps/<id>/versions.json` from the V2 inventory layout;
	// the per-version metadata is dropped for now since callers only need the
	// version strings to resolve MinorLocked constraints.
	GetAllStepVersions(ctx context.Context, id string) ([]string, error)
	// GetStepGroupInfo returns version-independent step metadata
	// (maintainer, deprecation, asset URLs). Mirrors `steps/<id>/step-info.json`.
	GetStepGroupInfo(ctx context.Context, id string) (StepGroupInfo, error)
	// GetStepModel fetches the V2 per-version step manifest (mirrors
	// `steps/<id>/<version>/step.json`, which serializes models.StepModel).
	GetStepModel(ctx context.Context, step ResolvedStepVersion) (models.StepModel, error)
	GetStepSourceZIPPath(ctx context.Context, step ResolvedStepVersion) (string, error)
}
