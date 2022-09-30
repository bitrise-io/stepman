package cli

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/stepman/stepman"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

func setup(c *cli.Context) error {
	// Input validation
	steplibURI := c.String(CollectionKey)
	if steplibURI == "" {
		log.Fatal("No step collection specified")
	}

	copySpecJSONPath := c.String(CopySpecJSONKey)

	if c.IsSet(LocalCollectionKey) {
		log.Warn("'local' flag is deprecated")
		log.Warn("use 'file://' prefix in steplib path instead")
		fmt.Println()
	}

	if c.Bool(LocalCollectionKey) {
		if !strings.HasPrefix(steplibURI, "file://") {
			log.Warnf("Appending file path prefix (file://) to StepLib (%s)", steplibURI)
			steplibURI = "file://" + steplibURI
			log.Warnf("From now you can refer to this StepLib with URI: %s", steplibURI)
			log.Warnf("For example, to delete StepLib call: `stepman delete --collection %s`", steplibURI)
		}
	}

	return Setup(steplibURI, copySpecJSONPath)
}

func Setup(steplibURI, copySpecJSONPath string) error {
	if steplibURI == "" {
		return fmt.Errorf("no step library specified")
	}

	// Setup
	if err := stepman.SetupLibrary(steplibURI); err != nil {
		return fmt.Errorf("setup failed: %s", err)
	}

	// Copy spec.json
	if copySpecJSONPath != "" {
		route, found := stepman.ReadRoute(steplibURI)
		if !found {
			return fmt.Errorf("no route found for steplib (%s)", steplibURI)
		}

		sourceSpecJSONPth := stepman.GetStepSpecPath(route)
		if err := command.CopyFile(sourceSpecJSONPth, copySpecJSONPath); err != nil {
			return fmt.Errorf("failed to copy spec.json from (%s) to (%s): %s", sourceSpecJSONPth, copySpecJSONPath, err)
		}
	}

	return nil
}
