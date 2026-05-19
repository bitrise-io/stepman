package steplibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/filedownloader"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/stepman/models"
)

// HTTPAPI fetches the V2 inventory layout (step_ids.json, latest.json,
// versions.json, step-info.json, step.json, src.zip) over HTTP from a base URL.
// JSON endpoints are returned as decoded structs; path-returning endpoints
// download the payload to CacheDir and return the local path.
type HTTPAPI struct {
	BaseURL    string
	Client     *http.Client
	Downloader filedownloader.Downloader
	CacheDir   string
}

func NewHTTPAPI(baseURL, cacheDir string, client *http.Client) *HTTPAPI {
	if client == nil {
		client = http.DefaultClient
	}
	// Wrap into a filedownloader for binary downloads; discard log output by default.
	dl := filedownloader.NewDownloaderWithClient(client, log.NewLogger(log.WithOutput(io.Discard)))
	return &HTTPAPI{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Client:     client,
		Downloader: dl,
		CacheDir:   cacheDir,
	}
}

func (h *HTTPAPI) GetAllStepIDs(ctx context.Context) ([]string, error) {
	var payload struct {
		FormatVersion string   `json:"format_version"`
		StepIDs       []string `json:"step_ids"`
	}
	if err := h.fetchJSON(ctx, "/spec/step_ids.json", &payload); err != nil {
		return nil, err
	}
	return payload.StepIDs, nil
}

func (h *HTTPAPI) GetLatestStepVersions(ctx context.Context, id string) (StepVersionsLatest, error) {
	var out StepVersionsLatest
	err := h.fetchJSON(ctx, fmt.Sprintf("/spec/steps/%s/latest.json", url.PathEscape(id)), &out)
	return out, err
}

func (h *HTTPAPI) GetAllStepVersions(ctx context.Context, id string) ([]string, error) {
	var payload struct {
		StepID   string `json:"step_id"`
		Latest   string `json:"latest"`
		Versions []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	if err := h.fetchJSON(ctx, fmt.Sprintf("/spec/steps/%s/versions.json", url.PathEscape(id)), &payload); err != nil {
		return nil, err
	}
	out := make([]string, len(payload.Versions))
	for i, v := range payload.Versions {
		out[i] = v.Version
	}
	return out, nil
}

func (h *HTTPAPI) GetStepGroupInfo(ctx context.Context, id string) (StepGroupInfo, error) {
	//nolint:exhaustruct // Deprecation is optional, nil means active
	out := StepGroupInfo{}
	err := h.fetchJSON(ctx, fmt.Sprintf("/steps/%s/step-info.json", url.PathEscape(id)), &out)
	return out, err
}

// GetStepModel fetches the V2 step manifest (`steps/<id>/<v>/step.json`,
// which serialises models.StepModel) and returns the decoded model.
func (h *HTTPAPI) GetStepModel(ctx context.Context, step ResolvedStepVersion) (models.StepModel, error) {
	//nolint:exhaustruct // server JSON dictates which fields are populated
	var out models.StepModel
	err := h.fetchJSON(
		ctx,
		fmt.Sprintf("/steps/%s/%s/step.json", url.PathEscape(step.ID), url.PathEscape(step.Version)),
		&out,
	)
	return out, err
}

func (h *HTTPAPI) GetStepSourceZIPPath(ctx context.Context, step ResolvedStepVersion) (string, error) {
	return h.download(
		ctx,
		fmt.Sprintf("/steps/%s/%s/src.zip", url.PathEscape(step.ID), url.PathEscape(step.Version)),
		filepath.Join(h.CacheDir, "steps", step.ID, step.Version, "src.zip"),
	)
}

// GetStepPrecompiledPath errors with not-implemented for now: the precompiled
// binary URL lives inside step.json's `executables[<platform>]` map, so this
// requires fetching step.json first and resolving by runtime OS+arch.
func (h *HTTPAPI) GetStepPrecompiledPath(_ context.Context, _ ResolvedStepVersion) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (h *HTTPAPI) fetchJSON(ctx context.Context, path string, dst any) (err error) {
	body, err := h.get(ctx, path)
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

func (h *HTTPAPI) download(ctx context.Context, path, destPath string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return "", fmt.Errorf("create cache dir for %s: %w", destPath, err)
	}

	// Place the temp file in the destination's parent directory so the final
	// os.Rename stays within one filesystem (cross-filesystem rename fails on
	// most kernels, which would happen if we used os.TempDir on Linux/macOS).
	tmp, err := os.CreateTemp(filepath.Dir(destPath), "download-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file in %s: %w", filepath.Dir(destPath), err)
	}
	tmpPath := tmp.Name()
	// filedownloader.Download re-opens via os.Create; release our handle first.
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file %s: %w", tmpPath, err)
	}

	src := h.BaseURL + path
	if err := h.Downloader.Download(ctx, tmpPath, src); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("download %s: %w", src, err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("rename cache file %s: %w", tmpPath, err)
	}
	return destPath, nil
}

func (h *HTTPAPI) get(ctx context.Context, path string) (io.ReadCloser, error) {
	u := h.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", u, err)
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: unexpected status %d", u, resp.StatusCode)
	}
	return resp.Body, nil
}
