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
	etag         string
	body         string
}

func (s *inventoryServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		w.Header().Set("Cache-Control", s.cacheControl)
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
func cachingClientFor(t *testing.T, srv *httptest.Server) (Client, func(*http.Response) string) {
	t.Helper()
	rt, err := newCachingTransport(srv.Client().Transport)
	require.NoError(t, err)
	status := func(resp *http.Response) string { return resp.Header.Get(httpcache.CacheStatusHeader) }
	return NewWithClient(&http.Client{Transport: rt}), status
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
	c, _ := cachingClientFor(t, srv)

	for range 5 {
		require.Equal(t, `{"step_ids":["script"]}`, getBody(t, c, srv.URL))
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
	c, _ := cachingClientFor(t, srv)

	require.Equal(t, `{"step_ids":["script"]}`, getBody(t, c, srv.URL))
	// Immediately stale (max-age=0), so the next read must revalidate and
	// still return the body from cache rather than erroring on the 304.
	require.Equal(t, `{"step_ids":["script"]}`, getBody(t, c, srv.URL))

	require.EqualValues(t, 1, origin.conditionals.Load(), "the second read must send If-None-Match")
	require.EqualValues(t, 2, origin.hits.Load())
}

// step.json is published immutable, so it must never be revalidated.
func TestCacheNeverRevalidatesImmutable(t *testing.T) {
	origin := &inventoryServer{cacheControl: "public, max-age=86400, immutable", etag: `"v1"`, body: `{"title":"Script"}`}
	srv := origin.start(t)
	c, _ := cachingClientFor(t, srv)

	for range 3 {
		require.Equal(t, `{"title":"Script"}`, getBody(t, c, srv.URL))
	}
	require.EqualValues(t, 1, origin.hits.Load())
	require.EqualValues(t, 0, origin.conditionals.Load())
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
	c, _ := cachingClientFor(t, srv)

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
