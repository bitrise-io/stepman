package steplibrary

import (
	"context"

	"github.com/bitrise-io/stepman/stepman"
)

// sourceProvider resolves the local directory holding a step's extracted
// source for a resolved version. Client depends on this interface so the
// source layer can be faked in tests without a function field.
type sourceProvider interface {
	stepSourceDir(ctx context.Context, step ResolvedStepVersion) (string, error)
}

// v1Source resolves step source through stepman's V1 on-disk cache.
type v1Source struct {
	steplibURI    string
	isOfflineMode bool
	log           stepman.Logger
}

func (p v1Source) stepSourceDir(_ context.Context, step ResolvedStepVersion) (string, error) {
	return stepman.GetStepSourceDir(p.steplibURI, step.ID, step.Version, p.log, p.isOfflineMode)
}
