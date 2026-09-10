package httpfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bartventer/httpcache"
	"github.com/stretchr/testify/require"
)

// inventoryServer serves body under the given Cache-Control with a fixed ETag,
// counting how many requests actually reach it and answering 304 to a matching
// If-None-Match. It stands in for the StepLib V2 API, whose real headers are
// max-age=60/300 with must-revalidate on the index files and immutable on
// step.json.
type inventoryServer struct {
	hits         atomic.Int32
	conditionals atomic.Int32
	cacheControl string
	age          string // optional Age header, as the CDN sends for edge-resident objects
	etag         string
	body         string
}

func (s *inventoryServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		w.Header().Set("Cache-Control", s.cacheControl)
		if s.age != "" {
			w.Header().Set("Age", s.age)
		}
		w.Header().Set("ETag", s.etag)
		if r.Header.Get("If-None-Match") == s.etag {
			s.conditionals.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if _, err := fmt.Fprint(w, s.body); err != nil {
			t.Errorf("write test response: %s", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// cachingClientFor builds the Inventory client but pointed at a test server,
// mirroring NewClients' wiring: the cache sits above the server's transport.
func cachingClientFor(t *testing.T, srv *httptest.Server) Client {
	t.Helper()
	rt, err := newCachingTransport(testLogger{t}, srv.Client().Transport)
	require.NoError(t, err)
	return NewWithClient(&http.Client{Transport: rt})
}

// getStatus reads the body and the cache's own verdict on the request, so a
// test can assert HIT/MISS directly rather than inferring it from how many
// requests reached the origin.
func getStatus(t *testing.T, c Client, url string) (string, string) {
	t.Helper()
	rt := c.(*client).httpClient.Transport
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b), resp.Header.Get(httpcache.CacheStatusHeader)
}

func getBody(t *testing.T, c Client, url string) string {
	t.Helper()
	body, err := c.Get(context.Background(), url)
	require.NoError(t, err)
	defer func() { require.NoError(t, body.Close()) }()
	b, err := io.ReadAll(body)
	require.NoError(t, err)
	return string(b)
}

// A fresh entry is served without touching the origin at all: this is the case
// that removes most of a run's inventory requests.
func TestCacheServesFreshWithoutOrigin(t *testing.T) {
	origin := &inventoryServer{cacheControl: "public, max-age=60, must-revalidate", etag: `"v1"`, body: `{"step_ids":["script"]}`}
	srv := origin.start(t)
	c := cachingClientFor(t, srv)

	body, status := getStatus(t, c, srv.URL)
	require.Equal(t, `{"step_ids":["script"]}`, body)
	require.Equal(t, "MISS", status, "the first read has nothing to serve")

	for range 4 {
		body, status = getStatus(t, c, srv.URL)
		require.Equal(t, `{"step_ids":["script"]}`, body)
		require.Equal(t, "HIT", status)
	}
	require.EqualValues(t, 1, origin.hits.Load(), "five reads of a fresh object must hit the origin once")
	require.EqualValues(t, 0, origin.conditionals.Load(), "a fresh entry must not be revalidated")
}

// An expired entry revalidates with If-None-Match and takes the 304, which the
// transport turns back into a usable 200 - httpfetch.Get rejects any non-2xx,
// so a 304 reaching it would surface as a StatusError.
func TestCacheRevalidatesWithETagAnd304(t *testing.T) {
	origin := &inventoryServer{cacheControl: "public, max-age=0, must-revalidate", etag: `"v1"`, body: `{"step_ids":["script"]}`}
	srv := origin.start(t)
	c := cachingClientFor(t, srv)

	require.Equal(t, `{"step_ids":["script"]}`, getBody(t, c, srv.URL))
	// Immediately stale (max-age=0), so the next read must revalidate and
	// still return the body from cache rather than erroring on the 304.
	body, status := getStatus(t, c, srv.URL)
	require.Equal(t, `{"step_ids":["script"]}`, body)
	require.Equal(t, "REVALIDATED", status)

	require.EqualValues(t, 1, origin.conditionals.Load(), "the second read must send If-None-Match")
	require.EqualValues(t, 2, origin.hits.Load())
}

// An immutable response that is still fresh is served without revalidating.
func TestCacheNeverRevalidatesFreshImmutable(t *testing.T) {
	origin := &inventoryServer{cacheControl: "public, max-age=86400, immutable", etag: `"v1"`, body: `{"title":"Script"}`}
	srv := origin.start(t)
	c := cachingClientFor(t, srv)

	for range 3 {
		require.Equal(t, `{"title":"Script"}`, getBody(t, c, srv.URL))
	}
	require.EqualValues(t, 1, origin.hits.Load())
	require.EqualValues(t, 0, origin.conditionals.Load())
}

// ...but that is not what production looks like. step.json is published
// "max-age=86400, s-maxage=31536000, immutable", and the CDN holds it at the
// edge for up to a year, so it arrives with an Age of several days - already
// stale, and immutable cannot apply to a response that was never fresh. Pin
// the behaviour we actually get, so this test does not quietly claim a win we
// do not have. See STEP-2527.
func TestImmutableIsStaleOnArrivalWithLargeAge(t *testing.T) {
	origin := &inventoryServer{
		cacheControl: "public, max-age=86400, s-maxage=31536000, immutable",
		age:          "342124", // ~4 days, measured on v2/steps/script/1.2.1/step.json
		etag:         `"v1"`,
		body:         `{"title":"Script"}`,
	}
	srv := origin.start(t)
	c := cachingClientFor(t, srv)

	require.Equal(t, `{"title":"Script"}`, getBody(t, c, srv.URL))
	body, status := getStatus(t, c, srv.URL)
	require.Equal(t, `{"title":"Script"}`, body, "the body is still correct")
	require.Equal(t, "REVALIDATED", status,
		"Age exceeds max-age, so every read revalidates - immutable never takes effect")
	require.EqualValues(t, 1, origin.conditionals.Load())
}

// Distinct URLs are distinct entries - a cache that collapsed them would serve
// one step's metadata for another.
func TestCacheKeyedPerURL(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=60")
		if _, err := fmt.Fprintf(w, `{"path":%q}`, r.URL.Path); err != nil {
			t.Errorf("write test response: %s", err)
		}
	}))
	t.Cleanup(srv.Close)
	c := cachingClientFor(t, srv)

	require.Equal(t, `{"path":"/a.json"}`, getBody(t, c, srv.URL+"/a.json"))
	require.Equal(t, `{"path":"/b.json"}`, getBody(t, c, srv.URL+"/b.json"))
	require.Equal(t, `{"path":"/a.json"}`, getBody(t, c, srv.URL+"/a.json"))
	require.EqualValues(t, 2, hits.Load(), "two distinct URLs, and the repeat served from cache")
}

// The two clients NewClients actually returns must differ: the inventory one
// caches, the downloads one does not. Exercising the real pair matters here -
// asserting against a separately built client would pass even if NewClients
// wired the cache to the wrong half.
func TestNewClientsCachesInventoryOnly(t *testing.T) {
	clients, err := NewClients(testLogger{t})
	require.NoError(t, err)

	t.Run("inventory caches", func(t *testing.T) {
		origin := &inventoryServer{cacheControl: "public, max-age=60, must-revalidate", etag: `"v1"`, body: `{"step_ids":["script"]}`}
		srv := origin.start(t)
		for range 3 {
			require.Equal(t, `{"step_ids":["script"]}`, getBody(t, clients.Inventory, srv.URL))
		}
		require.EqualValues(t, 1, origin.hits.Load(), "the inventory client must serve repeats from cache")
	})

	t.Run("downloads do not cache", func(t *testing.T) {
		// Same headers the inventory server sends, so the only thing that can
		// account for a difference is which client is used.
		origin := &inventoryServer{cacheControl: "public, max-age=60, must-revalidate", etag: `"v1"`, body: "binary-ish"}
		srv := origin.start(t)
		for range 3 {
			require.Equal(t, "binary-ish", getBody(t, clients.Downloads, srv.URL))
		}
		require.EqualValues(t, 3, origin.hits.Load(), "step archives and executables must never be held in memory")
	})
}

type testLogger struct{ t *testing.T }

func (l testLogger) Debugf(format string, v ...any) { l.t.Logf(format, v...) }

// A failing origin must not be cached. retryablehttp is configured with
// PassthroughErrorHandler, so a 5xx that outlives the retries arrives as a
// normal response; if the origin's error page carries a freshness directive,
// an unguarded cache would store it and replay it for the whole run.
func TestCacheDoesNotStoreErrorResponses(t *testing.T) {
	var hits atomic.Int32
	var failing atomic.Bool
	failing.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// The same freshness the real inventory paths advertise.
		w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
		if failing.Load() {
			w.WriteHeader(http.StatusBadGateway)
			if _, err := fmt.Fprint(w, "<html>502</html>"); err != nil {
				t.Errorf("write test response: %s", err)
			}
			return
		}
		if _, err := fmt.Fprint(w, `{"step_ids":["script"]}`); err != nil {
			t.Errorf("write test response: %s", err)
		}
	}))
	t.Cleanup(srv.Close)
	c := cachingClientFor(t, srv)

	// Two failing reads: the second must reach the origin, not be served the
	// stored 502.
	for range 2 {
		_, err := c.Get(context.Background(), srv.URL)
		require.Error(t, err)
	}
	require.EqualValues(t, 2, hits.Load(), "a 502 must not be cached and replayed")

	// Once the origin recovers, the very next read must succeed - a pinned
	// error would keep failing here for the rest of its freshness window.
	failing.Store(false)
	require.Equal(t, `{"step_ids":["script"]}`, getBody(t, c, srv.URL))
	require.EqualValues(t, 3, hits.Load())
}

// A 404 is a real answer from the inventory (an unknown step or version) but
// still must not be stored, for the same reason.
func TestCacheDoesNotStoreNotFound(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := cachingClientFor(t, srv)

	for range 2 {
		_, err := c.Get(context.Background(), srv.URL)
		require.Error(t, err)
	}
	require.EqualValues(t, 2, hits.Load(), "a 404 must not be cached and replayed")
}
