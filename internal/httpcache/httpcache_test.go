package httpcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testURL = "https://steplib.example/spec/steps/git-clone/latest.json"

// fakeRT is an in-memory http.RoundTripper. Keeping the network in-memory means
// every goroutine stays inside the synctest bubble, so the fake clock and
// quiescence behave (a real httptest.Server would spawn goroutines outside it).
type fakeRT struct {
	calls int
	fn    func(req *http.Request, callNum int) (*http.Response, error)
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls++
	return f.fn(req, f.calls)
}

func makeResp(status int, body string, header map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range header {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode:    status,
		Header:        h,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

// recLogger records Warnf lines so tests can assert the stale-fallback notice.
type recLogger struct{ warns []string }

func (l *recLogger) Debugf(string, ...any) {}
func (l *recLogger) Warnf(format string, v ...any) {
	l.warns = append(l.warns, fmt.Sprintf(format, v...))
}

type harness struct {
	t    *testing.T
	tr   *Transport
	base *fakeRT
	log  *recLogger
	root string
}

func newHarness(t *testing.T, fn func(req *http.Request, callNum int) (*http.Response, error)) *harness {
	t.Helper()
	root := t.TempDir()
	base := &fakeRT{fn: fn}
	log := &recLogger{}
	return &harness{
		t:    t,
		tr:   NewTransport(base, NewStore(root), log),
		base: base,
		log:  log,
		root: root,
	}
}

// get issues a GET through the cache and returns the status and body.
func (h *harness) get(url string) (int, string) {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(h.t, err)
	resp, err := h.tr.RoundTrip(req)
	require.NoError(h.t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(h.t, err)
	require.NoError(h.t, resp.Body.Close())
	return resp.StatusCode, string(body)
}

// readMeta loads the single cache entry's meta.json (fails if not exactly one).
func (h *harness) readMeta() Meta {
	h.t.Helper()
	entries, err := os.ReadDir(h.root)
	require.NoError(h.t, err)
	require.Len(h.t, entries, 1, "expected exactly one cache entry")
	data, err := os.ReadFile(filepath.Join(h.root, entries[0].Name(), metaFilename))
	require.NoError(h.t, err)
	var m Meta
	require.NoError(h.t, json.Unmarshal(data, &m))
	return m
}

func TestMissThenHit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(_ *http.Request, _ int) (*http.Response, error) {
			return makeResp(http.StatusOK, `{"latest":"8.5.0"}`, map[string]string{
				"Content-Type":  "application/json",
				"ETag":          `"v1"`,
				"Cache-Control": "public, max-age=60, must-revalidate",
			}), nil
		})

		status, body := h.get(testURL)
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, `{"latest":"8.5.0"}`, body)
		assert.Equal(t, 1, h.base.calls, "first GET hits the network")

		// Second GET while still fresh: served from disk, no network.
		status, body = h.get(testURL)
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, `{"latest":"8.5.0"}`, body)
		assert.Equal(t, 1, h.base.calls, "fresh hit must not hit the network")

		// Disk layout + metadata.
		entries, err := os.ReadDir(h.root)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.True(t, strings.HasSuffix(entries[0].Name(), "-latest.json"),
			"entry dir keeps the human-readable basename: %s", entries[0].Name())

		bodyPath := filepath.Join(h.root, entries[0].Name(), "latest.json")
		onDisk, err := os.ReadFile(bodyPath)
		require.NoError(t, err)
		assert.Equal(t, `{"latest":"8.5.0"}`, string(onDisk), "body stored under its real name")

		m := h.readMeta()
		assert.Equal(t, testURL, m.URL)
		assert.Equal(t, `"v1"`, m.ETag)
		assert.Equal(t, "latest.json", m.BodyFile)
		assert.Equal(t, int64(len(body)), m.BodySize)
		assert.True(t, strings.HasPrefix(m.BodySHA256, "sha256-"), "SRI hash format: %s", m.BodySHA256)
		assert.Equal(t, m.FetchedAt.Add(60*time.Second), m.ExpiresAt, "ExpiresAt = FetchedAt + max-age")
	})
}

func TestRevalidation304(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(req *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return makeResp(http.StatusOK, "BODY-A", map[string]string{
					"ETag":          `"v1"`,
					"Cache-Control": "max-age=60",
				}), nil
			}
			// Conditional request must carry the stored validator.
			assert.Equal(t, `"v1"`, req.Header.Get("If-None-Match"))
			return makeResp(http.StatusNotModified, "", map[string]string{
				"Cache-Control": "max-age=60",
			}), nil
		})

		_, body := h.get(testURL)
		require.Equal(t, "BODY-A", body)
		firstFetchedAt := h.readMeta().FetchedAt

		time.Sleep(61 * time.Second) // entry goes stale (fake clock, instant)

		status, body := h.get(testURL)
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, "BODY-A", body, "304 serves the cached body")
		assert.Equal(t, 2, h.base.calls, "stale entry triggers one revalidation")

		m := h.readMeta()
		assert.True(t, m.FetchedAt.After(firstFetchedAt), "304 refreshes FetchedAt")
		assert.Equal(t, m.FetchedAt.Add(60*time.Second), m.ExpiresAt)
	})
}

// TestRevalidation304PreservesMaxAge guards against a 304 that omits
// Cache-Control wiping the entry's original max-age. After revalidation the
// entry must be fresh again for the original window (served from disk without a
// further network call), not collapsed to revalidate-every-request.
func TestRevalidation304PreservesMaxAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(_ *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return makeResp(http.StatusOK, "BODY-A", map[string]string{
					"ETag":          `"v1"`,
					"Cache-Control": "public, max-age=60, must-revalidate",
				}), nil
			}
			// 304 deliberately carries NO Cache-Control header (common from CDNs).
			return makeResp(http.StatusNotModified, "", nil), nil
		})

		_, body := h.get(testURL)
		require.Equal(t, "BODY-A", body)

		time.Sleep(61 * time.Second) // stale -> triggers revalidation
		_, body = h.get(testURL)
		require.Equal(t, "BODY-A", body)
		require.Equal(t, 2, h.base.calls, "one revalidation after going stale")

		m := h.readMeta()
		assert.Equal(t, "public, max-age=60, must-revalidate", m.CacheControl,
			"original Cache-Control preserved across a header-less 304")
		assert.Equal(t, m.FetchedAt.Add(60*time.Second), m.ExpiresAt,
			"freshness window restored to the original max-age")

		// Within the restored window the entry is served from disk, no network.
		time.Sleep(30 * time.Second)
		_, body = h.get(testURL)
		assert.Equal(t, "BODY-A", body)
		assert.Equal(t, 2, h.base.calls, "must stay fresh for the original max-age, not revalidate every request")
	})
}

func TestRevalidation200Replacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(_ *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return makeResp(http.StatusOK, "BODY-A", map[string]string{
					"ETag":          `"v1"`,
					"Cache-Control": "max-age=60",
				}), nil
			}
			return makeResp(http.StatusOK, "BODY-B", map[string]string{
				"ETag":          `"v2"`,
				"Cache-Control": "max-age=60",
			}), nil
		})

		_, body := h.get(testURL)
		require.Equal(t, "BODY-A", body)

		time.Sleep(61 * time.Second)

		_, body = h.get(testURL)
		assert.Equal(t, "BODY-B", body, "new 200 replaces the cached body")
		assert.Equal(t, 2, h.base.calls)
		assert.Equal(t, `"v2"`, h.readMeta().ETag, "metadata updated to new validator")

		// Freshly stored entry is served from disk on the next call.
		_, body = h.get(testURL)
		assert.Equal(t, "BODY-B", body)
		assert.Equal(t, 2, h.base.calls, "replacement entry is fresh again")
	})
}

func TestImmutableNeverRevalidates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(_ *http.Request, _ int) (*http.Response, error) {
			return makeResp(http.StatusOK, "STEP-JSON", map[string]string{
				"Cache-Control": "public, max-age=31536000, immutable",
			}), nil
		})

		_, body := h.get(testURL)
		require.Equal(t, "STEP-JSON", body)

		time.Sleep(2 * 365 * 24 * time.Hour) // two years later

		_, body = h.get(testURL)
		assert.Equal(t, "STEP-JSON", body)
		assert.Equal(t, 1, h.base.calls, "immutable entry is never revalidated")
		assert.True(t, h.readMeta().Immutable)
	})
}

func TestRevalidationErrorSurfaced(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(_ *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return makeResp(http.StatusOK, "BODY-A", map[string]string{
					"ETag":          `"v1"`,
					"Cache-Control": "max-age=60",
				}), nil
			}
			return nil, errors.New("connection refused")
		})

		_, body := h.get(testURL)
		require.Equal(t, "BODY-A", body)

		time.Sleep(61 * time.Second)

		// No stale fallback: the transport error is surfaced to the caller.
		req, err := http.NewRequest(http.MethodGet, testURL, nil)
		require.NoError(t, err)
		_, rtErr := h.tr.RoundTrip(req)
		require.Error(t, rtErr, "revalidation transport error must be surfaced, not masked by stale")
		assert.Contains(t, rtErr.Error(), "connection refused")
		assert.Equal(t, 2, h.base.calls)
	})
}

func TestRevalidation5xxPassedThrough(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(_ *http.Request, n int) (*http.Response, error) {
			if n == 1 {
				return makeResp(http.StatusOK, "BODY-A", map[string]string{
					"ETag":          `"v1"`,
					"Cache-Control": "max-age=60",
				}), nil
			}
			return makeResp(http.StatusBadGateway, "upstream boom", nil), nil
		})

		_, body := h.get(testURL)
		require.Equal(t, "BODY-A", body)

		time.Sleep(61 * time.Second)

		// No stale fallback: the 5xx is passed through unchanged.
		status, _ := h.get(testURL)
		assert.Equal(t, http.StatusBadGateway, status, "5xx revalidation is passed through, not masked by stale")
		assert.Equal(t, 2, h.base.calls)
	})
}

func TestNoStoreNotCached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(_ *http.Request, _ int) (*http.Response, error) {
			return makeResp(http.StatusOK, "SECRET", map[string]string{
				"Cache-Control": "no-store",
			}), nil
		})

		_, body := h.get(testURL)
		assert.Equal(t, "SECRET", body)

		entries, err := os.ReadDir(h.root)
		require.NoError(t, err)
		assert.Empty(t, entries, "no-store response must not be written to disk")

		_, body = h.get(testURL)
		assert.Equal(t, "SECRET", body)
		assert.Equal(t, 2, h.base.calls, "no-store is re-fetched every time")
	})
}

func TestNonGETPassThrough(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(_ *http.Request, _ int) (*http.Response, error) {
			return makeResp(http.StatusOK, "POSTED", map[string]string{
				"Cache-Control": "max-age=60",
			}), nil
		})

		req, err := http.NewRequest(http.MethodPost, testURL, strings.NewReader("x"))
		require.NoError(t, err)
		resp, err := h.tr.RoundTrip(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())

		entries, err := os.ReadDir(h.root)
		require.NoError(t, err)
		assert.Empty(t, entries, "non-GET responses are not cached")
	})
}

func TestNon2xxNotCached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(_ *http.Request, _ int) (*http.Response, error) {
			return makeResp(http.StatusNotFound, "nope", nil), nil
		})

		status, _ := h.get(testURL)
		assert.Equal(t, http.StatusNotFound, status, "non-2xx passes through unchanged")

		entries, err := os.ReadDir(h.root)
		require.NoError(t, err)
		assert.Empty(t, entries, "404 must not be cached")
	})
}
