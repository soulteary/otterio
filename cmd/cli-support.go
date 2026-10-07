package cmd

import (
	"github.com/soulteary/otterio/internal/clisupport"
	"github.com/urfave/cli/v3"
)

func configureCommandTree(command *cli.Command) {
	clisupport.Configure(command)
	for _, child := range command.Commands {
		child.CommandNotFound = command.CommandNotFound
		configureCommandTree(child)
	}
}

func cliLocalFlag(command *cli.Command, name string) cli.Flag {
	for _, flag := range command.Flags {
		for _, alias := range flag.Names() {
			if alias == name {
				return flag
			}
		}
	}
	return nil
}

func cliAncestorString(command *cli.Command, name string, skip int) string {
	for _, ancestor := range command.Lineage()[min(skip, len(command.Lineage())):] {
		if flag := cliLocalFlag(ancestor, name); flag != nil {
			value, _ := flag.Get().(string)
			return value
		}
	}
	return ""
}

func cliParentString(command *cli.Command, name string) string {
	return cliAncestorString(command, name, 1)
}

func cliAncestorIsSet(command *cli.Command, name string, skip int) bool {
	for _, ancestor := range command.Lineage()[min(skip, len(command.Lineage())):] {
		if flag := cliLocalFlag(ancestor, name); flag != nil && flag.IsSet() {
			return true
		}
	}
	return false
}

func cliParentIsSet(command *cli.Command, name string) bool {
	return cliAncestorIsSet(command, name, 1)
}

// GatewayServerAddress preserves the gateway parent's non-default address
// precedence. It is shared with gateway implementations before startup.
func GatewayServerAddress(command *cli.Command) string {
	value := cliParentString(command, "address")
	if value == "" || value == ":"+GlobalOtterioDefaultPort {
		value = command.String("address")
	}
	return value
}
