package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/bitrise-io/go-utils/colorstring"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
	"github.com/urfave/cli"
)

func printRawStepList(stepList models.StepListModel, isShort bool) {
	fmt.Println(colorstring.Bluef("Step in StepLib (%s):", stepList.StepLib))
	for _, stepID := range stepList.Steps {
		fmt.Printf("%s\n", stepID)
	}
	fmt.Println()
	fmt.Println()
}

func printJSONStepList(stepList models.StepListModel, isShort bool) error {
	bytes, err := json.Marshal(stepList)
	if err != nil {
		return err
	}

	fmt.Println(string(bytes))
	return nil
}

func listSteps(stepLibURI, format string, isLocalStepLib bool) error {
	// Check if setup was done for collection
	if exist, err := stepman.RootExistForCollection(stepLibURI); err != nil {
		return err
	} else if !exist {
		if err := setupSteplib(stepLibURI, isLocalStepLib, format != OutputFormatRaw); err != nil {
			log.Fatal("Failed to setup steplib")
		}
	}

	stepLib, err := stepman.ReadStepSpec(stepLibURI)
	if err != nil {
		return err
	}

	// List
	stepList := models.StepListModel{
		StepLib: stepLibURI,
	}
	for stepID := range stepLib.Steps {
		stepList.Steps = append(stepList.Steps, stepID)
	}

	switch format {
	case OutputFormatRaw:
		printRawStepList(stepList, false)
		break
	case OutputFormatJSON:
		if err := printJSONStepList(stepList, false); err != nil {
			return err
		}
		break
	default:
		return fmt.Errorf("Invalid format: %s", format)
	}
	return nil
}

func stepList(c *cli.Context) error {
	// Input validation
	stepLibURI := c.String(CollectionKey)
	if stepLibURI == "" {
		return fmt.Errorf("Missing required input: collection")
	}

	format := c.String(FormatKey)
	if format == "" {
		format = OutputFormatRaw
	} else if !(format == OutputFormatRaw || format == OutputFormatJSON) {
		log.Fatalf("Invalid format: %s", format)
	}

	isLocalStepLib := false
	if strings.HasPrefix(stepLibURI, "file://") {
		isLocalStepLib = true
		stepLibURI = strings.TrimPrefix(stepLibURI, "file://")
	}

	if err := listSteps(stepLibURI, format, isLocalStepLib); err != nil {
		log.Errorf("Failed to list steps in StepLib (%s), err: %s", stepLibURI, err)
	}

	return nil
}
