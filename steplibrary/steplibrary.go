package steplibrary

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/stepman"
	"gopkg.in/yaml.v2"
)

type Steplib struct {
	log stepman.Logger
	// steplibURI is set by the `default_step_lib_source` property in bitrise.yml
	steplibURI  string
	api         API
	fileManager fileutil.FileManager
	fetcher     httpfetch.Client
	source      sourceProvider
}

type ActivateOutputPaths struct {
	YMLPath, CodePath string
}

// New builds a Steplib. steplibURI is the steplib identity (used for the V1
// cache and source fallback); inventoryURL is the base URL the V2 inventory
// JSON is fetched from.
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

func (s *Steplib) Activate(ctx context.Context, stepID, version string, outputPaths ActivateOutputPaths) (ActivatedStep, error) {
	stepInfo, resolved, err := s.getStepVersionInfo(ctx, stepID, version)
	if err != nil {
		return ActivatedStep{}, fmt.Errorf("resolve step version: %w", err)
	}

	stepModel, err := s.api.GetStepModel(ctx, resolved)
	if err != nil {
		return ActivatedStep{}, fmt.Errorf("fetch step definition: %w", err)
	}

	// Prefer the precompiled binary for the current platform when the step
	// publishes one; fall back to source on any failure so a single broken
	// executable can't block activation.
	execPath := ""
	if executable, ok := resolveExecutable(stepModel); ok {
		path, perr := s.downloadPrecompiled(ctx, stepID, executable, outputPaths.CodePath)
		if perr != nil {
			s.log.Warnf("Failed to download precompiled binary for %s, falling back to source: %s", currentPlatform(), perr)
		} else {
			execPath = path
		}
	}

	if execPath == "" {
		srcDir, err := s.source.stepSourceDir(ctx, resolved)
		if err != nil {
			return ActivatedStep{}, fmt.Errorf("resolve step source: %w", err)
		}
		if err := s.fileManager.CopyDir(srcDir, outputPaths.CodePath, &fileutil.CopyOptions{Overwrite: true}); err != nil {
			return ActivatedStep{}, fmt.Errorf("copy step source %s to %s: %w", srcDir, outputPaths.CodePath, err)
		}
	}

	stepYML, err := yaml.Marshal(stepModel)
	if err != nil {
		return ActivatedStep{}, fmt.Errorf("marshal step model to YAML: %w", err)
	}
	if err := s.fileManager.WriteBytes(outputPaths.YMLPath, stepYML); err != nil {
		return ActivatedStep{}, fmt.Errorf("write step.yml: %w", err)
	}

	activationType := ActivationTypeSteplibSource
	if execPath != "" {
		activationType = ActivationTypeSteplibExecutable
	}
	return ActivatedStep{
		StepInfo:         stepInfo,
		StepYMLPath:      outputPaths.YMLPath,
		ExecutablePath:   execPath,
		ActivationType:   activationType,
		DidStepLibUpdate: false, // deprecated
	}, nil
}
