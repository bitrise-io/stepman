// Package httpcache provides a transparent on-disk HTTP cache as an
// http.RoundTripper. It honors the server's Cache-Control/ETag, serves fresh
// entries without touching the network, and revalidates stale entries with
// conditional requests.
//
// When revalidation fails (network error or 5xx) the failure is surfaced to the
// caller rather than masked by serving a stale entry: a broken upstream should
// fail loudly, not silently resolve against possibly-outdated inventory. A
// deliberate offline mode that prefers stale entries is planned separately.
//
// Entries live one-directory-per-response under a hash+name layout (see Store),
// keeping the cache both machine-addressable and human-browsable.
package httpcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"time"
)

// Logger is the minimal logging surface the cache needs: Debugf for hit/miss
// tracing and Warnf for the stale-fallback notice (a degraded-mode signal worth
// surfacing). stepman.Logger satisfies it.
type Logger interface {
	Debugf(format string, v ...any)
	Warnf(format string, v ...any)
}

// Transport is an http.RoundTripper that caches GET responses on disk via the
// embedded Store and delegates the actual network fetch to base.
type Transport struct {
	base  http.RoundTripper
	store *Store
	log   Logger
}

// NewTransport returns a caching RoundTripper. base performs the real network
// fetches (e.g. the default pooled transport); store holds the cached entries.
func NewTransport(base http.RoundTripper, store *Store, log Logger) *Transport {
	return &Transport{base: base, store: store, log: log}
}

// RoundTrip implements http.RoundTripper. Only GET is cached; every other
// method is passed straight through to base.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return t.base.RoundTrip(req)
	}

	key := cacheKey(req)
	meta, hit, err := t.store.Lookup(key)
	if err != nil {
		t.log.Debugf("httpcache: lookup %s failed: %s", req.URL, err)
		hit = false
	}

	if hit && isFresh(meta, time.Now()) {
		resp, serr := t.serve(req, key, meta, "fresh")
		if serr == nil {
			return resp, nil
		}
		t.log.Debugf("httpcache: serving fresh %s failed: %s; refetching", req.URL, serr)
		hit = false
	}

	if hit {
		return t.revalidate(req, key, meta)
	}
	return t.fetchAndStore(req, key)
}

// revalidate is reached only when a stale entry exists. It issues a conditional
// request and: serves the cached body on 304; replaces it on 2xx; surfaces the
// failure on a transport error or 5xx (no stale fallback — see package doc).
func (t *Transport) revalidate(req *http.Request, key string, meta Meta) (*http.Response, error) {
	condReq := req.Clone(req.Context())
	if meta.ETag != "" {
		condReq.Header.Set("If-None-Match", meta.ETag)
	}
	if meta.LastModified != "" {
		condReq.Header.Set("If-Modified-Since", meta.LastModified)
	}

	resp, err := t.base.RoundTrip(condReq)
	if err != nil {
		// Surface the error; retryablehttp retries it, then it reaches the caller.
		return nil, err
	}

	switch {
	case resp.StatusCode == http.StatusNotModified:
		t.drain(req.URL.String(), resp.Body)
		refreshed := meta
		applyCacheHeaders(&refreshed, resp, time.Now())
		if terr := t.store.Touch(key, refreshed); terr != nil {
			t.log.Debugf("httpcache: touch %s failed: %s", req.URL, terr)
		}
		return t.serve(req, key, refreshed, "revalidated (304)")

	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return t.store200(req, key, resp)

	default:
		// 5xx and any other non-2xx (e.g. 404): pass through unchanged. The 5xx
		// case is retried by retryablehttp and ultimately surfaced as an error;
		// the stale entry is left in place for a future successful revalidation.
		return resp, nil
	}
}

// fetchAndStore handles a cache miss: fetch from base, store 2xx responses, pass
// everything else (errors, non-2xx) through unchanged.
func (t *Transport) fetchAndStore(req *http.Request, key string) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err // surfaced to retryablehttp, which retries
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return t.store200(req, key, resp)
	}
	return resp, nil
}

// store200 buffers a 2xx body, persists it (unless no-store), and returns a
// response whose body replays the buffer. Inventory files are small by design,
// so buffering in memory keeps the write atomic and the code simple.
func (t *Transport) store200(req *http.Request, key string, resp *http.Response) (*http.Response, error) {
	fetchedAt := time.Now()
	meta := Meta{
		URL:         req.URL.String(),
		Method:      req.Method,
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		BodyFile:    bodyFilename(req),
	}
	noStore := applyCacheHeaders(&meta, resp, fetchedAt)

	body, rerr := io.ReadAll(resp.Body)
	cerr := resp.Body.Close()
	if rerr != nil {
		return nil, errors.Join(fmt.Errorf("read body %s: %w", req.URL, rerr), cerr)
	}
	if cerr != nil {
		return nil, fmt.Errorf("close body %s: %w", req.URL, cerr)
	}

	if noStore {
		t.log.Debugf("httpcache: not storing %s (no-store)", req.URL)
	} else {
		meta.BodySize = int64(len(body))
		sum := sha256.Sum256(body)
		meta.BodySHA256 = "sha256-" + hex.EncodeToString(sum[:])
		if serr := t.store.Save(key, meta, body); serr != nil {
			// A failed write must not fail the request: the caller still has a
			// valid response; the cache just stays empty for this entry.
			t.log.Warnf("httpcache: storing %s failed: %s", req.URL, serr)
		} else {
			t.log.Debugf("httpcache: stored %s (%d bytes)", req.URL, meta.BodySize)
		}
	}

	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp, nil
}

// serve builds a synthesized 200 response from the cached entry. reason is a
// human-readable cause ("fresh", "revalidated (304)") used only for logging.
func (t *Transport) serve(req *http.Request, key string, meta Meta, reason string) (*http.Response, error) {
	body, err := t.store.Open(key, meta)
	if err != nil {
		return nil, err
	}
	t.log.Debugf("httpcache: serving %s cache for %s", reason, req.URL)

	header := http.Header{}
	setIfNotEmpty(header, "Content-Type", meta.ContentType)
	setIfNotEmpty(header, "ETag", meta.ETag)
	setIfNotEmpty(header, "Cache-Control", meta.CacheControl)
	setIfNotEmpty(header, "Last-Modified", meta.LastModified)
	header.Set("X-From-Cache", "1")

	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          body,
		ContentLength: meta.BodySize,
		Request:       req,
	}, nil
}

// drain consumes and closes a response body we are discarding (e.g. a 304 or a
// 5xx we fall back from), so the underlying connection can be reused.
func (t *Transport) drain(url string, body io.ReadCloser) {
	if body == nil {
		return
	}
	_, copyErr := io.Copy(io.Discard, io.LimitReader(body, 64<<10))
	closeErr := body.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		t.log.Debugf("httpcache: draining response for %s: %s", url, err)
	}
}

// applyCacheHeaders copies the validators and freshness directives from resp
// into m and returns whether the response must not be stored.
func applyCacheHeaders(m *Meta, resp *http.Response, now time.Time) (noStore bool) {
	// A 304 (and some 200s) may omit headers that were present originally; per
	// RFC 7234 §4.3.4 those must be preserved, not wiped. Validators already use
	// preserve-if-absent; Cache-Control must too, otherwise the first 304 that
	// drops the header collapses the entry's freshness window to zero and forces
	// a revalidation on every later request.
	cc := resp.Header.Get("Cache-Control")
	if cc == "" {
		cc = m.CacheControl
	}
	m.CacheControl = cc
	setIfNotEmptyField(&m.ETag, resp.Header.Get("ETag"))
	setIfNotEmptyField(&m.LastModified, resp.Header.Get("Last-Modified"))

	expiresAt, immutable, ns := parseCacheControl(cc, now)
	m.FetchedAt = now
	m.ExpiresAt = expiresAt
	m.Immutable = immutable
	return ns
}

func isFresh(m Meta, now time.Time) bool {
	if m.Immutable {
		return true
	}
	return now.Before(m.ExpiresAt)
}

// cacheKey is both the lookup key and the entry directory name:
// "<sha256(method+"\n"+url)>-<basename>". The hash guarantees uniqueness; the
// basename suffix makes the directory recognizable when browsing the cache.
func cacheKey(req *http.Request) string {
	sum := sha256.Sum256([]byte(req.Method + "\n" + req.URL.String()))
	return hex.EncodeToString(sum[:]) + "-" + bodyFilename(req)
}

// bodyFilename is the last path segment of the URL ("body" if there is none),
// used as the on-disk body filename so cached files keep their real names.
func bodyFilename(req *http.Request) string {
	base := path.Base(req.URL.Path)
	if base == "." || base == "/" || base == "" {
		return "body"
	}
	return base
}

func setIfNotEmpty(h http.Header, key, value string) {
	if value != "" {
		h.Set(key, value)
	}
}

func setIfNotEmptyField(dst *string, value string) {
	if value != "" {
		*dst = value
	}
}
