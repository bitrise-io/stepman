// Package httpfetch is a minimal HTTP client wrapper that exposes a streaming
// Get plus an atomic Download (temp file in dest dir + rename). It's the
// shared transport for stepman's V2 inventory fetches and precompiled
// binary downloads.
//
// Every operation is bounded, so a stalled server fails the step instead of
// hanging the build. The bounds, from the inside out:
//
//   - Per attempt, the transport waits at most DefaultResponseHeaderTimeout
//     for the response headers once the request is written, on top of
//     cleanhttp's dial and TLS handshake limits. This is what detects a server that
//     accepted the connection and went silent, however large the body would
//     have been. Such an attempt is retried like any other network error.
//   - Transient failures (network errors, 5xx, 429) are retried up to retryMax
//     times with exponential backoff: 1s, 2s, 4s, 8s, 15s in total. A
//     Retry-After from the server is honoured up to retryWaitMax.
//   - Per operation, each client caps the whole thing, retries, backoff and
//     reading the body included: DefaultInventoryTimeout for inventory reads,
//     DefaultDownloadTimeout for archive and executable downloads. This is the
//     only bound on a body that stalls after the headers arrived.
//
// Worst cases: an inventory read fails within 60s. A download fails within
// 5min; a mirror that never answers is given up on after about 2m45s
// (5 attempts × 30s + 15s backoff), one that stalls mid-body after the full
// 5min.
package httpfetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/bartventer/httpcache/store/memcache"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/go-retryablehttp"
)

const (
	// DefaultResponseHeaderTimeout is how long one attempt waits for the
	// server's response headers after the request is written.
	DefaultResponseHeaderTimeout = 30 * time.Second
	// DefaultInventoryTimeout caps one inventory read, retries included.
	DefaultInventoryTimeout = 60 * time.Second
	// DefaultDownloadTimeout caps one archive or executable download, retries
	// and writing the body to disk included.
	DefaultDownloadTimeout = 5 * time.Minute

	// Retry policy for transient failures. Pinned here rather than taken from
	// retryablehttp's defaults so the worst cases documented above stay true
	// across upgrades.
	retryMax     = 4
	retryWaitMin = 1 * time.Second
	retryWaitMax = 30 * time.Second
)

// Client streams or atomically downloads HTTP resources. Implementations in
// this package retry transient failures and bound every Get and Download, as
// described in the package documentation.
type Client interface {
	// Get streams the body of url. The caller closes the returned reader.
	// Non-2xx responses are returned as an error.
	Get(ctx context.Context, url string) (io.ReadCloser, error)
	// Download fetches url and atomically writes it to destPath. Missing
	// parent directories are created. A temp file is created alongside
	// destPath and renamed on success so partial downloads never appear at
	// the final path.
	Download(ctx context.Context, destPath, url string) error
	// DownloadWithHash behaves like Download but also verifies that the
	// downloaded content's SHA256 digest, hex-encoded, matches expectedHash.
	// The temp file is removed and an error is returned if the digest does
	// not match, so a mismatched file never appears at destPath.
	DownloadWithHash(ctx context.Context, destPath, url, expectedHash string) error
}

// Clients are a pair of caching and passthrough clients.
// They share one connection pool, for performance.
type Clients struct {
	// Caching reads through a HTTP cache that honours Cache-Control and revalidates with ETags.
	// It serves the inventory, bounded by DefaultInventoryTimeout per read.
	Caching Client
	// Passthrough fetches step archives and precompiled executables, uncached,
	// bounded by DefaultDownloadTimeout per download.
	Passthrough Client
}

// Logger is the minimal logging interface Client needs; the retry adapter only
// emits debug lines.
type Logger interface {
	Debugf(format string, v ...any)
}

// retryhttpLogger adapts Logger to the retryablehttp.Logger interface (Printf only).
type retryhttpLogger struct{ l Logger }

func (r *retryhttpLogger) Printf(f string, v ...any) { r.l.Debugf(f, v...) }

type client struct {
	httpClient *http.Client
	// timeout caps one Get or Download, retries and body included.
	timeout time.Duration
}

func newClient(httpClient *http.Client, timeout time.Duration) *client {
	return &client{httpClient: httpClient, timeout: timeout}
}

// newTransport returns the pooled transport the clients share, with the
// per-attempt response header bound on top of cleanhttp's dial and TLS limits.
func newTransport(responseHeaderTimeout time.Duration) *http.Transport {
	t := cleanhttp.DefaultPooledTransport()
	t.ResponseHeaderTimeout = responseHeaderTimeout
	return t
}

// newRetryingClient returns an *http.Client that retries transient failures
// over transport.
func newRetryingClient(logger Logger, transport http.RoundTripper) *http.Client {
	rc := retryablehttp.NewClient()
	rc.HTTPClient.Transport = transport
	rc.RetryMax = retryMax
	rc.RetryWaitMin = retryWaitMin
	rc.RetryWaitMax = retryWaitMax
	rc.Backoff = cappedBackoff
	rc.Logger = &retryhttpLogger{l: logger}
	rc.ErrorHandler = retryablehttp.PassthroughErrorHandler
	return rc.StandardClient()
}

// cappedBackoff is retryablehttp's default backoff, except that a server's
// Retry-After cannot stretch one wait past waitMax.
func cappedBackoff(waitMin, waitMax time.Duration, attemptNum int, resp *http.Response) time.Duration {
	return min(retryablehttp.DefaultBackoff(waitMin, waitMax, attemptNum, resp), waitMax)
}

// NewClient returns an uncached Client for step archives and precompiled
// executables, bounded by DefaultDownloadTimeout per operation. Inventory
// reads belong on the caching client from NewCachingClient.
func NewClient(logger Logger) Client {
	return newClient(newRetryingClient(logger, newTransport(DefaultResponseHeaderTimeout)), DefaultDownloadTimeout)
}

// NewCachingClient returns a caching and a passthrough Client. Both run over the
// same retrying transport, so they share one connection pool.
func NewCachingClient(logger Logger) (Clients, error) {
	// The cache is layered above the retries, so a 500 is retried before it is stored.
	passthrough := newRetryingClient(logger, newTransport(DefaultResponseHeaderTimeout))
	caching, err := NewCachingWithClient(logger, passthrough)
	if err != nil {
		return Clients{}, err
	}

	return Clients{
		Caching:     caching,
		Passthrough: newClient(passthrough, DefaultDownloadTimeout),
	}, nil
}

// NewUncachedClients returns the pair NewCachingClient would, without the
// inventory cache, for when that cache cannot be opened. Each keeps its bound.
func NewUncachedClients(logger Logger) Clients {
	hc := newRetryingClient(logger, newTransport(DefaultResponseHeaderTimeout))
	return Clients{
		Caching:     newClient(hc, DefaultInventoryTimeout),
		Passthrough: newClient(hc, DefaultDownloadTimeout),
	}
}

// NewCachingWithClient returns a caching Client bounded by
// DefaultInventoryTimeout per read.
// Prefer NewCachingClient unless you need a specific transport.
func NewCachingWithClient(logger Logger, httpClient *http.Client) (Client, error) {
	upstream := httpClient.Transport
	if upstream == nil {
		upstream = http.DefaultTransport // what net/http itself uses for a nil Transport
	}

	cached, err := newCachingTransport(logger, upstream)
	if err != nil {
		return nil, err
	}

	caching := *httpClient
	caching.Transport = cached
	return newClient(&caching, DefaultInventoryTimeout), nil
}

// NewWithClient returns an uncached Client backed by the given httpClient,
// bounded by DefaultDownloadTimeout per operation.
// Prefer NewClient unless you need a specific transport.
func NewWithClient(httpClient *http.Client) Client {
	return newClient(httpClient, DefaultDownloadTimeout)
}

func (c *client) Get(parent context.Context, url string) (io.ReadCloser, error) {
	// One deadline for the whole operation: the retries and backoff inside Do,
	// then the body stream, released when the caller closes it.
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("GET %s: %w", url, c.explainTimeout(ctx, parent, err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if readErr != nil {
			readErr = c.explainTimeout(ctx, parent, readErr)
		}
		closeErr := resp.Body.Close()
		cancel()
		return nil, errors.Join(
			&StatusError{URL: url, Code: resp.StatusCode, Body: string(bytes.TrimSpace(snippet))},
			readErr,
			closeErr,
		)
	}
	return &boundedBody{
		ReadCloser: resp.Body,
		cancel:     cancel,
		explain:    func(err error) error { return c.explainTimeout(ctx, parent, err) },
	}, nil
}

// explainTimeout names the operation timeout in err when that deadline, not the
// caller's context, ended the request. It decides on the contexts rather than on
// err, because the transport's header timeout also matches context.DeadlineExceeded.
func (c *client) explainTimeout(ctx, parent context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil {
		return fmt.Errorf("gave up after %s, the limit for the whole operation including retries: %w", c.timeout, err)
	}
	return err
}

// boundedBody is a response body that releases the operation deadline on Close
// and explains a timeout that cuts the stream short.
type boundedBody struct {
	io.ReadCloser
	cancel  context.CancelFunc
	explain func(error) error
}

func (b *boundedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = b.explain(err)
	}
	return n, err
}

func (b *boundedBody) Close() error {
	defer b.cancel()
	return b.ReadCloser.Close()
}

// StatusError is returned by Get when the server responds with a non-2xx
// status, so callers can branch on the code (e.g. treat 404 as "not found")
// via errors.As.
type StatusError struct {
	URL  string
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GET %s: unexpected status %d: %s", e.URL, e.Code, e.Body)
}

func (c *client) Download(ctx context.Context, destPath, url string) error {
	return c.download(ctx, destPath, url, "")
}

func (c *client) DownloadWithHash(ctx context.Context, destPath, url, expectedHash string) error {
	if expectedHash == "" {
		return fmt.Errorf("hash is empty")
	}
	return c.download(ctx, destPath, url, expectedHash)
}

// download fetches url into a temp file alongside destPath and atomically
// renames it into place. When expectedHash is non-empty, the content's SHA256
// digest (hex-encoded) is verified against it before the rename, so a
// mismatched or partial file never lands at destPath.
func (c *client) download(ctx context.Context, destPath, url, expectedHash string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create dest dir for %s: %w", destPath, err)
	}

	tmpPath, hash, err := c.fetchToTemp(ctx, filepath.Dir(destPath), url)
	if err != nil {
		return err
	}
	// Best-effort cleanup: on a verify/rename failure this removes the temp
	// file; after a successful rename tmpPath is gone, so Remove fails harmlessly.
	defer func() { _ = os.Remove(tmpPath) }()

	if expectedHash != "" && hash != expectedHash {
		return fmt.Errorf("SHA256 hash mismatch (%s): expected %s, got %s", url, expectedHash, hash)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpPath, destPath, err)
	}
	return nil
}

// fetchToTemp streams url into a new temp file under dir and returns its path
// and its SHA256 digest, hex-encoded. On error the temp file is removed and
// path/hash are empty; on success the caller owns cleanup.
func (c *client) fetchToTemp(ctx context.Context, dir, url string) (path string, hash string, err error) {
	// Place the temp file alongside destPath so the final rename stays on
	// one filesystem (cross-filesystem renames fail on most kernels).
	tmp, err := os.CreateTemp(dir, "download-*.tmp")
	if err != nil {
		return "", "", fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	defer func() {
		// A failed Close is intentionally a hard failure: it can mean the final
		// write never flushed to disk, so the temp file may be incomplete.
		if closeErr := tmp.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close %s: %w", tmp.Name(), closeErr))
		}
		if err != nil {
			// Best-effort cleanup of the partial temp file; the original error is
			// what matters, so a failed remove is intentionally ignored.
			_ = os.Remove(tmp.Name())
			path = ""
			hash = ""
		}
	}()

	body, err := c.Get(ctx, url)
	if err != nil {
		return "", "", err
	}
	defer func() {
		if closeErr := body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close response body: %w", closeErr))
		}
	}()

	h := sha256.New()
	if _, copyErr := io.Copy(io.MultiWriter(tmp, h), body); copyErr != nil {
		return "", "", fmt.Errorf("write to %s: %w", tmp.Name(), copyErr)
	}
	return tmp.Name(), hex.EncodeToString(h.Sum(nil)), nil
}
