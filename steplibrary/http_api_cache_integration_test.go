package steplibrary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bitrise-io/stepman/internal/httpfetch"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nopLogger struct{}

func (nopLogger) Debugf(string, ...any) {}
func (nopLogger) Warnf(string, ...any)  {}

// TestHTTPAPI_cachingClient_reusesFreshEntry verifies the end-to-end wiring:
// an HTTPAPI backed by httpfetch.NewCachingClient serves a second identical
// request from disk while the entry is fresh, so the origin is hit only once.
func TestHTTPAPI_cachingClient_reusesFreshEntry(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/spec/steps/hello-step/latest.json", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
		_, err := w.Write([]byte(`{"step_id":"hello-step","latest":"2.0.0","latest_by_major":{"2":"2.0.0"}}`))
		require.NoError(t, err)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	api := NewHTTPAPI(srv.URL, httpfetch.NewCachingClient(nopLogger{}, t.TempDir()))
	ctx := context.Background()

	first, err := api.GetLatestStepVersions(ctx, "hello-step")
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", first.Latest)

	second, err := api.GetLatestStepVersions(ctx, "hello-step")
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", second.Latest)

	assert.Equal(t, int32(1), hits.Load(), "fresh cache entry must be reused without re-hitting the origin")
}
