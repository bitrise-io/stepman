package cli

import (
	"fmt"
	"os"
	"path"

	log "github.com/Sirupsen/logrus"
	"github.com/bitrise-io/stepman/stepman"
	"github.com/bitrise-io/stepman/version"
	"github.com/codegangsta/cli"
)

func initLogFormatter() {
	log.SetFormatter(&log.TextFormatter{
		ForceColors:     true,
		FullTimestamp:   true,
		TimestampFormat: "15:04:05",
	})
}

func before(c *cli.Context) error {
	initLogFormatter()
	initHelpAndVersionFlags()
	initAppHelpTemplate()

	// Log level
	if logLevel, err := log.ParseLevel(c.String(LogLevelKey)); err != nil {
		log.Fatal("[BITRISE_CLI] - Failed to parse log level:", err)
	} else {
		log.SetLevel(logLevel)
	}

	// Setup
	err := stepman.CreateStepManDirIfNeeded()
	if err != nil {
		return err
	}

	// Debug mode
	stepman.DebugMode = c.Bool(DebugKey)
	return nil
}

func printVersion(c *cli.Context) {
	fmt.Fprintf(c.App.Writer, "%v\n", c.App.Version)
}

// Run ...
func Run() {
	cli.VersionPrinter = printVersion

	app := cli.NewApp()
	app.Name = path.Base(os.Args[0])
	app.Usage = "Step manager"
	app.Version = version.VERSION

	app.Author = ""
	app.Email = ""

	app.Before = before

	app.Flags = flags
	app.Commands = commands

	if err := app.Run(os.Args); err != nil {
		log.Error("[STEPMAN] - Stepman finished:", err)
	}
}
