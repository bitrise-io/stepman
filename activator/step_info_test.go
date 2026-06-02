package activator

import (
	"testing"

	"github.com/bitrise-io/go-utils/pointers"
	"github.com/bitrise-io/stepman/models"
)

func TestPopulateStepInfo(t *testing.T) {
	source := models.StepInfoModel{
		ID:              "script",
		Version:         "1.2.3",
		LatestVersion:   "1.2.4",
		OriginalVersion: "1.2.x",
		GroupInfo:       models.StepGroupInfoModel{Maintainer: "verified"},
		//nolint:exhaustruct
		Step: models.StepModel{Title: pointers.NewStringPtr("Source Title")},
	}

	t.Run("nil title is filled from source ID", func(t *testing.T) {
		//nolint:exhaustruct
		target := models.StepInfoModel{
			Step: models.StepModel{Title: nil},
		}
		populateStepInfo(&target, source)

		if target.Step.Title == nil {
			t.Fatalf("Title was not set")
		}
		if *target.Step.Title != "script" {
			t.Errorf("Title = %q, want %q (from source.ID)", *target.Step.Title, "script")
		}
		assertSourceFieldsCopied(t, target, source)
	})

	t.Run("empty title is replaced with source ID", func(t *testing.T) {
		//nolint:exhaustruct
		target := models.StepInfoModel{
			Step: models.StepModel{Title: pointers.NewStringPtr("")},
		}
		populateStepInfo(&target, source)

		if target.Step.Title == nil || *target.Step.Title != "script" {
			t.Errorf("Title = %v, want %q", target.Step.Title, "script")
		}
		assertSourceFieldsCopied(t, target, source)
	})

	t.Run("existing non-empty title is preserved", func(t *testing.T) {
		//nolint:exhaustruct
		target := models.StepInfoModel{
			Step: models.StepModel{Title: pointers.NewStringPtr("Caller Title")},
		}
		populateStepInfo(&target, source)

		if target.Step.Title == nil || *target.Step.Title != "Caller Title" {
			t.Errorf("Title = %v, want %q", target.Step.Title, "Caller Title")
		}
		assertSourceFieldsCopied(t, target, source)
	})

	t.Run("scalar fields are overwritten even when already set", func(t *testing.T) {
		//nolint:exhaustruct
		target := models.StepInfoModel{
			ID:              "stale-id",
			Version:         "0.0.1",
			LatestVersion:   "0.0.1",
			OriginalVersion: "0",
			GroupInfo:       models.StepGroupInfoModel{Maintainer: "stale"},
			Step:            models.StepModel{Title: pointers.NewStringPtr("Caller Title")},
		}
		populateStepInfo(&target, source)
		assertSourceFieldsCopied(t, target, source)
	})
}

func assertSourceFieldsCopied(t *testing.T, got, want models.StepInfoModel) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}
	if got.LatestVersion != want.LatestVersion {
		t.Errorf("LatestVersion = %q, want %q", got.LatestVersion, want.LatestVersion)
	}
	if got.OriginalVersion != want.OriginalVersion {
		t.Errorf("OriginalVersion = %q, want %q", got.OriginalVersion, want.OriginalVersion)
	}
	if got.GroupInfo.Maintainer != want.GroupInfo.Maintainer {
		t.Errorf("GroupInfo.Maintainer = %q, want %q", got.GroupInfo.Maintainer, want.GroupInfo.Maintainer)
	}
}
