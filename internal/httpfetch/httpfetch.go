// Package httpfetch is a minimal HTTP client wrapper that exposes a streaming
// Get plus an atomic Download (temp file in dest dir + rename). It's the
// shared transport for stepman's V2 inventory fetches and precompiled
// binary downloads.
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

	"log/slog"

	"github.com/bartventer/httpcache"
	// Registers the "memcache" store scheme used by inventoryCacheDSN.
	_ "github.com/bartventer/httpcache/store/memcache"
	"github.com/hashicorp/go-retryablehttp"
)

// inventoryCacheDSN selects the library's in-memory store. Nothing is written
// to disk: the cache lives and dies with the process.
const inventoryCacheDSN = "memcache://"

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
	// DownloadWithHash behaves like Download but also verifies that the
	// downloaded content matches expectedHash ("sha256-<hex>"). The temp file
	// is removed and an error is returned if the hash does not match, so a
	// mismatched file never appears at destPath.
	DownloadWithHash(ctx context.Context, destPath, url, expectedHash string) error
}

// Logger is the minimal logging interface Client needs; the retry adapter only
// emits debug lines.
type Logger interface {
	Debugf(format string, v ...any)
}

// slogHandler routes the cache library's slog output to our Logger, so cache
// hits, misses and revalidations show up under --debug alongside everything
// else. Without it the library defaults to a discard handler and the cache is
// invisible in exactly the situation someone would be debugging.
type slogHandler struct{ l Logger }

func (h slogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h slogHandler) Handle(_ context.Context, r slog.Record) error {
	msg := r.Message
	r.Attrs(func(a slog.Attr) bool {
		msg += " " + a.String()
		return true
	})
	h.l.Debugf("httpcache: %s", msg)
	return nil
}

// WithAttrs and WithGroup are required by slog.Handler. The cache's output is
// flat debug lines, so grouping buys nothing and the receiver is returned
// unchanged rather than building a tree we would only flatten again.
func (h slogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h slogHandler) WithGroup(string) slog.Handler      { return h }

// retryhttpLogger adapts Logger to the retryablehttp.Logger interface (Printf only).
type retryhttpLogger struct{ l Logger }

func (r *retryhttpLogger) Printf(f string, v ...any) { r.l.Debugf(f, v...) }

type client struct {
	httpClient *http.Client
}

// NewClient returns a Client backed by a retryablehttp client, so callers get
// transient-failure retries by default. Use NewWithClient to supply a specific
// *http.Client (e.g. a test server's client).
func NewClient(logger Logger) Client {
	rc := retryablehttp.NewClient()
	rc.Logger = &retryhttpLogger{l: logger}
	rc.ErrorHandler = retryablehttp.PassthroughErrorHandler
	return &client{httpClient: rc.StandardClient()}
}

// Clients are the two HTTP clients a stepman run needs. They share one
// retryablehttp client, and therefore one connection pool, so every request a
// run makes can reuse the same connections.
//
// They are a struct rather than two return values because both have the same
// type: a transposed pair would compile and then quietly cache the wrong half.
type Clients struct {
	// Inventory reads StepLib V2 inventory JSON through an in-memory HTTP
	// cache that honours Cache-Control and revalidates with ETags. The
	// inventory is small and every step re-reads it, so this is where the
	// caching pays.
	Inventory Client

	// Downloads fetches step archives and precompiled executables, uncached.
	// They are large, immutable, already hash-verified, and land at their own
	// paths, so holding them in memory would cost a lot and save nothing.
	Downloads Client
}

// NewClients builds the pair over a single shared connection pool.
func NewClients(logger Logger) (Clients, error) {
	rc := retryablehttp.NewClient()
	rc.Logger = &retryhttpLogger{l: logger}
	rc.ErrorHandler = retryablehttp.PassthroughErrorHandler

	// StandardClient returns a fresh *http.Client each call, both backed by
	// this one retryablehttp client - which is where the connection pool
	// lives, so the two share it.
	downloads := rc.StandardClient()
	inventory := rc.StandardClient()

	// The cache sits above the retries: a hit returns without entering the
	// retry machinery, while a revalidation still gets retried on 5xx.
	cached, err := newCachingTransport(logger, downloads.Transport)
	if err != nil {
		return Clients{}, err
	}
	inventory.Transport = cached

	return Clients{
		Inventory: &client{httpClient: inventory},
		Downloads: &client{httpClient: downloads},
	}, nil
}

// newCachingTransport wraps upstream with the in-memory response cache.
// httpcache.NewTransport panics with ErrOpenCache rather than returning an
// error when the store cannot be opened, so turn that one back into an error:
// a cache that is only an optimisation must not take down a build. Anything
// else is a genuine bug and keeps its stack.
func newCachingTransport(logger Logger, upstream http.RoundTripper) (rt http.RoundTripper, err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if rerr, ok := r.(error); ok && errors.Is(rerr, httpcache.ErrOpenCache) {
			rt, err = nil, fmt.Errorf("open in-memory HTTP cache: %w", rerr)
			return
		}
		panic(r)
	}()
	return httpcache.NewTransport(
		inventoryCacheDSN,
		httpcache.WithUpstream(cacheSuccessesOnly{upstream: upstream}),
		httpcache.WithLogger(slog.New(slogHandler{l: logger})),
	), nil
}

// cacheSuccessesOnly stops the cache storing anything but a 2xx.
//
// The cache stores any final status carrying a freshness indicator, and
// retryablehttp is configured with PassthroughErrorHandler, so a 502 that
// survives the retries is handed back as a normal response. If the origin's
// error page carries the same "max-age=60" the inventory paths use, that 502
// gets stored and served as fresh for the next minute - with no request and no
// retries - so one blip fails every remaining step of the run instead of one.
//
// Marking non-2xx responses no-store keeps them out. It does not change what
// the caller sees: httpfetch.Get rejects every non-2xx anyway.
type cacheSuccessesOnly struct{ upstream http.RoundTripper }

func (c cacheSuccessesOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.upstream.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	// 304 is how revalidation succeeds; the cache consumes it and must not be
	// told to discard the entry it belongs to.
	if resp.StatusCode == http.StatusNotModified {
		return resp, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Header.Set("Cache-Control", "no-store")
	}
	return resp, nil
}

// NewWithClient returns a Client backed by the given httpClient, which must be
// non-nil. Prefer NewClient unless you need a specific transport.
func NewWithClient(httpClient *http.Client) Client {
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
		snippet, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, errors.Join(
			&StatusError{URL: url, Code: resp.StatusCode, Body: string(bytes.TrimSpace(snippet))},
			readErr,
			resp.Body.Close(),
		)
	}
	return resp.Body, nil
}

// StatusError is returned by Get when the server responds with a non-2xx
// status, so callers can branch on the code (e.g. treat 404 as "not found")
// via errors.As. Body holds a bounded snippet of the response body, which
// usually explains the failure (a 404 page, S3's XML error, …).
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
// renames it into place. When expectedHash is non-empty the content is verified
// against it ("sha256-<hex>") before the rename, so a mismatched or partial
// file never lands at destPath.
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
		return fmt.Errorf("hash mismatch (%s) expected %s, got %s", url, expectedHash, hash)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpPath, destPath, err)
	}
	return nil
}

// fetchToTemp streams url into a new temp file under dir and returns its path
// and sha256 hash ("sha256-<hex>"). On error the temp file is removed and
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
	return tmp.Name(), "sha256-" + hex.EncodeToString(h.Sum(nil)), nil
}
