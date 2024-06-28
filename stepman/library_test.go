package stepman

import (
	"errors"
	"fmt"
	"testing"

	"github.com/bitrise-io/go-utils/command"
)

func BenchmarkStepLibSetup(b *testing.B) {
	for _, mode := range []string{"repo-based", "spec-json-based"} {
		b.Run(fmt.Sprintf("Benchmarking UpdateLibrary=%s", mode), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				var steplib string
				if mode == "repo-based" {
					// spec.json based update is only supported for the default StepLib URI ( "https://github.com/bitrise-io/bitrise-steplib.git")
					steplib = "https://github.com/bitrise-io/bitrise-steplib"
				} else {
					steplib = "https://github.com/bitrise-io/bitrise-steplib.git"
				}

				b.StopTimer()
				route, found := ReadRoute(steplib)
				if found {
					if err := CleanupRoute(route); err != nil {
						b.Fatal(err)
					}
				}
				b.StartTimer()

				if err := SetupLibrary(steplib, NopeLogger{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkStepLibUpdate(b *testing.B) {
	oldCommit := "bf150ba4c10a05b9dfb063178746cb76286d04f1" // Commits on Apr 14, 2022

	for _, mode := range []string{"repo-based", "spec-json-based"} {
		b.Run(fmt.Sprintf("Benchmarking UpdateLibrary=%s", mode), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				var steplib string
				if mode == "repo-based" {
					// spec.json based update is only supported for the default StepLib URI ( "https://github.com/bitrise-io/bitrise-steplib.git")
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

				if _, err := UpdateLibrary(steplib, NopeLogger{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func setupStepLib(uri, commit string) error {
	route, found := ReadRoute(uri)
	if found {
		if err := CleanupRoute(route); err != nil {
			return err
		}
	}

	if err := SetupLibrary(uri, NopeLogger{}); err != nil {
		return err
	}

	route, found = ReadRoute(uri)
	if !found {
		return errors.New("no rout found")
	}

	pth := GetLibraryBaseDirPath(route)
	cmd := command.New("git", "reset", "--hard", commit)
	cmd.SetDir(pth)
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

type NopeLogger struct {
}

func (l NopeLogger) Warnf(format string, v ...interface{}) {}

func (l NopeLogger) Debugf(format string, v ...interface{}) {}

func (l NopeLogger) Errorf(format string, v ...interface{}) {}

func (l NopeLogger) Infof(format string, v ...interface{}) {}
