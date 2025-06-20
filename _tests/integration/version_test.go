package integration

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/stepman/version"
	"github.com/stretchr/testify/require"
)

func Test_VersionOutput(t *testing.T) {
	t.Log("Version")
	{
		out, err := command.RunCommandAndReturnCombinedStdoutAndStderr(binPath(), "version")
		require.NoError(t, err, out)
		require.Equal(t, version.Version, out)
	}

	t.Log("Version --full")
	{
		out, err := command.RunCommandAndReturnCombinedStdoutAndStderr(binPath(), "version", "--full")
		require.NoError(t, err, out)

		expectedOSVersion := fmt.Sprintf("%s (%s)", runtime.GOOS, runtime.GOARCH)
		expectedVersionOut := fmt.Sprintf(`version: %s
os: %s
go: %s
build_number: 
commit:`, version.Version, expectedOSVersion, runtime.Version())

		require.Equal(t, expectedVersionOut, out)
	}
}
