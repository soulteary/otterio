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

type scopedAliasFlag[T any, C any, V cli.ValueCreator[T, C]] struct {
	*cli.FlagBase[T, C, V]
	command *cli.Command
	used    aliasUsage
}

func (flag *scopedAliasFlag[T, C, V]) usedAliases() aliasUsage { return flag.used }

func (flag *scopedAliasFlag[T, C, V]) PreParse() error {
	flag.used = make(aliasUsage)
	return flag.FlagBase.PreParse()
}

func (flag *scopedAliasFlag[T, C, V]) Set(name, value string) error {
	if err := flag.FlagBase.Set(name, value); err != nil {
		return err
	}
	flag.used[name] = true
	return nil
}

func (flag *scopedAliasFlag[T, C, V]) PostParse() error {
	if err := checkAliases(flag.command); err != nil {
		return err
	}
	return flag.FlagBase.PostParse()
}

func trackAliases[T any, C any, V cli.ValueCreator[T, C]](flag *cli.FlagBase[T, C, V], command *cli.Command) cli.Flag {
	return &scopedAliasFlag[T, C, V]{FlagBase: flag, command: command}
}

func configureAliases(flag cli.Flag, command *cli.Command) cli.Flag {
	if len(flag.Names()) < 2 {
		return flag
	}
	// Embed the native base so actions, validators, destinations, visibility
	// and upstream private methods retain their original package identity.
	// In particular the native list separator setter must remain available.
	switch value := flag.(type) {
	case *cli.BoolFlag:
		return trackAliases(value, command)
	case *cli.StringFlag:
		return trackAliases(value, command)
	case *cli.IntFlag:
		return trackAliases(value, command)
	case *cli.Int8Flag:
		return trackAliases(value, command)
	case *cli.Int16Flag:
		return trackAliases(value, command)
	case *cli.Int32Flag:
		return trackAliases(value, command)
	case *cli.Int64Flag:
		return trackAliases(value, command)
	case *cli.UintFlag:
		return trackAliases(value, command)
	case *cli.Uint8Flag:
		return trackAliases(value, command)
	case *cli.Uint16Flag:
		return trackAliases(value, command)
	case *cli.Uint32Flag:
		return trackAliases(value, command)
	case *cli.Uint64Flag:
		return trackAliases(value, command)
	case *cli.Float32Flag:
		return trackAliases(value, command)
	case *cli.Float64Flag:
		return trackAliases(value, command)
	case *cli.DurationFlag:
		return trackAliases(value, command)
	case *cli.TimestampFlag:
		return trackAliases(value, command)
	case *cli.GenericFlag:
		return trackAliases(value, command)
	case *cli.TextFlag:
		return trackAliases(value, command)
	case *cli.StringSliceFlag:
		return trackAliases(value, command)
	case *cli.IntSliceFlag:
		return trackAliases(value, command)
	case *cli.Int64SliceFlag:
		return trackAliases(value, command)
	default:
		return flag
	}
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
