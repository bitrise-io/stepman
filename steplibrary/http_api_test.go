package steplibrary

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bitrise-io/stepman/internal/httpfetch"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPAPI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/spec/step_ids.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"format_version":"2.0.0","step_ids":["hello-step","git-clone"]}`))
	})
	mux.HandleFunc("/spec/steps/hello-step/latest.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"step_id":"hello-step","latest":"2.0.0","latest_by_major":{"1":"1.1.0","2":"2.0.0"}}`))
	})
	mux.HandleFunc("/spec/steps/hello-step/versions.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"step_id":"hello-step","latest":"2.0.0","versions":[{"version":"2.0.0"},{"version":"1.1.0"},{"version":"1.0.0"}]}`))
	})
	mux.HandleFunc("/steps/hello-step/step-info.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"maintainer":"bitrise","deprecation":null,"asset_urls":{"icon.svg":"assets/icon.svg"}}`))
	})
	mux.HandleFunc("/steps/hello-step/2.0.0/step.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"title":"Hello","summary":"says hi"}`))
	})
	mux.HandleFunc("/steps/hello-step/2.0.0/src.zip", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("PK\x03\x04seed-zip-bytes"))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	api := NewHTTPAPI(srv.URL, httpfetch.NewWithClient(srv.Client()))
	ctx := context.Background()

	t.Run("GetAllStepIDs", func(t *testing.T) {
		got, gotErr := api.GetAllStepIDs(ctx)
		require.NoError(t, gotErr, "GetAllStepIDs")
		assert.Equal(t, []string{"hello-step", "git-clone"}, got, "step IDs")
	})

	t.Run("GetLatestStepVersions", func(t *testing.T) {
		got, gotErr := api.GetLatestStepVersions(ctx, "hello-step")
		require.NoError(t, gotErr, "GetLatestStepVersions")
		assert.Equal(t, "hello-step", got.StepID, "StepID")
		assert.Equal(t, "2.0.0", got.Latest, "Latest")
		assert.Equal(t, "2.0.0", got.LatestByMajor["2"], "LatestByMajor[2]")
	})

	t.Run("GetAllStepVersions returns only version strings", func(t *testing.T) {
		got, gotErr := api.GetAllStepVersions(ctx, "hello-step")
		require.NoError(t, gotErr, "GetAllStepVersions")
		assert.Equal(t, []string{"2.0.0", "1.1.0", "1.0.0"}, got, "versions")
	})

	t.Run("GetStepGroupInfo", func(t *testing.T) {
		got, gotErr := api.GetStepGroupInfo(ctx, "hello-step")
		require.NoError(t, gotErr, "GetStepGroupInfo")
		assert.Equal(t, "bitrise", got.Maintainer, "Maintainer")
		assert.Nil(t, got.Deprecation, "Deprecation")
		assert.Equal(t, "assets/icon.svg", got.AssetURLs["icon.svg"], "AssetURLs[icon.svg]")
	})

	t.Run("GetStepModel decodes step.json into models.StepModel", func(t *testing.T) {
		got, gotErr := api.GetStepModel(ctx, ResolvedStepVersion{ID: "hello-step", Version: "2.0.0"})
		require.NoError(t, gotErr, "GetStepModel")
		require.NotNil(t, got.Title, "Title")
		assert.Equal(t, "Hello", *got.Title, "Title")
	})

	t.Run("404 surfaces unexpected status", func(t *testing.T) {
		_, gotErr := api.GetLatestStepVersions(ctx, "missing-step")
		require.Error(t, gotErr, "GetLatestStepVersions for missing step")
		assert.Contains(t, gotErr.Error(), "unexpected status 404", "error message")
	})
}

// TestHTTPAPI_fetchJSON_propagatesCloseError verifies the named-return + defer
// pattern in fetchJSON: when decoding succeeds but closing the body fails, the
// close error must surface as the call's error.
func TestHTTPAPI_fetchJSON_propagatesCloseError(t *testing.T) {
	api := &HTTPAPI{
		BaseURL: "http://example.test",
		Fetcher: fakeGetFetcher{body: `{"step_ids":["a","b"]}`, closeErr: errors.New("boom")},
	}

	_, gotErr := api.GetAllStepIDs(context.Background())
	require.Error(t, gotErr, "GetAllStepIDs")
	assert.Contains(t, gotErr.Error(), "close response body", "error wraps close failure")
	assert.Contains(t, gotErr.Error(), "boom", "error preserves underlying cause")
}
