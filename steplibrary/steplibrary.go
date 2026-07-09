package steplibrary

import (
	"context"
	"fmt"

	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepid"
	"github.com/bitrise-io/stepman/stepman"
)

type Client struct {
	log          stepman.Logger
	inventoryURL string
	api          API
}

// New builds a stepman.Client.
// inventoryURL: the base URL of the API where metadata is fetched from.
func New(log stepman.Logger, inventoryURL string) *Client {
	return &Client{
		log:          log,
		inventoryURL: inventoryURL,
		api:          NewHTTPAPI(inventoryURL, httpfetch.NewClient(log)),
	}
}

func (c *Client) FetchStepMetadata(ctx context.Context, stepRef stepid.CanonicalID) (models.StepInfoModel, error) {
	stepInfo, resolved, err := c.getStepVersionInfo(ctx, stepRef.IDorURI, stepRef.Version)
	if err != nil {
		return models.StepInfoModel{}, fmt.Errorf("resolve step version: %w", err)
	}

	stepModel, err := c.api.GetStepModel(ctx, resolved)
	if err != nil {
		return models.StepInfoModel{}, fmt.Errorf("fetch step definition: %w", err)
	}
	stepInfo.Step = stepModel

	return stepInfo, nil
}

// StepDownloadLocations resolves the concrete source download locations for
// id@version from the inventory's base locations (meta.json download_locations),
// preserving their order: a "zip" base becomes <base>/<id>/<version>/step.zip and
// a "git" base becomes the step's own source git URL. For the bitrise steplib zip
// comes first, so callers try the zip fast path before falling back to a git clone.
func (c *Client) StepDownloadLocations(ctx context.Context, id, version, sourceGit string) ([]models.DownloadLocationModel, error) {
	bases, err := c.api.GetDownloadLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch download locations: %w", err)
	}

	var locations []models.DownloadLocationModel
	for _, base := range bases {
		switch base.Type {
		case "zip":
			locations = append(locations, models.DownloadLocationModel{
				Type: "zip",
				Src:  base.Src + id + "/" + version + "/step.zip",
			})
		case "git":
			if sourceGit != "" {
				locations = append(locations, models.DownloadLocationModel{Type: "git", Src: sourceGit})
			}
		}
	}
	return locations, nil
}
