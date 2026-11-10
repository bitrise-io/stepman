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
func stallingServer(t *testing.T, requests *atomic.Int32, prelude func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		prelude(w)
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
	return stallingServer(t, requests, func(http.ResponseWriter) {})
}

// flushPartialBody sends the headers and the start of a body, then stalls.
func flushPartialBody(w http.ResponseWriter) {
	_, _ = w.Write([]byte("partial"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
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

	start := time.Now()
	_, err := c.Get(context.Background(), srv.URL)
	elapsed := time.Since(start)

	require.Error(t, err)
	var netErr net.Error
	require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "expected a timeout error, got %v", err)
	// The header timeout error also satisfies errors.Is(err, context.DeadlineExceeded),
	// so tell the two bounds apart by their messages.
	require.Contains(t, err.Error(), "timeout awaiting response headers")
	require.NotContains(t, err.Error(), "gave up after", "the header timeout, not the operation deadline, should have fired")
	require.EqualValues(t, 1+retryMax, requests.Load(), "a header timeout is retried like any other network error")
	require.Less(t, elapsed, time.Second)
}

func TestOperationTimeoutFailsASilentServerBeforeTheHeaderTimeout(t *testing.T) {
	var requests atomic.Int32
	srv := silentServer(t, &requests)
	c := newTestClient(t, DefaultResponseHeaderTimeout, 200*time.Millisecond)

	start := time.Now()
	_, err := c.Get(context.Background(), srv.URL)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "gave up after 200ms")
	require.EqualValues(t, 1, requests.Load())
	require.Less(t, elapsed, time.Second)
}

func TestOperationTimeoutCutsTheRetryBackoffShort(t *testing.T) {
	// Every attempt gets a 500, which is retried after a 1s backoff.
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newTestClient(t, DefaultResponseHeaderTimeout, 200*time.Millisecond)

	start := time.Now()
	_, err := c.Get(context.Background(), srv.URL)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.EqualValues(t, 1, requests.Load(), "the deadline should fire during the first backoff")
	require.Less(t, elapsed, time.Second, "the 1s backoff should have been cut short")
}

func TestDownloadTimesOutOnABodyThatStallsAfterTheHeaders(t *testing.T) {
	var requests atomic.Int32
	srv := stallingServer(t, &requests, flushPartialBody)
	c := newTestClient(t, DefaultResponseHeaderTimeout, 200*time.Millisecond)
	destPath := filepath.Join(t.TempDir(), "out.bin")

	err := c.Download(context.Background(), destPath, srv.URL)

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

	body, err := c.Get(context.Background(), srv.URL)
	require.NoError(t, err)
	defer func() { _ = body.Close() }()

	got, err := io.ReadAll(body)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "gave up after 200ms")
	require.Equal(t, "partial", string(got))
}

func TestCallerDeadlineIsNotReportedAsTheOperationTimeout(t *testing.T) {
	var requests atomic.Int32
	srv := silentServer(t, &requests)
	c := newTestClient(t, DefaultResponseHeaderTimeout, DefaultInventoryTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
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
