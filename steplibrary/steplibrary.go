package steplibrary

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/stepman"
	"gopkg.in/yaml.v2"
)

type Client struct {
	log stepman.Logger
	// steplibURI is set by the `default_step_lib_source` property in bitrise.yml
	steplibURI  string
	api         API
	fileManager fileutil.FileManager
	fetcher     httpfetch.Client
}

type ActivateOutputPaths struct {
	YMLPath, CodePath string
}

// New builds a Client. steplibURI is the steplib identity; inventoryURL is the
// base URL the V2 inventory JSON is fetched from.
func New(log stepman.Logger, steplibURI, inventoryURL string, fileManager fileutil.FileManager) *Client {
	return &Client{
		log:         log,
		steplibURI:  steplibURI,
		api:         NewHTTPAPI(inventoryURL, httpfetch.NewClient(log)),
		fileManager: fileManager,
		fetcher:     httpfetch.NewClient(log),
	}
}

func (c *Client) Activate(ctx context.Context, stepID, version string, outputPaths ActivateOutputPaths) (ActivatedStep, error) {
	stepInfo, resolved, err := c.getStepVersionInfo(ctx, stepID, version)
	if err != nil {
		return ActivatedStep{}, fmt.Errorf("resolve step version: %w", err)
	}

	stepModel, err := c.api.GetStepModel(ctx, resolved)
	if err != nil {
		return ActivatedStep{}, fmt.Errorf("fetch step definition: %w", err)
	}

	// Prefer the precompiled binary for the current platform when the step
	// publishes one; on any failure fall back to source activation.
	execPath := ""
	if executable, ok := ResolveExecutable(stepModel); ok {
		path, perr := DownloadPrecompiled(ctx, c.fetcher, c.log, stepID, executable, outputPaths.CodePath)
		if perr != nil {
			c.log.Warnf("Failed to download precompiled binary for %s, falling back to source: %s", currentPlatform(), perr)
		} else {
			execPath = path
		}
	}

	stepYML, err := yaml.Marshal(stepModel)
	if err != nil {
		return ActivatedStep{}, fmt.Errorf("marshal step model to YAML: %w", err)
	}
	if err := c.fileManager.WriteBytes(outputPaths.YMLPath, stepYML); err != nil {
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
