// This consumer is built outside cmd to check the exported gateway API. Its
// actions intentionally avoid starting a server or touching configuration.
package main

import (
	"context"
	"fmt"

	otterio "github.com/soulteary/otterio/cmd"
	"github.com/urfave/cli/v3"
)

var _ func(context.Context, *cli.Command, otterio.Gateway) error = otterio.StartGateway

func main() {
	if otterio.GlobalFlags()[0] == otterio.GlobalFlags()[0] || otterio.ServerFlags()[0] == otterio.ServerFlags()[0] {
		panic("flag factories shared state")
	}
	values := []string{}
	if err := otterio.RegisterGatewayCommand(func() *cli.Command {
		return &cli.Command{
			Name:  "external-api",
			Flags: []cli.Flag{&cli.StringFlag{Name: "backend", Aliases: []string{"b"}, Value: "fresh", Local: true}},
			Action: func(_ context.Context, command *cli.Command) error {
				values = append(values, command.String("backend"))
				return nil
			},
		}
	}); err != nil {
		panic(err)
	}
	otterio.Main([]string{"consumer", "gateway", "external-api", "-b", "first"})
	otterio.Main([]string{"consumer", "gateway", "external-api"})
	if len(values) != 2 || values[0] != "first" || values[1] != "fresh" {
		panic(fmt.Sprintf("command state leaked: %q", values))
	}
	fmt.Println("gateway API and fresh flags verified")
}
