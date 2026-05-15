package steplibrary

import (
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
)

// Steplib is a stub for the steplib v2 experiment.
// TODO: implement the v2 step retrieval flow.
type Steplib struct {
	log           stepman.Logger
	steplibURI    string
	isOfflineMode bool
}

func New(log stepman.Logger, steplibURI string, isOfflineMode bool) *Steplib {
	return &Steplib{
		log:           log,
		steplibURI:    steplibURI,
		isOfflineMode: isOfflineMode,
	}
}

func (s *Steplib) Activate(id, version string) (models.StepInfoModel, error) {
	return models.StepInfoModel{}, nil
}
