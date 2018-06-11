package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/stepman/stepman"
	"github.com/bitrise-io/stepman/stringbuilder"
	"github.com/urfave/cli"
)

const (
	// ShareFilename ...
	ShareFilename string = "share.json"
)

// ShareModel ...
type ShareModel struct {
	Collection string
	StepID     string
	StepTag    string
}

// ShareBranchName ...
func (share ShareModel) ShareBranchName() string {
	return share.StepID + "-" + share.StepTag
}

// DeleteShareSteplibFile ...
func DeleteShareSteplibFile() error {
	return command.RemoveDir(getShareFilePath())
}

// ReadShareSteplibFromFile ...
func ReadShareSteplibFromFile() (ShareModel, error) {
	if exist, err := pathutil.IsPathExists(getShareFilePath()); err != nil {
		return ShareModel{}, err
	} else if !exist {
		return ShareModel{}, errors.New("No share steplib found")
	}

	bytes, err := fileutil.ReadBytesFromFile(getShareFilePath())
	if err != nil {
		return ShareModel{}, err
	}

	share := ShareModel{}
	if err := json.Unmarshal(bytes, &share); err != nil {
		return ShareModel{}, err
	}

	return share, nil
}

// WriteShareSteplibToFile ...
func WriteShareSteplibToFile(share ShareModel) error {
	var bytes []byte
	bytes, err := json.MarshalIndent(share, "", "\t")
	if err != nil {
		log.Errorf("Failed to parse json, error: %s", err)
		return err
	}

	return fileutil.WriteBytesToFile(getShareFilePath(), bytes)
}

// GuideTextForStepAudit ...
func GuideTextForStepAudit(toolMode bool) string {
	name := "stepman"
	if toolMode {
		name = "bitrise"
	}

	b := stringbuilder.New()
	b.Add("First, you need to ensure that your step is stored in a ").AddBlue("public git repository ").Add("and it matches our requirements.")
	b.AddLn("To audit your step on your local machine call ").AddBlue("$ %s audit --step-yml path/to/your/step.yml", name)
	return b.String()
}

// GuideTextForStart ...
func GuideTextForStart() string {
	b := stringbuilder.New()
	b.AddBlue("Fork the StepLib repository ").Add("you want to share your Step in.")
	b.AddLn(`You can find the main ("official") StepLib repository at `).AddGreen("https://github.com/bitrise-io/bitrise-steplib")
	return b.String()
}

// GuideTextForShareStart ...
func GuideTextForShareStart(toolMode bool) string {
	name := "stepman"
	if toolMode {
		name = "bitrise"
	}

	b := stringbuilder.New()
	b.Add("Call ").AddBlue("$ %s share start -c https://github.com/[your-username]/bitrise-steplib.git", name).Add(", with the git clone URL of your forked StepLib repository.")
	b.AddLn("This will prepare your forked StepLib locally for sharing.")
	return b.String()
}

// GuideTextForShareCreate ...
func GuideTextForShareCreate(toolMode bool) string {
	name := "stepman"
	if toolMode {
		name = "bitrise"
	}

	b := stringbuilder.New()
	b.Add("Next, call ").AddBlue("$ %s share create --tag [step-version-tag] --git [step-git-uri] --stepid [step-id]", name).Add(",")
	b.AddLn("to add your Step to your forked StepLib repository (locally).")
	b.AddNewLine()
	b.AddYellowLn("Important: ").Add("you have to add the (version) tag to your Step's repository.")
	b.AddNewLine()
	b.AddLn("An example call ").AddGreen("$ %s share create --tag 1.0.0 --git https://github.com/[your-username]/my-awesome-step.git --stepid my-awesome-step", name)
	return b.String()
}

// GuideTextForAudit ...
func GuideTextForAudit(toolMode bool) string {
	name := "stepman"
	if toolMode {
		name = "bitrise"
	}

	b := stringbuilder.New()
	b.Add("You can call ").AddBlue("$ %s audit -c https://github.com/[your-username]/bitrise-steplib.git ", name)
	b.AddLn("to perform a complete health-check on your forked StepLib before submitting your Pull Request.")
	b.AddNewLine()
	b.AddLn("This can help you catch issues which might prevent your Step from being accepted.")
	return b.String()
}

// GuideTextForShareFinish ...
func GuideTextForShareFinish(toolMode bool) string {
	name := "stepman"
	if toolMode {
		name = "bitrise"
	}

	b := stringbuilder.New()
	b.Add("Almost done! You should review your Step's step.yml file (the one added to the local StepLib),")
	b.AddLn("and once you're happy with it call ").AddBlue("$ %s share finish", name)
	b.AddNewLine()
	b.Add("This will commit & push the step.yml into your forked StepLib repository.")
	return b.String()
}

// GuideTextForFinish ...
func GuideTextForFinish() string {
	b := stringbuilder.New()
	b.Add("The only remaining thing is to ").AddBlue("create a Pull Request ").Add(" in the original StepLib repository. And you are done!")
	return b.String()
}

func share(c *cli.Context) {
	toolMode := c.Bool(ToolMode)

	b := stringbuilder.New()
	b.AddNewLine()
	b.Add("Do you want to share your own Step with the world? Awesome!")
	b.AddNewLine()
	b.AddLn("Just follow these steps:")
	b.AddNewLine()
	b.AddLn("0. ").Add(GuideTextForStepAudit(toolMode)).AddNewLine()
	b.AddLn("1. ").Add(GuideTextForStart()).AddNewLine()
	b.AddLn("2. ").Add(GuideTextForShareStart(toolMode)).AddNewLine()
	b.AddLn("3. ").Add(GuideTextForShareCreate(toolMode)).AddNewLine()
	b.AddLn("4. ").Add(GuideTextForAudit(toolMode)).AddNewLine()
	b.AddLn("5. ").Add(GuideTextForShareFinish(toolMode)).AddNewLine()
	b.AddLn("6. ").Add(GuideTextForFinish()).AddNewLine()
	b.AddNewLine()
	fmt.Printf(b.String())
}

func getShareFilePath() string {
	return path.Join(stepman.GetStepmanDirPath(), ShareFilename)
}
