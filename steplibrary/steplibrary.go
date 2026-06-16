package steplibrary

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/activator/result"
	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/stepman"
	"gopkg.in/yaml.v2"
)

type Steplib struct {
	log stepman.Logger
	// steplibURI is the steplib *identity* — the URI the user references in
	// bitrise.yml (e.g. the official git URL). It is reported as
	// StepInfoModel.Library. It is NOT the URL the V2 inventory is fetched
	// from; that is the inventory URL held by the HTTP API.
	steplibURI  string
	api         API
	fileManager fileutil.FileManager
	fetcher     httpfetch.Client
	source      sourceProvider
}

type ActivateOutputPaths struct {
	YMLPath, CodePath string
}

// New builds a Steplib. steplibURI is the steplib identity (the user's
// bitrise.yml URI, used for the V1 cache and source fallback); inventoryURL is
// the base URL the V2 inventory JSON is fetched from. They differ for the
// official steplib, whose git identity is rewritten to a compiled-in V2 host.
func New(log stepman.Logger, steplibURI, inventoryURL string, isOfflineMode bool, fileManager fileutil.FileManager) *Steplib {
	return &Steplib{
		log:         log,
		steplibURI:  steplibURI,
		api:         NewHTTPAPI(inventoryURL, httpfetch.NewClient(log)),
		fileManager: fileManager,
		fetcher:     httpfetch.NewClient(log),
		source:      v1Source{steplibURI: steplibURI, isOfflineMode: isOfflineMode, log: log},
	}
}

func (s *Steplib) Activate(ctx context.Context, stepID, version string, outputPaths ActivateOutputPaths) (result.ActivatedStep, error) {
	stepInfo, resolved, err := s.getStepVersionInfo(ctx, stepID, version)
	if err != nil {
		return result.ActivatedStep{}, err
	}

	stepModel, err := s.api.GetStepModel(ctx, resolved)
	if err != nil {
		return result.ActivatedStep{}, err
	}

	// Prefer the precompiled binary for the current platform when the step
	// publishes one; transparently fall back to source on any failure so an
	// individual broken executable can't block activation.
	var execPath string
	if executable, ok := resolveExecutable(stepModel); ok {
		path, perr := s.downloadPrecompiled(ctx, stepID, executable, outputPaths.CodePath)
		if perr == nil {
			execPath = path
		} else {
			s.log.Warnf("Failed to download precompiled binary for %s, falling back to source: %s", currentPlatform(), perr)
		}
	}

	if execPath == "" {
		srcDir, serr := s.source.stepSourceDir(ctx, resolved)
		if serr != nil {
			return result.ActivatedStep{}, serr
		}
		if cerr := s.fileManager.CopyDir(srcDir, outputPaths.CodePath, &fileutil.CopyOptions{Overwrite: true}); cerr != nil {
			return result.ActivatedStep{}, fmt.Errorf("copy step source %s to %s: %w", srcDir, outputPaths.CodePath, cerr)
		}
	}

	stepYML, err := yaml.Marshal(stepModel)
	if err != nil {
		return result.ActivatedStep{}, fmt.Errorf("marshal step model to YAML: %w", err)
	}

	if err := s.fileManager.WriteBytes(outputPaths.YMLPath, stepYML); err != nil {
		return result.ActivatedStep{}, err
	}

	activationType := result.ActivationTypeSteplibSource
	if execPath != "" {
		activationType = result.ActivationTypeSteplibExecutable
	}
	return result.ActivatedStep{
		StepInfo:         stepInfo,
		StepYMLPath:      outputPaths.YMLPath,
		ExecutablePath:   execPath,
		ActivationType:   activationType,
		DidStepLibUpdate: false, // deprecated
	}, nil
}
