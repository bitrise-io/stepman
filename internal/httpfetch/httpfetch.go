// Package httpfetch is a minimal HTTP client wrapper that exposes a streaming
// Get plus an atomic Download (temp file in dest dir + rename). It's the
// shared transport for stepman's V2 inventory fetches and precompiled
// binary downloads.
package httpfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
)

// Client streams or atomically downloads HTTP resources. Implementations
// returned by NewClient retry transient failures on both Get and Download.
type Client interface {
	// Get streams the body of url. The caller closes the returned reader.
	// Non-2xx responses are returned as an error.
	Get(ctx context.Context, url string) (io.ReadCloser, error)
	// Download fetches url and atomically writes it to destPath. Missing
	// parent directories are created. A temp file is created alongside
	// destPath and renamed on success so partial downloads never appear at
	// the final path.
	Download(ctx context.Context, destPath, url string) error
}

type client struct {
	httpClient *http.Client
}

// NewClient returns a Client backed by httpClient. When httpClient is nil
// a retryablehttp-backed client is used, so production callers get
// transient-failure retries by default.
func NewClient(httpClient *http.Client, logger log.Logger) Client {
	if httpClient == nil {
		httpClient = retryhttp.NewClient(logger).StandardClient()
	}
	return &client{httpClient: httpClient}
}

func (c *client) Get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}
	return resp.Body, nil
}

func (c *client) Download(ctx context.Context, destPath, url string) (err error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create dest dir for %s: %w", destPath, err)
	}

	// Place the temp file alongside destPath so the final rename stays on
	// one filesystem (cross-filesystem renames fail on most kernels).
	tmp, err := os.CreateTemp(filepath.Dir(destPath), "download-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", filepath.Dir(destPath), err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	body, gerr := c.Get(ctx, url)
	if gerr != nil {
		_ = tmp.Close()
		cleanup()
		return gerr
	}

	_, copyErr := io.Copy(tmp, body)
	_ = body.Close()
	if copyErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write to %s: %w", tmpPath, copyErr)
	}
	if cerr := tmp.Close(); cerr != nil {
		cleanup()
		return fmt.Errorf("close %s: %w", tmpPath, cerr)
	}
	if rerr := os.Rename(tmpPath, destPath); rerr != nil {
		cleanup()
		return fmt.Errorf("rename %s to %s: %w", tmpPath, destPath, rerr)
	}
	return nil
}
