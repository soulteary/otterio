// This consumer is built outside cmd to check the exported gateway API. Its
// actions intentionally avoid starting a server or touching configuration.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"

	otterio "github.com/soulteary/otterio/cmd"
	"github.com/urfave/cli/v3"
)

var _ func(context.Context, *cli.Command, otterio.Gateway) error = otterio.StartGateway

type state struct {
	backend string
	toggle  bool
	number  int
	labels  []string
	codes   []int
	ids     []int64
	decimal []int
	wide    []int64
}

func main() {
	if otterio.GlobalFlags()[0] == otterio.GlobalFlags()[0] || otterio.ServerFlags()[0] == otterio.ServerFlags()[0] {
		panic("flag factories shared state")
	}
	probe := len(os.Args) > 1 && os.Args[1] == "probe"
	values := []state{}
	allocated := make(map[cli.Flag]bool)
	if err := otterio.RegisterGatewayCommand(func() *cli.Command {
		flags := []cli.Flag{
			&cli.StringFlag{Name: "backend", Aliases: []string{"b"}, Value: "fresh", Local: true},
			&cli.BoolFlag{Name: "toggle", Aliases: []string{"t"}, Local: true},
			&cli.IntFlag{Name: "number", Aliases: []string{"n"}, Value: 7, Local: true},
			&cli.UintFlag{Name: "count", Aliases: []string{"u"}, Local: true},
			&cli.Float64Flag{Name: "ratio", Aliases: []string{"r"}, Local: true},
			&cli.DurationFlag{Name: "timeout", Aliases: []string{"d"}, Local: true},
			&cli.StringSliceFlag{Name: "labels", Aliases: []string{"l"}, Local: true},
			&cli.IntSliceFlag{Name: "codes", Aliases: []string{"k"}, Config: cli.IntegerConfig{Base: 10}, Local: true},
			&cli.Int64SliceFlag{Name: "ids", Aliases: []string{"i"}, Config: cli.IntegerConfig{Base: 10}, Local: true},
			&cli.IntSliceFlag{Name: "decimal", Config: cli.IntegerConfig{Base: 10}, Local: true},
			&cli.Int64SliceFlag{Name: "wide", Config: cli.IntegerConfig{Base: 10}, Local: true},
		}
		for _, flag := range flags {
			if allocated[flag] {
				panic("gateway factory reused a native flag")
			}
			allocated[flag] = true
		}
		return &cli.Command{
			Name:  "external-api",
			Flags: flags,
			Action: func(_ context.Context, command *cli.Command) error {
				values = append(values, state{command.String("backend"), command.Bool("toggle"), command.Int("number"), command.StringSlice("labels"), command.IntSlice("codes"), command.Int64Slice("ids"), command.IntSlice("decimal"), command.Int64Slice("wide")})
				if probe {
					fmt.Println("action")
				}
				return nil
			},
		}
	}); err != nil {
		panic(err)
	}
	if probe {
		otterio.Main(append([]string{"consumer", "gateway", "external-api"}, os.Args[2:]...))
		return
	}
	otterio.Main([]string{"consumer", "gateway", "external-api", "-b", "first", "--toggle", "--number", "2", "--labels", "a,b", "--labels", "c", "--codes", "-1", "--codes", "2", "--ids", "1", "--ids", "2", "--decimal", "010", "--wide", "010"})
	otterio.Main([]string{"consumer", "gateway", "external-api"})
	want := []state{{"first", true, 2, []string{"a,b", "c"}, []int{-1, 2}, []int64{1, 2}, []int{10}, []int64{10}}, {backend: "fresh", number: 7, labels: []string{}, codes: []int{}, ids: []int64{}, decimal: []int{}, wide: []int64{}}}
	if !reflect.DeepEqual(values, want) {
		panic(fmt.Sprintf("command state or native list parsing changed: got %#v, want %#v", values, want))
	}
	checkAliasProcesses()
	fmt.Println("gateway API and fresh flags verified")
}

func checkAliasProcesses() {
	executable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	for _, flag := range []struct{ name, alias, value string }{
		{"backend", "b", "text"}, {"toggle", "t", "false"}, {"number", "n", "1"},
		{"count", "u", "1"}, {"ratio", "r", "1.5"}, {"timeout", "d", "1s"},
		{"labels", "l", "text"}, {"codes", "k", "1"}, {"ids", "i", "1"},
	} {
		for _, unknown := range []bool{false, true} {
			args := []string{"--" + flag.name + "=" + flag.value, "-" + flag.alias + "=" + flag.value}
			if unknown {
				args = append(args, "--unknown")
			}
			code, stdout := runProbe(executable, args)
			want := "Cannot use two forms of the same flag: " + flag.alias + " " + flag.name + "\n\n"
			if code != 1 || stdout != want {
				panic(fmt.Sprintf("alias diagnostic: args=%q code=%d stdout=%q want=%q", args, code, stdout, want))
			}
		}
		args := []string{"--" + flag.name + "=" + flag.value, "--" + flag.name + "=" + flag.value}
		if code, stdout := runProbe(executable, args); code != 0 || stdout != "action\n" {
			panic(fmt.Sprintf("same spelling rejected: args=%q code=%d stdout=%q", args, code, stdout))
		}
	}
	if code, stdout := runProbe(executable, []string{"--toggle", "-t=invalid"}); code != 1 || !strings.Contains(stdout, `invalid boolean value "invalid" for -t: parse error`) || strings.Contains(stdout, "Cannot use two forms") {
		panic(fmt.Sprintf("failed Set was counted as an alias: code=%d stdout=%q", code, stdout))
	}
	for _, name := range []string{"decimal", "wide", "codes", "ids"} {
		if code, stdout := runProbe(executable, []string{"--" + name, "0x10"}); code != 1 || !strings.Contains(stdout, `invalid value "0x10"`) || strings.Contains(stdout, "action") {
			panic(fmt.Sprintf("decimal list accepted hexadecimal input: flag=%s code=%d stdout=%q", name, code, stdout))
		}
	}
}

func runProbe(executable string, args []string) (int, string) {
	command := exec.Command(executable, append([]string{"probe"}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	code := 0
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			panic(err)
		}
	}
	if stderr.Len() != 0 {
		panic(fmt.Sprintf("diagnostic reached stderr: %q", stderr.String()))
	}
	return code, stdout.String()
}
