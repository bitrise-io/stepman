package steplibrary

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/stepid"
	"github.com/bitrise-io/stepman/stepman"
	"gopkg.in/yaml.v2"
)

type Client struct {
	log          stepman.Logger
	inventoryURL string
	api          API
	fileManager  fileutil.FileManager
}

type ActivateOutputPaths struct {
	YMLPath, CodePath string
}

// New builds a Client. inventoryURL is the base URL the V2 inventory JSON is
// fetched from.
func New(log stepman.Logger, inventoryURL string, fileManager fileutil.FileManager) *Client {
	return &Client{
		log:          log,
		inventoryURL: inventoryURL,
		api:          NewHTTPAPI(inventoryURL, httpfetch.NewClient(log)),
		fileManager:  fileManager,
	}
}

func (c *Client) FetchStepMetadata(ctx context.Context, stepRef stepid.CanonicalID, outputYMLPath string) (ActivateResult, error) {
	stepInfo, resolved, err := c.getStepVersionInfo(ctx, stepRef.IDorURI, stepRef.Version)
	if err != nil {
		return ActivateResult{}, fmt.Errorf("resolve step version: %w", err)
	}

	stepModel, err := c.api.GetStepModel(ctx, resolved)
	if err != nil {
		return ActivateResult{}, fmt.Errorf("fetch step definition: %w", err)
	}
	stepInfo.Step = stepModel

	stepYML, err := yaml.Marshal(stepModel)
	if err != nil {
		return ActivateResult{}, fmt.Errorf("marshal step model to YAML: %w", err)
	}

	if err := c.fileManager.WriteBytes(outputYMLPath, stepYML); err != nil {
		return ActivateResult{}, fmt.Errorf("write step.yml: %w", err)
	}

	return ActivateResult{
		StepInfo:    stepInfo,
		StepYMLPath: outputYMLPath,
	}, nil
}
