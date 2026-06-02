package steplibrary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPAPI_Integration serves docs/spec-v2/sample-output/ over an
// httptest.Server and exercises HTTPAPI against it. This validates that
// the generator output and the HTTP client agree on the V2 schema shape
// end-to-end, without mocking the JSON in the test itself.
func TestHTTPAPI_Integration(t *testing.T) {
	sampleDir := filepath.Join("..", "docs", "spec-v2", "sample-output")
	_, err := os.Stat(sampleDir)
	require.NoErrorf(t, err, "sample-output not found at %s", sampleDir)

	srv := httptest.NewServer(http.FileServer(http.Dir(sampleDir)))
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	api := NewHTTPAPI(srv.URL, cacheDir, srv.Client(), discardLogger{})
	ctx := context.Background()

	t.Run("GetAllStepIDs returns sample step IDs", func(t *testing.T) {
		got, gotErr := api.GetAllStepIDs(ctx)
		require.NoError(t, gotErr, "GetAllStepIDs")
		assert.Equal(t, []string{"bash-step", "deprecated-step", "hello-step", "multi-platform-step", "no-info-step"}, got, "step IDs")
	})

	t.Run("GetLatestStepVersions(hello-step)", func(t *testing.T) {
		got, gotErr := api.GetLatestStepVersions(ctx, "hello-step")
		require.NoError(t, gotErr, "GetLatestStepVersions")
		assert.Equal(t, "hello-step", got.StepID, "StepID")
		assert.Equal(t, "2.0.0", got.Latest, "Latest")
		assert.Equal(t, "1.1.0", got.LatestByMajor["1"], "LatestByMajor[1]")
		assert.Equal(t, "2.0.0", got.LatestByMajor["2"], "LatestByMajor[2]")
	})

	t.Run("GetAllStepVersions(hello-step) returns newest-first", func(t *testing.T) {
		got, gotErr := api.GetAllStepVersions(ctx, "hello-step")
		require.NoError(t, gotErr, "GetAllStepVersions")
		assert.Equal(t, []string{"2.0.0", "1.1.0", "1.0.0"}, got, "versions")
	})

	t.Run("GetStepGroupInfo(hello-step) shows active step", func(t *testing.T) {
		got, gotErr := api.GetStepGroupInfo(ctx, "hello-step")
		require.NoError(t, gotErr, "GetStepGroupInfo")
		assert.Equal(t, "bitrise", got.Maintainer, "Maintainer")
		assert.Nil(t, got.Deprecation, "Deprecation")
		assert.Equal(t, "assets/icon.svg", got.AssetURLs["icon.svg"], "AssetURLs[icon.svg]")
	})

	t.Run("GetStepGroupInfo(deprecated-step) exposes deprecation metadata", func(t *testing.T) {
		got, gotErr := api.GetStepGroupInfo(ctx, "deprecated-step")
		require.NoError(t, gotErr, "GetStepGroupInfo")
		assert.Equal(t, "community", got.Maintainer, "Maintainer")
		require.NotNil(t, got.Deprecation, "Deprecation")
		assert.Equal(t, "2026-12-31", got.Deprecation.RemovalDate, "RemovalDate")
		assert.Contains(t, got.Deprecation.Notes, "Replaced by hello-step", "Notes")
	})

	t.Run("GetStepModel(hello-step, 2.0.0) decodes step.json", func(t *testing.T) {
		got, gotErr := api.GetStepModel(ctx, ResolvedStepVersion{ID: "hello-step", Version: "2.0.0"})
		require.NoError(t, gotErr, "GetStepModel")
		require.NotNil(t, got.Title, "Title")
		assert.Equal(t, "Hello Step", *got.Title, "Title")
		require.NotNil(t, got.SourceCodeURL, "SourceCodeURL")
		assert.Equal(t, "https://github.com/example/hello-step", *got.SourceCodeURL, "SourceCodeURL")
		require.NotNil(t, got.Source, "Source")
		assert.Equal(t, "https://github.com/example/hello-step.git", got.Source.Git, "Source.Git")
	})
}
