package integration

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
}
