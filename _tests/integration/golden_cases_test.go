package integration

var goldenCases = []goldenCase{
	{name: "version", steps: []goldenStep{
		stepmanCmd("--version"),
		stepmanCmd("-v"),
		stepmanCmd("version"),
	}},
}
