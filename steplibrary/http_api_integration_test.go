package steplibrary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestHTTPAPI_Integration serves docs/spec-v2/sample-output/ over an
// httptest.Server and exercises HTTPAPI against it. This validates that
// the generator output and the HTTP client agree on the V2 schema shape
// end-to-end, without mocking the JSON in the test itself.
func TestHTTPAPI_Integration(t *testing.T) {
	sampleDir := filepath.Join("..", "docs", "spec-v2", "sample-output")
	if _, err := os.Stat(sampleDir); err != nil {
		t.Fatalf("sample-output not found at %s: %v", sampleDir, err)
	}

	srv := httptest.NewServer(http.FileServer(http.Dir(sampleDir)))
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	api := NewHTTPAPI(srv.URL, cacheDir, srv.Client(), discardLogger{})
	ctx := context.Background()

	t.Run("GetAllStepIDs returns sample step IDs", func(t *testing.T) {
		got, err := api.GetAllStepIDs(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"bash-step", "deprecated-step", "hello-step", "multi-platform-step", "no-info-step"}
		if !slices.Equal(got, want) {
			t.Errorf("step IDs = %v, want %v", got, want)
		}
	})

	t.Run("GetLatestStepVersions(hello-step)", func(t *testing.T) {
		got, err := api.GetLatestStepVersions(ctx, "hello-step")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.StepID != "hello-step" {
			t.Errorf("StepID = %q, want %q", got.StepID, "hello-step")
		}
		if got.Latest != "2.0.0" {
			t.Errorf("Latest = %q, want %q", got.Latest, "2.0.0")
		}
		if got.LatestByMajor["1"] != "1.1.0" || got.LatestByMajor["2"] != "2.0.0" {
			t.Errorf("LatestByMajor = %v, want {1:1.1.0, 2:2.0.0}", got.LatestByMajor)
		}
	})

	t.Run("GetAllStepVersions(hello-step) returns newest-first", func(t *testing.T) {
		got, err := api.GetAllStepVersions(ctx, "hello-step")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"2.0.0", "1.1.0", "1.0.0"}
		if !slices.Equal(got, want) {
			t.Errorf("versions = %v, want %v", got, want)
		}
	})

	t.Run("GetStepGroupInfo(hello-step) shows active step", func(t *testing.T) {
		got, err := api.GetStepGroupInfo(ctx, "hello-step")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Maintainer != "bitrise" {
			t.Errorf("Maintainer = %q, want %q", got.Maintainer, "bitrise")
		}
		if got.Deprecation != nil {
			t.Errorf("Deprecation = %+v, want nil", got.Deprecation)
		}
		if got.AssetURLs["icon.svg"] != "assets/icon.svg" {
			t.Errorf("AssetURLs[icon.svg] = %q, want %q", got.AssetURLs["icon.svg"], "assets/icon.svg")
		}
	})

	t.Run("GetStepGroupInfo(deprecated-step) exposes deprecation metadata", func(t *testing.T) {
		got, err := api.GetStepGroupInfo(ctx, "deprecated-step")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Maintainer != "community" {
			t.Errorf("Maintainer = %q, want %q", got.Maintainer, "community")
		}
		if got.Deprecation == nil {
			t.Fatalf("Deprecation = nil, want populated")
		}
		if got.Deprecation.RemovalDate != "2026-12-31" {
			t.Errorf("RemovalDate = %q, want %q", got.Deprecation.RemovalDate, "2026-12-31")
		}
		if !strings.Contains(got.Deprecation.Notes, "Replaced by hello-step") {
			t.Errorf("Notes = %q, want substring %q", got.Deprecation.Notes, "Replaced by hello-step")
		}
	})

	t.Run("GetStepModel(hello-step, 2.0.0) decodes step.json", func(t *testing.T) {
		got, err := api.GetStepModel(ctx, ResolvedStepVersion{ID: "hello-step", Version: "2.0.0"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Title == nil || *got.Title != "Hello Step" {
			t.Errorf("Title = %v, want %q", got.Title, "Hello Step")
		}
		if got.SourceCodeURL == nil || *got.SourceCodeURL != "https://github.com/example/hello-step" {
			t.Errorf("SourceCodeURL = %v, want %q", got.SourceCodeURL, "https://github.com/example/hello-step")
		}
		if got.Source == nil || got.Source.Git != "https://github.com/example/hello-step.git" {
			t.Errorf("Source.Git = %+v, want %q", got.Source, "https://github.com/example/hello-step.git")
		}
	})

	t.Run("GetStepSourceZIPPath errors when sample lacks src.zip", func(t *testing.T) {
		// The sample-output tree intentionally omits src.zip blobs.
		_, err := api.GetStepSourceZIPPath(ctx, ResolvedStepVersion{ID: "hello-step", Version: "2.0.0"})
		if err == nil {
			t.Fatal("expected error for missing src.zip, got nil")
		}
		// httpfetch surfaces non-2xx as "unexpected status 404" in its error chain.
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("error = %q, want substring %q", err.Error(), "404")
		}
	})
}
