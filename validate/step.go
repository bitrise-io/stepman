package validate

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	envmanModels "github.com/bitrise-io/envman/models"
	"github.com/bitrise-io/stepman/models"
)

const maxSummaryLength = 100

func inputOutput(env envmanModels.EnvironmentItemModel) error {
	key, value, err := env.GetKeyValuePair()
	if err != nil {
		return err
	}
	if key == "" {
		return errors.New("no environment key found")
	}
	options, err := env.GetOptions()
	if err != nil {
		return fmt.Errorf("%s is invalid: %s", key, err)
	}

	if options.Title == nil || *options.Title == "" {
		return fmt.Errorf("%s is invalid: missing title", key)
	}

	if len(options.ValueOptions) > 0 && value == "" {
		return fmt.Errorf("%s is invalid: has value_options, but missing default value", key)
	}

	return nil
}

func stepSource(sourcePtr *models.StepSourceModel) error {
	if sourcePtr == nil {
		return errors.New("source not specified")
	}
	source := *sourcePtr

	if source.Git == "" {
		return errors.New("missing source.Git")
	}

	if !strings.HasPrefix(source.Git, "http://") && !strings.HasPrefix(source.Git, "https://") {
		return errors.New("invalid source.Git: url scheme should be: http:// or https://")
	}
	if !strings.HasSuffix(source.Git, ".git") {
		return errors.New("invalid source.Git: missing .git suffix")
	}

	if source.Commit == "" {
		return errors.New("missing source.Commit")
	}
	return nil
}

func stepSummary(summaryPtr *string) error {
	if summaryPtr == nil || *summaryPtr == "" {
		return errors.New("summary not specified")
	}
	summary := *summaryPtr

	if strings.Contains(summary, "\n") {
		return errors.New("summary should be one line")
	}
	if utf8.RuneCountInString(summary) > maxSummaryLength {
		return fmt.Errorf("summary should contain maximum (%d) characters, actual: (%d)", maxSummaryLength, utf8.RuneCountInString(summary))
	}
	return nil
}

// Step ...
func Step(step models.StepModel, validatePublish bool) error {
	if step.Title == nil || *step.Title == "" {
		return errors.New("title not specified")
	}
	if err := stepSummary(step.Summary); err != nil {
		return err
	}
	if step.Website == nil || *step.Website == "" {
		return errors.New("website not specified")
	}

	if step.Timeout != nil && *step.Timeout < 0 {
		return errors.New("timeout is less then 0")
	}

	if validatePublish {
		if step.PublishedAt == nil || (*step.PublishedAt).Equal(time.Time{}) {
			return errors.New("publishedAt not specified")
		}
		if err := stepSource(step.Source); err != nil {
			return err
		}
	}

	for _, input := range step.Inputs {
		if err := inputOutput(input); err != nil {
			return err
		}
	}

	for _, output := range step.Outputs {
		if err := inputOutput(output); err != nil {
			return err
		}
	}

	return nil
}
