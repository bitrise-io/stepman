package integration

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/command/git"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/stepman"
	"github.com/stretchr/testify/require"
)

func TestUpdate(t *testing.T) {
	t.Log("remote library")
	{
		out, err := command.New(binPath(), "delete", "-c", defaultLibraryURI).RunAndReturnTrimmedCombinedOutput()
		require.NoError(t, err, out)

		out, err = command.New(binPath(), "setup", "-c", defaultLibraryURI).RunAndReturnTrimmedCombinedOutput()
		require.NoError(t, err, out)

		out, err = command.New(binPath(), "update", "-c", defaultLibraryURI).RunAndReturnTrimmedCombinedOutput()
		require.NoError(t, err, out)
	}

	t.Log("local library")
	{
		tmpDir, err := pathutil.NormalizedOSTempDirPath("__library__")
		require.NoError(t, err)
		defer func() {
			require.NoError(t, os.RemoveAll(tmpDir))
		}()
		repo, err := git.New(tmpDir)
		require.NoError(t, err)
		require.NoError(t, repo.Clone(defaultLibraryURI).Run())

		out, err := command.New(binPath(), "delete", "-c", "file://"+tmpDir).RunAndReturnTrimmedCombinedOutput()
		require.NoError(t, err, out)

		out, err = command.New(binPath(), "setup", "-c", "file://"+tmpDir).RunAndReturnTrimmedCombinedOutput()
		require.NoError(t, err, out)

		out, err = command.New(binPath(), "update", "-c", tmpDir).RunAndReturnTrimmedCombinedOutput()
		require.Error(t, err, out)

		out, err = command.New(binPath(), "update", "-c", "file://"+tmpDir).RunAndReturnTrimmedCombinedOutput()
		require.NoError(t, err, out)

		out, err = command.New(binPath(), "delete", "-c", "file://"+tmpDir).RunAndReturnTrimmedCombinedOutput()
		require.NoError(t, err, out)
	}
}

func Test_StepLibSetup(t *testing.T) {
	steplib := "https://github.com/bitrise-io/bitrise-steplib.git"
	oldCommit := "bf150ba4c10a05b9dfb063178746cb76286d04f1" // Commits on Apr 14, 2022
	err := setupStepLib(steplib, oldCommit)
	if err != nil {
		t.Fatal(err)
	}
}

func Test_StepLibUpdate(t *testing.T) {
	steplib := "https://github.com/bitrise-io/bitrise-steplib.git"
	oldCommit := "bf150ba4c10a05b9dfb063178746cb76286d04f1" // Commits on Apr 14, 2022
	err := setupStepLib(steplib, oldCommit)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stepman.UpdateLibrary(steplib, NopeLogger{}); err != nil {
		t.Fatal(err)
	}
}

func Test_StepLibUpdate2(t *testing.T) {
	steplib := "https://github.com/bitrise-io/bitrise-steplib.git"
	oldCommit := "bf150ba4c10a05b9dfb063178746cb76286d04f1" // Commits on Apr 14, 2022
	err := setupStepLib(steplib, oldCommit)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stepman.UpdateLibrary(steplib, NopeLogger{}); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkStepLibUpdate(b *testing.B) {
	oldCommit := "bf150ba4c10a05b9dfb063178746cb76286d04f1" // Commits on Apr 14, 2022

	for _, mode := range []string{"old", "new"} {
		b.Run(fmt.Sprintf("Benchmarking stepman.UpdateLibrary=%s", mode), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				var steplib string
				if mode == "old" {
					steplib = "https://github.com/bitrise-io/bitrise-steplib"

					b.StopTimer()
					err := setupStepLib(steplib, oldCommit)
					if err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
				} else {
					steplib = "https://github.com/bitrise-io/bitrise-steplib.git"
				}

				if _, err := stepman.UpdateLibrary(steplib, NopeLogger{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkStepLibSetup(b *testing.B) {
	for _, mode := range []string{"old", "new"} {
		b.Run(fmt.Sprintf("Benchmarking stepman.UpdateLibrary=%s", mode), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				var steplib string
				if mode == "old" {
					steplib = "https://github.com/bitrise-io/bitrise-steplib"
				} else {
					steplib = "https://github.com/bitrise-io/bitrise-steplib.git"
				}

				b.StopTimer()
				route, found := stepman.ReadRoute(steplib)
				if found {
					if err := stepman.CleanupRoute(route); err != nil {
						b.Fatal(err)
					}
				}
				b.StartTimer()

				if err := stepman.SetupLibrary(steplib, NopeLogger{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func setupStepLib(uri, commit string) error {
	route, found := stepman.ReadRoute(uri)
	if found {
		if err := stepman.CleanupRoute(route); err != nil {
			return err
		}
	}

	if err := stepman.SetupLibrary(uri, NopeLogger{}); err != nil {
		return err
	}

	route, found = stepman.ReadRoute(uri)
	if !found {
		return errors.New("no rout found")
	}

	pth := stepman.GetLibraryBaseDirPath(route)
	cmd := command.New("git", "reset", "--hard", commit)
	cmd.SetDir(pth)
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

type NopeLogger struct {
}

func (l NopeLogger) Warnf(format string, v ...interface{}) {
	return
}

func (l NopeLogger) Debugf(format string, v ...interface{}) {
	return
}

func (l NopeLogger) Errorf(format string, v ...interface{}) {
	return
}

func (l NopeLogger) Infof(format string, v ...interface{}) {
	return
}
