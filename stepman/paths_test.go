package stepman

import (
	"github.com/stretchr/testify/assert"
	"os"
	"path"
	"testing"
)

const (
	GivenSteplibURI  = "https://github.com/bitrise-io/steplib"
	GivenFolderAlias = "12334556"
	GivenHomePath    = "/usr/testeruser/"
	GivenStepID      = "test-custom-step"
	GivenStepVersion = "0.5.6"
)

func GivenRoute() SteplibRoute {
	return SteplibRoute{
		SteplibURI:  GivenSteplibURI,
		FolderAlias: GivenFolderAlias,
	}
}

func Test_GivenHomeDir_WhenGetStepmanDirPathCalled_ThenGoodPathReturned(t *testing.T) {
	// Given
	os.Setenv("HOME", GivenHomePath)
	expected := path.Join(GivenHomePath, ".stepman")

	// When
	actual := GetStepmanDirPath()

	// Then
	assert.Equal(t, actual, expected)
}

func Test_GivenStepmanDir_WhenGetCollectionDirPathCalled_ThenGoodPathReturned(t *testing.T) {
	// Given
	os.Setenv("HOME", GivenHomePath)
	expected := path.Join(GetStepmanDirPath(), "step_collections")

	// When
	actual := GetCollectionsDirPath()

	// Then
	assert.Equal(t, actual, expected)
}

func Test_GivenRoute_WhenGetLibraryBaseDirPathCalled_ThenGoodPathReturned(t *testing.T) {
	// Given
	route := GivenRoute()
	expected := path.Join(GetCollectionsDirPath(), route.FolderAlias, "collection")

	// When
	actual := GetLibraryBaseDirPath(route)

	// Then
	assert.Equal(t, expected, actual)
}

func Test_GivenRouteAndStepId_WhenGetStepCollectionDirPath_ThenGoodPathReturned(t *testing.T) {
	// Given
	route := GivenRoute()
	step := GivenStepID
	version := GivenStepVersion
	expected := path.Join(GetLibraryBaseDirPath(route), "steps", step, version)

	// When
	actual := GetStepCollectionDirPath(route, step, version)

	// Then
	assert.Equal(t, expected, actual)
}

func Test_GivenRouteAndStepId_WhenGetStepGlobalInfoPathCalled_ThenGoodPathReturned(t *testing.T) {
	// Given
	route := GivenRoute()
	step := GivenStepID
	expected := path.Join(GetLibraryBaseDirPath(route), "steps", step, "step-info.yml")

	// When
	actual := GetStepGlobalInfoPath(route, step)

	// Then
	assert.Equal(t, expected, actual)
}
