package stepman

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/command/git"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/stepman/models"
)

const (
	filePathPrefix            = "file://"
	defaultStepLib            = "https://github.com/bitrise-io/bitrise-steplib.git"
	defaultStepLibSpecJSONURL = "http://bitrise-steplib-collection.s3.amazonaws.com/spec.json"
)

// Logger ...
type Logger interface {
	Debugf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Infof(format string, v ...interface{})
}

func SetupLibrary(libraryURI string, log Logger) error {
	if exist, err := RootExistForLibrary(libraryURI); err != nil {
		return fmt.Errorf("failed to check if routing exist for library (%s), error: %s", libraryURI, err)
	} else if exist {
		return nil
	}

	alias := GenerateFolderAlias()
	route := SteplibRoute{
		SteplibURI:  libraryURI,
		FolderAlias: alias,
	}

	isLocalLibrary := strings.HasPrefix(libraryURI, filePathPrefix)
	if isLocalLibrary {
		if err := setupWithLocalStepLib(libraryURI, route); err != nil {
			// TODO: is cleanup needed?
			if err := CleanupRoute(route); err != nil {
				log.Warnf("Failed to cleanup routing for library (%s), error: %s", libraryURI, err)
			}

			return err
		}

		return nil
	}

	isCustomStepLibrary := libraryURI != defaultStepLib
	if isCustomStepLibrary {
		if err := setupWithStepLibRepo(libraryURI, route); err != nil {
			if err := CleanupRoute(route); err != nil {
				log.Warnf("Failed to cleanup routing for library (%s), error: %s", libraryURI, err)
			}

			return err
		}
		return nil
	}

	if err := setupWithStepLibSpecURL(libraryURI, defaultStepLibSpecJSONURL, route); err != nil {
		if err := setupWithStepLibRepo(libraryURI, route); err != nil {
			if err := CleanupRoute(route); err != nil {
				log.Warnf("Failed to cleanup routing for library (%s), error: %s", libraryURI, err)
			}

			return err
		}
		return nil
	}

	return nil
}

func UpdateLibrary(libraryURI string, log Logger) (models.StepCollectionModel, error) {
	route, found := ReadRoute(libraryURI)
	if !found {
		if err := CleanupDanglingLibrary(libraryURI); err != nil {
			log.Warnf("Failed to cleaning up library (%s), error: %s", libraryURI, err)
		}
		return models.StepCollectionModel{}, fmt.Errorf("no route found for library: %s", libraryURI)
	}

	isLocalLibrary := strings.HasPrefix(libraryURI, filePathPrefix)
	if isLocalLibrary {
		if err := CleanupRoute(route); err != nil {
			return models.StepCollectionModel{}, fmt.Errorf("failed to cleanup route for library (%s), error: %s", libraryURI, err)
		}

		if err := setupWithLocalStepLib(libraryURI, route); err != nil {
			return models.StepCollectionModel{}, fmt.Errorf("failed to setup library (%s), error: %s", libraryURI, err)
		}

		return ReadStepSpec(libraryURI)
	}

	isCustomStepLibrary := libraryURI != defaultStepLib
	if isCustomStepLibrary {
		if err := updateWithStepLibRepo(libraryURI, route); err != nil {
			return models.StepCollectionModel{}, fmt.Errorf("failed to update library (%s), error: %s", libraryURI, err)
		}

		return ReadStepSpec(libraryURI)
	}

	if err := setupWithStepLibSpecURL(libraryURI, defaultStepLibSpecJSONURL, route); err != nil {
		if err := updateWithStepLibRepo(libraryURI, route); err != nil {
			return models.StepCollectionModel{}, fmt.Errorf("failed to update library (%s), error: %s", libraryURI, err)
		}

		return ReadStepSpec(libraryURI)
	}

	return ReadStepSpec(libraryURI)
}

func setupWithLocalStepLib(libraryURI string, route SteplibRoute) error {
	libraryBaseDir := GetLibraryBaseDirPath(route)

	if err := os.MkdirAll(libraryBaseDir, 0777); err != nil {
		return fmt.Errorf("failed to create library dir (%s), error: %s", libraryBaseDir, err)
	}

	libraryFilePath := libraryURI
	if strings.HasPrefix(libraryFilePath, filePathPrefix) {
		libraryFilePath = strings.TrimPrefix(libraryURI, filePathPrefix)
	}

	if err := command.CopyDir(libraryFilePath, libraryBaseDir, true); err != nil {
		return fmt.Errorf("failed to copy dir (%s) to (%s), error: %s", libraryFilePath, libraryBaseDir, err)
	}

	if err := ReGenerateLibrarySpec(route); err != nil {
		return fmt.Errorf("failed to re-generate library (%s), error: %s", libraryURI, err)
	}

	if err := AddRoute(route); err != nil {
		return fmt.Errorf("failed to add routing, error: %s", err)
	}
	return nil
}

func setupWithStepLibRepo(libraryURI string, route SteplibRoute) error {
	libraryBaseDir := GetLibraryBaseDirPath(route)

	if err := retry.Times(2).Wait(3 * time.Second).Try(func(attempt uint) error {
		repo, err := git.New(libraryBaseDir)
		if err != nil {
			return err
		}
		return repo.Clone(libraryURI).Run()
	}); err != nil {
		return fmt.Errorf("failed to clone library (%s), error: %s", libraryURI, err)
	}

	if err := ReGenerateLibrarySpec(route); err != nil {
		return fmt.Errorf("failed to re-generate library (%s), error: %s", libraryURI, err)
	}

	if err := AddRoute(route); err != nil {
		return fmt.Errorf("failed to add routing, error: %s", err)
	}

	return nil
}

func updateWithStepLibRepo(libraryURI string, route SteplibRoute) error {
	pth := GetLibraryBaseDirPath(route)

	if err := retry.Times(2).Wait(3 * time.Second).Try(func(attempt uint) error {
		repo, err := git.New(pth)
		if err != nil {
			return err
		}
		cmd := repo.Pull()
		return cmd.Run()
	}); err != nil {
		return fmt.Errorf("failed to pull library (%s), error: %s", libraryURI, err)
	}

	if err := ReGenerateLibrarySpec(route); err != nil {
		return fmt.Errorf("failed to generate spec for library (%s), error: %s", libraryURI, err)
	}

	return nil
}

func setupWithStepLibSpecURL(libraryURI string, specURL string, route SteplibRoute) error {
	specPath := GetStepSpecPath(route)

	if err := os.MkdirAll(filepath.Dir(specPath), 0755); err != nil {
		return err
	}

	if err := retry.Times(2).Wait(3 * time.Second).Try(func(attempt uint) error {
		specPath := GetStepSpecPath(route)

		if err := os.MkdirAll(filepath.Dir(specPath), 0755); err != nil {
			return err
		}

		if err := downloadSpec(specPath, specURL); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to clone library (%s), error: %s", libraryURI, err)
	}

	if err := AddRoute(route); err != nil {
		return fmt.Errorf("failed to add routing, error: %s", err)
	}

	return nil
}

func downloadSpec(filepath string, url string) error {
	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}
