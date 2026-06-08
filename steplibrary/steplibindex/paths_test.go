package steplibindex

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPathURL_escaping covers the URL-form helpers' url.PathEscape behavior
// on step IDs and versions that contain characters reserved in URL path
// segments. It isolates the escaping rule so a regression fails loudly.
func TestPathURL_escaping(t *testing.T) {
	t.Run("LatestPointerPathURL", func(t *testing.T) {
		cases := []struct{ stepID, want string }{
			{"git-clone", "/v2/index/steps/git-clone/latest.json"},
			{"with space", "/v2/index/steps/with%20space/latest.json"},
			{"with/slash", "/v2/index/steps/with%2Fslash/latest.json"},
			{"weird?chars", "/v2/index/steps/weird%3Fchars/latest.json"},
			// url.PathEscape leaves "+" alone — it's valid unescaped in
			// URL path segments per RFC 3986. The line documents the
			// behavior so a future change to escape it is intentional.
			{"with+plus", "/v2/index/steps/with+plus/latest.json"},
		}
		for _, c := range cases {
			assert.Equal(t, c.want, LatestPointerPathURL(c.stepID), "stepID=%q", c.stepID)
		}
	})

	t.Run("StepJSONPathURL_escapes_both_segments", func(t *testing.T) {
		// Both stepID and version are url.PathEscape'd.
		assert.Equal(t,
			"/v2/steps/git-clone/1.0.0/step.json",
			StepJSONPathURL("git-clone", "1.0.0"),
		)
		assert.Equal(t,
			"/v2/steps/with%2Fslash/1.0.0/step.json",
			StepJSONPathURL("with/slash", "1.0.0"),
		)
		assert.Equal(t,
			"/v2/steps/git-clone/1.0.0%20beta/step.json",
			StepJSONPathURL("git-clone", "1.0.0 beta"),
		)
	})

	t.Run("VersionsPathURL_and_StepInfoPathURL", func(t *testing.T) {
		// Confirm escaping wires through the other URL helpers too.
		assert.Equal(t, "/v2/index/steps/has%2Fslash/versions.json", VersionsPathURL("has/slash"))
		assert.Equal(t, "/v2/steps/has%2Fslash/step-info.json", StepInfoPathURL("has/slash"))
	})
}

// TestPathURL_constants documents the constant URL forms. Locks them in so
// a typo in the layout-rename refactor would surface here.
func TestPathURL_constants(t *testing.T) {
	assert.Equal(t, "/v2/meta.json", MetaPathURL())
	assert.Equal(t, "/v2/index/step_ids.json", StepIDsPathURL())
}

// TestPathFS documents the filesystem-form helpers. The FS forms are what the
// generator and validator use; lock the v2-rooted layout in.
func TestPathFS(t *testing.T) {
	assert.Equal(t, "v2/meta.json", MetaPathFS())
	assert.Equal(t, "v2/index/step_ids.json", StepIDsPathFS())
	assert.Equal(t, "v2/index/steps/git-clone/latest.json", LatestPointerPathFS("git-clone"))
	assert.Equal(t, "v2/index/steps/git-clone/versions.json", VersionsPathFS("git-clone"))
	assert.Equal(t, "v2/index/steps/git-clone", IndexStepDirFS("git-clone"))
	assert.Equal(t, "v2/steps/git-clone/step-info.json", StepInfoPathFS("git-clone"))
	assert.Equal(t, "v2/steps/git-clone/1.0.0/step.json", StepJSONPathFS("git-clone", "1.0.0"))
	assert.Equal(t, "v2/steps/git-clone/assets", StepAssetDirFS("git-clone"))
	assert.Equal(t, "v2/steps/git-clone/assets/icon.svg", StepAssetPathFS("git-clone", "icon.svg"))
	assert.Equal(t, "v2/steps/git-clone", StepDirFS("git-clone"))
}
