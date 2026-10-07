/*
 * MinIO Cloud Storage, (C) 2015-2019 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/soulteary/otterio/internal/clisupport"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/soulteary/otterio/pkg/trie"
	"github.com/soulteary/otterio/pkg/words"
	"github.com/urfave/cli/v3"
)

// GlobalFlags returns fresh global flag definitions for each command.
func GlobalFlags() []cli.Flag {
	return []cli.Flag{
		// Deprecated flag, so its hidden now - existing deployments will keep working.
		&cli.StringFlag{
			Local:   true,
			Name:    "config-dir",
			Aliases: []string{"C"},
			Value:   defaultConfigDir.Get(),
			Usage:   "[DEPRECATED] path to legacy configuration directory",
			Hidden:  true,
		},
		&cli.StringFlag{
			Local:   true,
			Name:    "certs-dir",
			Aliases: []string{"S"},
			Value:   defaultCertsDir.Get(),
			Usage:   "path to certs directory",
		},
		&cli.BoolFlag{
			Local: true,
			Name:  "quiet",
			Usage: "disable startup information",
		},
		&cli.BoolFlag{
			Local: true,
			Name:  "anonymous",
			Usage: "hide sensitive information from logging",
		},
		&cli.BoolFlag{
			Local: true,
			Name:  "json",
			Usage: "output server logs and startup information in json format",
		},
		// Deprecated flag, so its hidden now, existing deployments will keep working.
		&cli.BoolFlag{
			Local:  true,
			Name:   "compat",
			Usage:  "enable strict S3 compatibility by turning off certain performance optimizations",
			Hidden: true,
		},
		// This flag is hidden and to be used only during certain performance testing.
		&cli.BoolFlag{
			Local:  true,
			Name:   "no-compat",
			Usage:  "disable strict S3 compatibility by turning on certain performance optimizations",
			Hidden: true,
		},
	}
}

// Help template for otterio.
var otterioHelpTemplate = `NAME:
  {{.Name}} - {{.Usage}}

DESCRIPTION:
  {{.Description}}

USAGE:
  {{.HelpName}} {{if .VisibleFlags}}[FLAGS] {{end}}COMMAND{{if .VisibleFlags}}{{end}} [ARGS...]

COMMANDS:
  {{range .VisibleCommands}}{{join .Names ", "}}{{ "\t" }}{{.Usage}}
  {{end}}{{if .VisibleFlags}}
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}
VERSION:
  {{.Version}}
`

func newApp(name string) *cli.Command {
	// Collection of otterio commands currently supported are.
	commands := []*cli.Command{}

	// Collection of otterio commands currently supported in a trie tree.
	commandsTree := trie.NewTrie()

	// registerCommand registers a cli command.
	registerCommand := func(command *cli.Command) {
		commands = append(commands, command)
		commandsTree.Insert(command.Name)
	}

	findClosestCommands := func(command string) []string {
		var closestCommands []string
		closestCommands = append(closestCommands, commandsTree.PrefixMatch(command)...)

		sort.Strings(closestCommands)
		// Suggest other close commands - allow missed, wrongly added and
		// even transposed characters
		for _, value := range commandsTree.Walk(commandsTree.Root()) {
			if sort.SearchStrings(closestCommands, value) < len(closestCommands) {
				continue
			}
			// 2 is arbitrary and represents the max
			// allowed number of typed errors
			if words.DamerauLevenshteinDistance(command, value) < 2 {
				closestCommands = append(closestCommands, value)
			}
		}

		return closestCommands
	}

	// Register all commands.
	registerCommand(newServerCommand())
	registerCommand(newGatewayCommand())

	// Set up an isolated command tree for every invocation.
	clisupport.Install()
	app := &cli.Command{}
	stopAfterFirstArgument := 1
	app.StopOnNthArg = &stopAfterFirstArgument
	app.Name = name
	app.Authors = []any{"MinIO, Inc."}
	app.Version = ReleaseTag
	app.Usage = "High Performance Object Storage"
	app.Description = `Build high performance data infrastructure for machine learning, analytics and application data workloads with OtterIO`
	app.Flags = GlobalFlags()
	app.HideHelpCommand = true // Hide `help, h` command, we already have `otterio --help`.
	app.Commands = commands
	app.CustomRootCommandHelpTemplate = otterioHelpTemplate
	app.CommandNotFound = func(_ context.Context, _ *cli.Command, command string) {
		console.Printf("‘%s’ is not a otterio sub-command. See ‘otterio --help’.\n", command)
		closestCommands := findClosestCommands(command)
		if len(closestCommands) > 0 {
			console.Println()
			console.Println("Did you mean one of these?")
			for _, cmd := range closestCommands {
				console.Printf("\t‘%s’\n", cmd)
			}
		}

		os.Exit(1)
	}

	configureCommandTree(app)
	return app
}

// Main main for otterio server.
func Main(args []string) {
	// Set the otterio app name.
	appName := filepath.Base(args[0])

	// Run the app - exit on error.
	if err := clisupport.Run(context.Background(), newApp(appName), args); err != nil {
		var exit cli.ExitCoder
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		os.Exit(1)
	}
}
