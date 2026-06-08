package steplibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/steplibindex"
)

// HTTPAPI fetches the V2 inventory layout (step_ids.json, latest.json,
// versions.json, step-info.json, step.json) over HTTP from a base URL.
// JSON endpoints are decoded in memory and returned as structs.
type HTTPAPI struct {
	BaseURL string
	Fetcher httpfetch.Client
}

func NewHTTPAPI(baseURL string, fetcher httpfetch.Client) *HTTPAPI {
	return &HTTPAPI{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Fetcher: fetcher,
	}
}

func (h *HTTPAPI) GetAllStepIDs(ctx context.Context) ([]string, error) {
	var payload steplibindex.StepIDs
	if err := h.fetchJSON(ctx, steplibindex.StepIDsPathURL(), &payload); err != nil {
		return nil, err
	}
	return payload.StepIDs, nil
}

func (h *HTTPAPI) GetLatestStepVersions(ctx context.Context, id string) (steplibindex.LatestPointer, error) {
	var out steplibindex.LatestPointer
	err := h.fetchJSON(ctx, steplibindex.LatestPointerPathURL(id), &out)
	return out, err
}

func (h *HTTPAPI) GetAllStepVersions(ctx context.Context, id string) ([]string, error) {
	var payload steplibindex.Versions
	if err := h.fetchJSON(ctx, steplibindex.VersionsPathURL(id), &payload); err != nil {
		return nil, err
	}
	return payload.Versions, nil
}

func (h *HTTPAPI) GetStepGroupInfo(ctx context.Context, id string) (steplibindex.StepInfo, error) {
	//nolint:exhaustruct // Deprecation is optional, nil means active
	out := steplibindex.StepInfo{}
	err := h.fetchJSON(ctx, steplibindex.StepInfoPathURL(id), &out)
	return out, err
}

// GetStepModel fetches the V2 step manifest (`steps/<id>/<v>/step.json`,
// which serialises models.StepModel) and returns the decoded model.
func (h *HTTPAPI) GetStepModel(ctx context.Context, step ResolvedStepVersion) (models.StepModel, error) {
	//nolint:exhaustruct // server JSON dictates which fields are populated
	var out models.StepModel
	err := h.fetchJSON(ctx, steplibindex.StepJSONPathURL(step.ID, step.Version), &out)
	return out, err
}

func (h *HTTPAPI) fetchJSON(ctx context.Context, path string, dst any) (err error) {
	body, err := h.Fetcher.Get(ctx, h.BaseURL+path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close response body for %s%s: %w", h.BaseURL, path, cerr)
		}
	}()
	if derr := json.NewDecoder(body).Decode(dst); derr != nil {
		return fmt.Errorf("decode %s%s: %w", h.BaseURL, path, derr)
	}
	return nil
}
