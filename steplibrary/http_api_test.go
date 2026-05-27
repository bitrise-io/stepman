package steplibrary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	cacheDir := t.TempDir()
	api := NewHTTPAPI(srv.URL, cacheDir, srv.Client(), discardLogger{})
	ctx := context.Background()

	t.Run("GetAllStepIDs", func(t *testing.T) {
		got, err := api.GetAllStepIDs(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"hello-step", "git-clone"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("GetAllStepIDs = %v, want %v", got, want)
		}
	})

	t.Run("GetLatestStepVersions", func(t *testing.T) {
		got, err := api.GetLatestStepVersions(ctx, "hello-step")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.StepID != "hello-step" || got.Latest != "2.0.0" {
			t.Errorf("StepID/Latest = %q/%q, want %q/%q", got.StepID, got.Latest, "hello-step", "2.0.0")
		}
		if got.LatestByMajor["2"] != "2.0.0" {
			t.Errorf("LatestByMajor[2] = %q, want %q", got.LatestByMajor["2"], "2.0.0")
		}
	})

	t.Run("GetAllStepVersions returns only version strings", func(t *testing.T) {
		got, err := api.GetAllStepVersions(ctx, "hello-step")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"2.0.0", "1.1.0", "1.0.0"}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
		}
		for i, v := range want {
			if got[i] != v {
				t.Errorf("[%d] = %q, want %q", i, got[i], v)
			}
		}
	})

	t.Run("GetStepGroupInfo", func(t *testing.T) {
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

	t.Run("GetStepModel decodes step.json into models.StepModel", func(t *testing.T) {
		got, err := api.GetStepModel(ctx, ResolvedStepVersion{ID: "hello-step", Version: "2.0.0"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Title == nil || *got.Title != "Hello" {
			t.Errorf("Title = %v, want %q", got.Title, "Hello")
		}
	})

	t.Run("GetStepSourceZIPPath downloads to cache dir", func(t *testing.T) {
		path, err := api.GetStepSourceZIPPath(ctx, ResolvedStepVersion{ID: "hello-step", Version: "2.0.0"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(cacheDir, "steps", "hello-step", "2.0.0", "src.zip")
		if path != want {
			t.Errorf("path = %q, want %q", path, want)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read downloaded file: %v", err)
		}
		if !strings.HasPrefix(string(body), "PK\x03\x04") {
			t.Errorf("downloaded body missing zip magic: %q", body)
		}
	})

	t.Run("404 surfaces unexpected status", func(t *testing.T) {
		_, err := api.GetLatestStepVersions(ctx, "missing-step")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "unexpected status 404") {
			t.Errorf("error = %q, want substring %q", err.Error(), "unexpected status 404")
		}
	})
}
