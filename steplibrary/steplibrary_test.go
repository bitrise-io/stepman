package steplibrary

import (
	"testing"

	"github.com/bitrise-io/stepman/steplibrary/steplibindex"
	"github.com/stretchr/testify/assert"
)

func TestToStepGroupInfoModel(t *testing.T) {
	t.Run("active step has empty deprecation fields", func(t *testing.T) {
		got := toStepGroupInfoModel(steplibindex.StepInfo{
			Maintainer:  "bitrise",
			Deprecation: nil,
			AssetURLs:   []string{"assets/icon.svg"},
		})
		assert.Equal(t, "bitrise", got.Maintainer, "Maintainer")
		assert.Empty(t, got.RemovalDate, "RemovalDate")
		assert.Empty(t, got.DeprecateNotes, "DeprecateNotes")
		assert.Equal(t, "assets/icon.svg", got.AssetURLs["icon.svg"], "AssetURLs[icon.svg]")
	})

	t.Run("deprecated step flattens nested fields", func(t *testing.T) {
		got := toStepGroupInfoModel(steplibindex.StepInfo{
			Maintainer: "community",
			Deprecation: &steplibindex.Deprecation{
				RemovalDate: "2025-12-31",
				Notes:       "Replaced by `new-step`.",
			},
			AssetURLs: nil,
		})
		assert.Equal(t, "2025-12-31", got.RemovalDate, "RemovalDate")
		assert.Equal(t, "Replaced by `new-step`.", got.DeprecateNotes, "DeprecateNotes")
		assert.Equal(t, "community", got.Maintainer, "Maintainer")
	})
}
