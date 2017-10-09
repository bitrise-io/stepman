package validate

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	envmanModels "github.com/bitrise-io/envman/models"
	"github.com/bitrise-io/go-utils/command/git"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/stepman/models"
)

const maxSummaryLength = 100

func inputOutput(env envmanModels.EnvironmentItemModel, isInput bool) error {
	key, value, err := env.GetKeyValuePair()
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("no environment key found for: %v", env)
	}
	options, err := env.GetOptions()
	if err != nil {
		return fmt.Errorf("%s is invalid: %s", key, err)
	}

	if options.Title == nil || *options.Title == "" {
		return fmt.Errorf("%s is invalid: missing title", key)
	}

	if isInput {
		if len(options.ValueOptions) > 0 && value == "" {
			return fmt.Errorf("%s is invalid: has value_options, but missing default value", key)
		}
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

// StepDefinition ...
func StepDefinition(step models.StepModel) error {
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

	for _, input := range step.Inputs {
		if err := inputOutput(input, true); err != nil {
			return err
		}
	}

	for _, output := range step.Outputs {
		if err := inputOutput(output, false); err != nil {
			return err
		}
	}

	return nil
}

// StepDefinitionPublishParams ...
func StepDefinitionPublishParams(step models.StepModel, id, version string) error {
	if step.PublishedAt == nil || (*step.PublishedAt).Equal(time.Time{}) {
		return errors.New("publishedAt not specified")
	}
	if err := stepSource(step.Source); err != nil {
		return err
	}

	pth, err := pathutil.NormalizedOSTempDirPath(id + "-" + version)
	if err != nil {
		return fmt.Errorf("failed to create a temporary directory for the step's audit, error: %s", err)
	}

	err = retry.Times(2).Wait(3 * time.Second).Try(func(attempt uint) error {
		return git.CloneTagOrBranchCommand(step.Source.Git, pth, version).Run()
	})
	if err != nil {
		return fmt.Errorf("failed to git-clone the step (url: %s) version (%s), error: %s", step.Source.Git, version, err)
	}

	latestCommit, err := git.GetCommitHashOfHead(pth)
	if err != nil {
		return fmt.Errorf("failed to get git-latest-commit-hash, error: %s", err)
	}
	if latestCommit != step.Source.Commit {
		return fmt.Errorf("step source commit hash (%s) should be the commit hash (%s) of git tag", step.Source.Commit, latestCommit)
	}

	return nil
}
