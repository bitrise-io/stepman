package httpfetch

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/require"
)

// The deadlines in these tests are milliseconds so a stall is detected
// quickly; the production values only change the arithmetic. The servers use
// real sockets, which rules out testing/synctest.

// stallingServer runs prelude on every request, then never finishes the
// response: the handler blocks until the client goes away or the test ends.
func stallingServer(t *testing.T, requests *atomic.Int32, prelude func(t *testing.T, w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		prelude(t, w)
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	// Close waits for the handlers to return, so unblock them first: cleanups
	// run in reverse registration order.
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(done) })
	return srv
}

// silentServer accepts every request and never sends a byte back.
func silentServer(t *testing.T, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	return stallingServer(t, requests, func(*testing.T, http.ResponseWriter) {})
}

// flushPartialBody sends the headers and the start of a body, then stalls.
func flushPartialBody(t *testing.T, w http.ResponseWriter) {
	// t.Errorf, not require: this runs on the server's goroutine.
	if _, err := w.Write([]byte("partial")); err != nil {
		t.Errorf("write partial body: %s", err)
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// requireRequests waits for the server to have counted want requests. The
// client can give up on a request before the server's handler has run, so the
// count can lag behind the client returning.
func requireRequests(t *testing.T, requests *atomic.Int32, want int32) {
	t.Helper()
	require.Eventually(t, func() bool { return requests.Load() == want }, 5*time.Second, 5*time.Millisecond,
		"expected %d requests, got %d", want, requests.Load())
}

// newTestClient is the production client stack with the given bounds.
func newTestClient(t *testing.T, responseHeaderTimeout, operationTimeout time.Duration) *client {
	t.Helper()
	return newClient(newRetryingClient(testLogger{t}, newTransport(responseHeaderTimeout)), operationTimeout)
}

// withFastBackoff shrinks the retry backoff to a millisecond, so a test that
// exhausts every retry finishes quickly.
func withFastBackoff(t *testing.T, c *client) *client {
	t.Helper()
	rt, ok := c.httpClient.Transport.(*retryablehttp.RoundTripper)
	require.True(t, ok, "expected a retryablehttp transport, got %T", c.httpClient.Transport)
	rt.Client.RetryWaitMin = time.Millisecond
	rt.Client.RetryWaitMax = time.Millisecond
	return c
}

func TestResponseHeaderTimeoutFailsEachAttemptAgainstASilentServer(t *testing.T) {
	var requests atomic.Int32
	srv := silentServer(t, &requests)
	c := withFastBackoff(t, newTestClient(t, 20*time.Millisecond, 5*time.Second))

	_, err := c.Get(t.Context(), srv.URL)

	require.Error(t, err)
	var netErr net.Error
	require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "expected a timeout error, got %v", err)
	// The header timeout error also satisfies errors.Is(err, context.DeadlineExceeded),
	// so tell the two bounds apart by their messages.
	require.Contains(t, err.Error(), "timeout awaiting response headers")
	require.NotContains(t, err.Error(), "gave up after", "the header timeout, not the operation deadline, should have fired")
	// A header timeout is retried like any other network error.
	requireRequests(t, &requests, 1+retryMax)
}

func TestOperationTimeoutFailsASilentServerBeforeTheHeaderTimeout(t *testing.T) {
	var requests atomic.Int32
	srv := silentServer(t, &requests)
	c := newTestClient(t, DefaultResponseHeaderTimeout, 200*time.Millisecond)

	start := time.Now()
	_, err := c.Get(t.Context(), srv.URL)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "gave up after 200ms")
	requireRequests(t, &requests, 1)
	require.Less(t, elapsed, DefaultResponseHeaderTimeout/2, "the 30s header timeout should not have been waited out")
}

func TestOperationTimeoutCutsTheRetryBackoffShort(t *testing.T) {
	// Every attempt gets a 500, which is retried after a backoff of at least retryWaitMin.
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newTestClient(t, DefaultResponseHeaderTimeout, 200*time.Millisecond)

	start := time.Now()
	_, err := c.Get(t.Context(), srv.URL)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.EqualValues(t, 1, requests.Load(), "the deadline should fire during the first backoff")
	require.Less(t, elapsed, retryWaitMin, "the backoff should have been cut short")
}

func TestDownloadTimesOutOnABodyThatStallsAfterTheHeaders(t *testing.T) {
	var requests atomic.Int32
	srv := stallingServer(t, &requests, flushPartialBody)
	c := newTestClient(t, DefaultResponseHeaderTimeout, 200*time.Millisecond)
	destPath := filepath.Join(t.TempDir(), "out.bin")

	err := c.Download(t.Context(), destPath, srv.URL)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "gave up after 200ms")
	_, statErr := os.Stat(destPath)
	require.True(t, os.IsNotExist(statErr), "no partial file may appear at destPath")
	entries, err := os.ReadDir(filepath.Dir(destPath))
	require.NoError(t, err)
	require.Empty(t, entries, "the temp file should have been cleaned up")
}

func TestGetBodyReadReportsTheOperationTimeout(t *testing.T) {
	var requests atomic.Int32
	srv := stallingServer(t, &requests, flushPartialBody)
	c := newTestClient(t, DefaultResponseHeaderTimeout, 200*time.Millisecond)

	body, err := c.Get(t.Context(), srv.URL)
	require.NoError(t, err)
	defer func() { require.NoError(t, body.Close()) }()

	got, err := io.ReadAll(body)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "gave up after 200ms")
	require.Equal(t, "partial", string(got))
}

func TestCallerDeadlineIsNotReportedAsTheOperationTimeout(t *testing.T) {
	var requests atomic.Int32
	srv := silentServer(t, &requests)
	c := newTestClient(t, DefaultResponseHeaderTimeout, DefaultInventoryTimeout)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, err := c.Get(ctx, srv.URL)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotContains(t, err.Error(), "gave up after")
}

func TestConstructorsWireTheDocumentedBounds(t *testing.T) {
	clients, err := NewCachingClient(testLogger{t})
	require.NoError(t, err)
	require.Equal(t, DefaultInventoryTimeout, clients.Caching.(*client).timeout)
	require.Equal(t, DefaultDownloadTimeout, clients.Passthrough.(*client).timeout)

	uncached := NewUncachedClients(testLogger{t})
	require.Equal(t, DefaultInventoryTimeout, uncached.Caching.(*client).timeout)
	require.Equal(t, DefaultDownloadTimeout, uncached.Passthrough.(*client).timeout)

	plain := NewClient(testLogger{t}).(*client)
	require.Equal(t, DefaultDownloadTimeout, plain.timeout)

	rt, ok := plain.httpClient.Transport.(*retryablehttp.RoundTripper)
	require.True(t, ok, "expected a retryablehttp transport, got %T", plain.httpClient.Transport)
	require.Equal(t, retryMax, rt.Client.RetryMax)
	require.Equal(t, retryWaitMin, rt.Client.RetryWaitMin)
	transport, ok := rt.Client.HTTPClient.Transport.(*http.Transport)
	require.True(t, ok, "expected an *http.Transport, got %T", rt.Client.HTTPClient.Transport)
	require.Equal(t, DefaultResponseHeaderTimeout, transport.ResponseHeaderTimeout)
}

func TestRetryAfterIsCappedAtRetryWaitMax(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := withFastBackoff(t, newTestClient(t, DefaultResponseHeaderTimeout, 5*time.Second))

	body, err := c.Get(t.Context(), srv.URL)

	require.NoError(t, err, "the 1h Retry-After should have been capped, not waited out to the deadline")
	require.NoError(t, body.Close())
	require.EqualValues(t, 2, requests.Load())
}

func TestStalledErrorBodyReportsTheOperationTimeout(t *testing.T) {
	// A 404 is not retried, so its body is read by Get itself for the error.
	var requests atomic.Int32
	srv := stallingServer(t, &requests, func(t *testing.T, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
		flushPartialBody(t, w)
	})
	c := newTestClient(t, DefaultResponseHeaderTimeout, 200*time.Millisecond)

	_, err := c.Get(t.Context(), srv.URL)

	var statusErr *StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusNotFound, statusErr.Code)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "gave up after 200ms")
}
