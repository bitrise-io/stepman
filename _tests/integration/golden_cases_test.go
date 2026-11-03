package integration

// validStepYAML is a self-contained step.yml the audit command accepts in
// the pre-share mode (title/summary/website present; source/published_at NOT
// required until share-pr audit).
const validStepYAML = `title: Audit Fixture
summary: Minimal step that passes "stepman audit --step-yml".
description: Used by the golden suite's audit case.
website: https://example.com/audit-fixture
run_if: ""
inputs:
- example_input: default
  opts:
    title: Example
    summary: Example input.
`

var goldenCases = []goldenCase{
	{name: "version", steps: []goldenStep{
		stepmanCmd("--version"),
		stepmanCmd("-v"),
		stepmanCmd("version"),
	}},
	{name: "setup", needsSteplib: true, steps: []goldenStep{
		stepmanCmd("setup", "-c", "file://$STEPLIB"),
		// re-run: should be a no-op because route already exists.
		stepmanCmd("setup", "-c", "file://$STEPLIB"),
	}},
	{name: "update", needsSteplib: true, steps: []goldenStep{
		stepmanCmd("setup", "-c", "file://$STEPLIB"),
		stepmanCmd("update", "-c", "file://$STEPLIB"),
	}},
	{name: "collections", needsSteplib: true, steps: []goldenStep{
		// Empty state: no steplib has been set up yet.
		stepmanCmd("collections"),
		stepmanCmd("collections", "--format", "json"),
		stepmanCmd("setup", "-c", "file://$STEPLIB"),
		stepmanCmd("collections"),
		stepmanCmd("collections", "--format", "json"),
	}},
	{name: "step-list", needsSteplib: true, steps: []goldenStep{
		stepmanCmd("setup", "-c", "file://$STEPLIB"),
		stepmanCmd("step-list", "-c", "file://$STEPLIB"),
		stepmanCmd("step-list", "-c", "file://$STEPLIB", "--format", "json"),
	}},
	{name: "step-info", needsSteplib: true, steps: []goldenStep{
		stepmanCmd("setup", "-c", "file://$STEPLIB"),
		stepmanCmd("step-info", "--library", "file://$STEPLIB", "--id", "test-step", "--version", "1.0.0", "--format", "raw"),
		stepmanCmd("step-info", "--library", "file://$STEPLIB", "--id", "test-step", "--version", "1.0.0", "--format", "json"),
	}},
	// NOTE: `download` is intentionally omitted — the subcommand clones the
	// step's `source.git` from a public remote and the fixture's placeholder URL
	// cannot be served locally (step.yml audit rejects anything that isn't
	// http(s)://*.git), so a golden here would depend on github.com reachability
	// and GitHub's live 404 wording. Live-steplib coverage of download lives in
	// TestStepInfo / update_test.go.
	{name: "audit-step-yml", steps: []goldenStep{
		// Local step.yml audit — no steplib needed, no network. Smoke-tests
		// both the success wording and the error path.
		stepmanCmd("audit", "--step-yml", "test-step/step.yml"),
	}, files: map[string]string{
		"test-step/step.yml": validStepYAML,
	}},
	// NOTE: `audit -c <steplib>` is omitted — it iterates every step in the
	// collection and git-clones each `source.git` to validate the commit SHA,
	// which requires network reachability. The step-yml form above covers the
	// local-validation code path.
	{name: "delete", needsSteplib: true, steps: []goldenStep{
		stepmanCmd("setup", "-c", "file://$STEPLIB"),
		stepmanCmd("delete", "-c", "file://$STEPLIB"),
		// delete when nothing is set up: must stay non-zero and informative.
		stepmanCmd("delete", "-c", "file://$STEPLIB"),
	}},
	{name: "export-spec", needsSteplib: true, steps: []goldenStep{
		stepmanCmd("setup", "-c", "file://$STEPLIB"),
		stepmanCmd("export-spec", "--steplib", "file://$STEPLIB", "--output", "$WORK/out-full.json", "--export-type", "full"),
		stepmanCmd("export-spec", "--steplib", "file://$STEPLIB", "--output", "$WORK/out-latest.json", "--export-type", "latest"),
		stepmanCmd("export-spec", "--steplib", "file://$STEPLIB", "--output", "$WORK/out-minimal.json", "--export-type", "minimal"),
	}},
	// `share` covers only the two offline-safe subcommands (`share` help and
	// `share start`). `share create`'s step-audit rejects any non-http(s)
	// source URL, and the subsequent re-audit (and `share audit` and
	// `share finish`) clone the step from `source.git`, which turns the test
	// into a GitHub-reachability check. Documented as a skip in the PR body.
	{name: "share", needsSteplib: true, steps: []goldenStep{
		stepmanCmd("share"),
		stepmanCmd("share", "start", "-c", "file://$STEPLIB"),
	}},
}
