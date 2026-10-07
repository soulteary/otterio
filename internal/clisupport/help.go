// Package clisupport contains OtterIO's presentation policy for urfave/cli.
// Parsing and command execution remain owned by the upstream library.
package clisupport

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/urfave/cli/v3"
)

var installOnce sync.Once
var booleanUsageError = regexp.MustCompile(`^invalid value (".*") for flag (-[^:]+): parse error$`)

// Install adds the platform-specific example fields used by OtterIO templates.
func Install() {
	installOnce.Do(func() {
		// Requests are resolved by per-command PostParse flags. Automatic help
		// detection otherwise turns a parsed help flag plus an error into success.
		cli.HelpFlag = nil
		cli.ShowSubcommandHelp = func(command *cli.Command) error {
			if command.CustomHelpTemplate == "" {
				return cli.DefaultShowSubcommandHelp(command)
			}
			cli.HelpPrinter(command.Root().Writer, command.CustomHelpTemplate, command)
			return nil
		}
		cli.HelpPrinterCustom = func(w io.Writer, templ string, data any, funcs map[string]any) {
			if command, ok := data.(*cli.Command); ok {
				prompt, envCommand := "$", "export"
				if runtime.GOOS == "windows" {
					prompt, envCommand = "C:\\>", "set"
				}
				data = struct {
					*cli.Command
					HelpName, Prompt, EnvVarSetCommand, AssignmentOperator string
				}{command, command.FullName(), prompt, envCommand, "="}
			}
			cli.DefaultPrintHelpCustom(w, templ, data, funcs)
		}
	})
}

// Configure preserves the program's help and usage-error presentation. Flag
// objects and built-in flags are local so repeated options keep distinct scope.
func Configure(command *cli.Command) {
	command.DisableSliceFlagSeparator = true
	if len(command.Commands) > 0 && command.StopOnNthArg == nil {
		stopAfterFirstArgument := 1
		command.StopOnNthArg = &stopAfterFirstArgument
	}
	command.OnUsageError = func(_ context.Context, cmd *cli.Command, err error, _ bool) error {
		if aliasError := checkAliases(cmd); aliasError != nil {
			return aliasError
		}
		message := strings.Replace(err.Error(), "flag needs an argument: --", "flag needs an argument: -", 1)
		if parts := booleanUsageError.FindStringSubmatch(message); parts != nil {
			name := strings.TrimLeft(parts[2], "-")
			for _, flag := range cmd.Flags {
				meta, ok := flag.(cli.DocGenerationFlag)
				if !ok || meta.TypeName() != "bool" {
					continue
				}
				for _, alias := range flag.Names() {
					if alias == name {
						message = fmt.Sprintf("invalid boolean value %s for %s: parse error", parts[1], parts[2])
					}
				}
			}
		}
		if len(cmd.Lineage()) > 1 && len(cmd.Commands) == 0 {
			fmt.Fprintln(cmd.Root().Writer, "Incorrect Usage:", message)
			fmt.Fprintln(cmd.Root().Writer)
		} else {
			fmt.Fprintf(cmd.Root().Writer, "Incorrect Usage. %s\n\n", message)
		}
		if message != err.Error() {
			return fmt.Errorf("%s", message)
		}
		return err
	}
	// The executable controls its exit boundary. In particular a returned error
	// must not exit before its owner's cleanup has run.
	command.ExitErrHandler = func(context.Context, *cli.Command, error) {}
	for i, flag := range command.Flags {
		command.Flags[i] = configureAliases(flag, command)
	}
	command.Flags = append(command.Flags, &presentationFlag{
		BoolFlag: &cli.BoolFlag{Name: "help", Aliases: []string{"h"}, Usage: "show help", Local: true},
		command:  command, help: true,
	})
	if command.Version != "" && !command.HideVersion {
		// v3's built-in version flag treats --version=false as a request to
		// print the version. Keep the program's boolean-value behavior and
		// resolve it before any child command is parsed or initialized.
		command.Flags = append(command.Flags, &presentationFlag{
			BoolFlag: &cli.BoolFlag{Name: "version", Aliases: []string{"v"}, Usage: "print the version", Local: true},
			command:  command, version: true,
		})
	}
	for _, flag := range command.Flags {
		if setter, ok := flag.(cli.StringerSetter); ok {
			setter.SetStringer(flagString)
		}
	}
}

type presentationFlag struct {
	*cli.BoolFlag
	command       *cli.Command
	help, version bool
	used          aliasUsage
}

func (flag *presentationFlag) usedAliases() aliasUsage { return flag.used }

func (flag *presentationFlag) PreParse() error {
	flag.used = make(aliasUsage)
	return flag.BoolFlag.PreParse()
}

func (flag *presentationFlag) Set(name, value string) error {
	if err := flag.BoolFlag.Set(name, value); err != nil {
		return err
	}
	flag.used[name] = true
	return nil
}

func (flag *presentationFlag) PostParse() error {
	if err := checkAliases(flag.command); err != nil {
		return err
	}
	if err := flag.BoolFlag.PostParse(); err != nil {
		return err
	}
	if value, _ := flag.Get().(bool); value {
		if flag.help {
			if len(flag.command.Lineage()) == 1 {
				_ = cli.ShowRootCommandHelp(flag.command)
			} else {
				_ = cli.ShowCommandHelp(context.Background(), flag.command.Lineage()[1], flag.command.Name)
			}
		} else if flag.version {
			cli.ShowVersion(flag.command)
		}
		return cli.Exit("", 0)
	}
	return nil
}

func flagString(flag cli.Flag) string {
	meta, ok := flag.(cli.DocGenerationFlag)
	if !ok {
		return strings.Join(flag.Names(), ", ")
	}
	placeholder, usage := "", meta.GetUsage()
	if start := strings.IndexByte(usage, '`'); start >= 0 {
		if end := strings.IndexByte(usage[start+1:], '`'); end >= 0 {
			placeholder = usage[start+1 : start+1+end]
			usage = strings.ReplaceAll(usage, "`", "")
		}
	}
	if meta.TakesValue() && placeholder == "" {
		placeholder = "value"
	}
	names := make([]string, 0, len(flag.Names()))
	for _, name := range flag.Names() {
		prefix := "--"
		if len(name) == 1 {
			prefix = "-"
		}
		name = prefix + name
		if placeholder != "" {
			name += " " + placeholder
		}
		names = append(names, name)
	}
	nameText := strings.Join(names, ", ")
	if meta.TakesValue() {
		if value := meta.GetValue(); value != "" {
			if meta.TypeName() == "string" {
				usage += " (default: " + value + ")"
			} else {
				usage += fmt.Sprintf(" (default: %s)", value)
			}
		}
	}
	if envs := meta.GetEnvVars(); len(envs) != 0 {
		prefix, suffix, separator := "$", "", ", $"
		if runtime.GOOS == "windows" {
			prefix, suffix, separator = "%", "%", "%, %"
		}
		usage += " [" + prefix + strings.Join(envs, separator) + suffix + "]"
	}
	return nameText + "\t" + usage
}
