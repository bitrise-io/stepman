package indexgen

import (
	"cmp"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/steplibindex"
	"github.com/bitrise-io/stepman/stepman"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

// TestRoundTrip_stepYAML_to_stepJSON locks in the central V2 wire-format
// contract: a generated step.json must be exactly the V1 step.yml that
// produced it, just JSON-encoded.
//
// It runs against the real default steplib — every step, every version — so it
// needs network access and is gated on RUN_STEPLIB_GEN (skipped in CI):
//
//	RUN_STEPLIB_GEN=1 go test -run TestRoundTrip -v ./steplibrary/indexgen/
//
// STEPLIB_URI overrides the source (default: bitrise-steplib). For every
// step.yml in the steplib it:
//
//  1. Parses the source step.yml through the same pipeline the generator
//     uses (yaml.Unmarshal + Normalize + FillMissingDefaults) → fromYAML.
//  2. Parses the generated step.json into a models.StepModel → fromJSON.
//  3. Marshals both back to JSON and asserts semantic equality (assert.JSONEq).
//
// Comparing via the JSON wire format rather than reflect.DeepEqual on the
// Go structs is deliberate: an empty slice and a nil slice both serialize
// to the same JSON under omitempty, so they're semantically equivalent for
// every consumer of the wire format. The contract is "the bytes a consumer
// receives carry the same step", not "the in-memory Go struct is
// bit-identical."
func TestRoundTrip_stepYAML_to_stepJSON(t *testing.T) {
	if os.Getenv("RUN_STEPLIB_GEN") == "" {
		t.Skip("set RUN_STEPLIB_GEN=1 to round-trip every step in a real steplib")
	}
	uri := cmp.Or(os.Getenv("STEPLIB_URI"), "https://github.com/bitrise-io/bitrise-steplib.git")

	// Generate the V2 tree (Generate clones the steplib into stepman's local
	// cache), then read the clone back to pair each step.yml with its step.json.
	out := t.TempDir()
	_, err := Generate(uri, out, Options{}, testLogger{t})
	require.NoError(t, err, "Generate")

	route, found := stepman.ReadRoute(uri)
	require.True(t, found, "no route for %s after Generate", uri)
	inputFS := os.DirFS(stepman.GetLibraryBaseDirPath(route))
	v2Dir := filepath.Join(out, steplibindex.VersionDir())

	pairs := collectStepYMLAndJSONPaths(t, inputFS, v2Dir)
	require.NotEmpty(t, pairs, "expected at least one step.yml/step.json pair to compare")
	t.Logf("round-tripping %d step versions from %s", len(pairs), uri)

	for _, p := range pairs {
		t.Run(p.yamlPath, func(t *testing.T) {
			fromYAML := parseStepYMLForRoundTrip(t, inputFS, p.yamlPath)
			fromJSON := parseStepJSONForRoundTrip(t, p.jsonPath)

			yamlAsJSON, err := json.Marshal(fromYAML)
			require.NoError(t, err, "re-marshal fromYAML")
			jsonAsJSON, err := json.Marshal(fromJSON)
			require.NoError(t, err, "re-marshal fromJSON")

			assert.JSONEq(t, string(yamlAsJSON), string(jsonAsJSON),
				"step.yml and the generated step.json must serialize to semantically equal JSON")
		})
	}
}

type stepYAMLJSONPair struct {
	yamlPath string // path within inputFS, e.g. "steps/hello-step/1.0.0/step.yml"
	jsonPath string // absolute path on disk, e.g. "<v2>/steps/hello-step/1.0.0/step.json"
}

// collectStepYMLAndJSONPaths walks inputFS's source steps/ tree for every
// step.yml and returns the matching pair of (input step.yml path, generated
// step.json path under the v2 output dir). Asserts each generated step.json
// exists. Non-semver version dirs are skipped to match the generator.
func collectStepYMLAndJSONPaths(t *testing.T, inputFS fs.FS, outV2Dir string) []stepYAMLJSONPair {
	t.Helper()
	var pairs []stepYAMLJSONPair

	require.NoError(t, fs.WalkDir(inputFS, steplibindex.StepsRootFS, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Base(p) != "step.yml" {
			return nil
		}
		// p looks like "steps/<id>/<version>/step.yml" — drop the trailing
		// "/step.yml" to get the version dir, then locate the JSON counterpart
		// in the v2 output (which mirrors the steps/<id>/<version>/ layout).
		rel := strings.TrimSuffix(p, "/step.yml") // steps/<id>/<version>
		segs := strings.Split(rel, "/")
		if len(segs) != 3 {
			return nil // not a steps/<id>/<version> path
		}
		if _, err := models.ParseSemver(segs[2]); err != nil {
			return nil // non-semver version dir: generator skips these
		}
		jsonPath := filepath.Join(outV2Dir, rel, "step.json")
		require.FileExists(t, jsonPath, "generated step.json missing for %s", p)
		pairs = append(pairs, stepYAMLJSONPair{yamlPath: p, jsonPath: jsonPath})
		return nil
	}))
	return pairs
}

// parseStepYMLForRoundTrip mirrors the generator's canonical step.yml parse
// pipeline (yaml.Unmarshal + Normalize + FillMissingDefaults). It is
// duplicated here on purpose: the round-trip property we're asserting is
// "the bytes the generator emits decode back to whatever its pipeline
// produced", so we re-derive the expected model from first principles
// rather than calling the generator's internal helper.
func parseStepYMLForRoundTrip(t *testing.T, inputFS fs.FS, ymlPath string) models.StepModel {
	t.Helper()
	bytes, err := fs.ReadFile(inputFS, ymlPath)
	require.NoError(t, err, "read %s", ymlPath)

	var step models.StepModel
	require.NoError(t, yaml.Unmarshal(bytes, &step), "yaml.Unmarshal %s", ymlPath)
	require.NoError(t, step.Normalize(), "Normalize %s", ymlPath)
	require.NoError(t, step.FillMissingDefaults(), "FillMissingDefaults %s", ymlPath)
	return step
}

func parseStepJSONForRoundTrip(t *testing.T, jsonPath string) models.StepModel {
	t.Helper()
	bytes, err := os.ReadFile(jsonPath)
	require.NoError(t, err, "read %s", jsonPath)

	var step models.StepModel
	require.NoError(t, json.Unmarshal(bytes, &step), "json.Unmarshal %s", jsonPath)
	return step
}
