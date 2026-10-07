package clisupport

import (
	"fmt"

	"github.com/urfave/cli/v3"
)

// aliasUsage tracks distinct spellings at one command scope. Repeating one
// spelling is valid; mixing a canonical name and alias retains the legacy
// diagnostic even when both values are equal.
type aliasUsage map[string]bool

type aliasTracker interface {
	usedAliases() aliasUsage
}

type scopedStringFlag struct {
	*cli.StringFlag
	command *cli.Command
	used    aliasUsage
}

func (flag *scopedStringFlag) usedAliases() aliasUsage { return flag.used }

func (flag *scopedStringFlag) PreParse() error {
	flag.used = make(aliasUsage)
	return flag.StringFlag.PreParse()
}

func (flag *scopedStringFlag) Set(name, value string) error {
	if err := flag.StringFlag.Set(name, value); err != nil {
		return err
	}
	flag.used[name] = true
	return nil
}

func (flag *scopedStringFlag) PostParse() error {
	if err := checkAliases(flag.command); err != nil {
		return err
	}
	return flag.StringFlag.PostParse()
}

func checkAliases(command *cli.Command) error {
	for _, flag := range command.Flags {
		tracker, ok := flag.(aliasTracker)
		if !ok {
			continue
		}
		first := ""
		for _, name := range flag.Names() {
			if !tracker.usedAliases()[name] {
				continue
			}
			if first != "" {
				err := fmt.Errorf("Cannot use two forms of the same flag: %s %s", name, first)
				fmt.Fprintln(command.Root().Writer, err)
				if len(command.Lineage()) > 1 {
					fmt.Fprintln(command.Root().Writer)
				}
				return err
			}
			first = name
		}
	}
	return nil
}
