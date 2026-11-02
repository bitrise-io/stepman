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
}
