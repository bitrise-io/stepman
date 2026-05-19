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
	api := NewHTTPAPI(srv.URL, cacheDir, srv.Client())
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

	t.Run("GetStepYMLPath(hello-step, 2.0.0) downloads step.json", func(t *testing.T) {
		path, err := api.GetStepYMLPath(ctx, ResolvedStepVersion{ID: "hello-step", Version: "2.0.0"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(cacheDir, "steps", "hello-step", "2.0.0", "step.json")
		if path != want {
			t.Errorf("path = %q, want %q", path, want)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read downloaded file: %v", err)
		}
		// step.json mirrors models.StepModel — id/version are implicit from the
		// file path, not serialised as fields. Verify against stable content.
		if !strings.Contains(string(body), `"title": "Hello Step"`) {
			t.Errorf("downloaded body missing title field: %q", body[:min(200, len(body))])
		}
		if !strings.Contains(string(body), `"source_code_url": "https://github.com/example/hello-step"`) {
			t.Errorf("downloaded body missing source_code_url field: %q", body[:min(300, len(body))])
		}
	})

	t.Run("GetStepSourceZIPPath errors when sample lacks src.zip", func(t *testing.T) {
		// The sample-output tree intentionally omits src.zip blobs.
		_, err := api.GetStepSourceZIPPath(ctx, ResolvedStepVersion{ID: "hello-step", Version: "2.0.0"})
		if err == nil {
			t.Fatal("expected error for missing src.zip, got nil")
		}
		// filedownloader wraps non-2xx as "status code 404" in its error chain.
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("error = %q, want substring %q", err.Error(), "404")
		}
	})
}
