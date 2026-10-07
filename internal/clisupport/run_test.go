package clisupport

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestRunArgumentGrammar(t *testing.T) {
	Install()
	for _, test := range []struct {
		name, usage string
		args, want  []string
		address     string
		help        bool
	}{
		{name: "unknown root stops", args: []string{"unknown", "--help"}, want: []string{"unknown", "--help"}},
		{name: "root help stops after positional", args: []string{"--help", "unknown", "--unknown"}, help: true},
		{name: "group stops", args: []string{"group", "unknown", "--help"}, want: []string{"unknown", "--help"}},
		{name: "numeric root", args: []string{"--help", "-1"}, usage: "flag provided but not defined: -1"},
		{name: "numeric leaf", args: []string{"leaf", "disk", "--help", "-1"}, usage: "flag provided but not defined: -1"},
		{name: "first invalid option wins", args: []string{"leaf", "--unknown", "-1"}, usage: "flag provided but not defined: -unknown"},
		{name: "numeric invalid option wins", args: []string{"leaf", "-1", "--unknown"}, usage: "flag provided but not defined: -1"},
		{name: "negative flag value", args: []string{"leaf", "--address", "-1"}, want: []string{}, address: "-1"},
		{name: "negative inline value", args: []string{"leaf", "--address=-1"}, want: []string{}, address: "-1"},
		{name: "leaf delimiter", args: []string{"leaf", "--", "-1", "--help"}, want: []string{"-1", "--help"}},
		{name: "root delimiter then child parsing", args: []string{"--", "leaf", "--help", "-1"}, usage: "flag provided but not defined: -1"},
		{name: "group delimiter then child parsing", args: []string{"group", "--", "child", "--help", "-1"}, usage: "flag provided but not defined: -1"},
		{name: "quoted root leading whitespace", args: []string{" --help"}, want: []string{" --help"}},
		{name: "quoted root empty positional", args: []string{"", "--help"}, want: []string{"", "--help"}},
		{name: "quoted leaf leading whitespace", args: []string{"leaf", " --help", ""}, want: []string{" --help", ""}},
		{name: "quoted leaf positional before help", args: []string{"leaf", " --help", "--help"}, help: true},
		{name: "quoted trailing whitespace in flag", args: []string{"leaf", "--help "}, usage: "flag provided but not defined: -help "},
		{name: "whitespace value preserved", args: []string{"leaf", "--address", " -1 "}, want: []string{}, address: " -1 "},
		{name: "bad triple dash", args: []string{"leaf", "---x"}, usage: "bad flag syntax: ---x"},
		{name: "bad single dash equal", args: []string{"leaf", "-=x"}, usage: "bad flag syntax: -=x"},
		{name: "bad double dash equal", args: []string{"leaf", "--=x"}, usage: "bad flag syntax: --=x"},
		{name: "empty bool", args: []string{"leaf", "--help="}, usage: `invalid boolean value "" for -help: parse error`},
		{name: "empty bool alias fails without being visited", args: []string{"leaf", "--help", "-h="}, usage: `invalid boolean value "" for -h: parse error`},
		{name: "empty string is valid", args: []string{"leaf", "--address="}, want: []string{}},
		{name: "final flag consumes earlier positional", args: []string{"leaf", "earlier", "remaining", "--address"}, want: []string{"remaining"}, address: "earlier"},
		{name: "alias error precedes lexical error", args: []string{"leaf", "--help", "-h", "-1"}, usage: "Cannot use two forms of the same flag: h help"},
		{name: "generated token collision", args: []string{"leaf", "--__otterio_lexical_0"}, usage: "flag provided but not defined: -__otterio_lexical_0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output, errorOutput bytes.Buffer
			var received []string
			address, called := "", false
			action := func(_ context.Context, cmd *cli.Command) error {
				called = true
				received = cmd.Args().Slice()
				address = cmd.String("address")
				return nil
			}
			leaf := func(name string) *cli.Command {
				cmd := &cli.Command{Name: name, Action: action, Flags: []cli.Flag{&cli.StringFlag{Name: "address", Local: true}}}
				Configure(cmd)
				return cmd
			}
			group := &cli.Command{Name: "group", Commands: []*cli.Command{leaf("child")}, Action: action}
			Configure(group)
			root := &cli.Command{Name: "test", Commands: []*cli.Command{leaf("leaf"), group}, Action: action, Writer: &output, ErrWriter: &errorOutput}
			Configure(root)
			args := append([]string{"test"}, test.args...)
			original := append([]string(nil), args...)
			err := Run(context.Background(), root, args)
			if !reflect.DeepEqual(args, original) {
				t.Fatal("runner changed the caller's argument slice")
			}
			if test.usage != "" {
				if err == nil || !strings.Contains(output.String(), test.usage) || called {
					t.Fatalf("usage: err=%v stdout=%q called=%v", err, output.String(), called)
				}
			} else if test.help {
				var exit cli.ExitCoder
				if !errors.As(err, &exit) || exit.ExitCode() != 0 || output.Len() == 0 || called {
					t.Fatalf("help: err=%v stdout=%q called=%v", err, output.String(), called)
				}
			} else if err != nil || !called || !reflect.DeepEqual(received, test.want) || address != test.address {
				t.Fatalf("action: err=%v args=%#v address=%q called=%v", err, received, address, called)
			}
			if errorOutput.Len() != 0 {
				t.Fatalf("unexpected stderr: %q", errorOutput.String())
			}
		})
	}
}

func TestRunNativeListAndPersistentFlags(t *testing.T) {
	Install()
	called := false
	child := &cli.Command{Name: "child", Flags: []cli.Flag{
		&cli.IntSliceFlag{Name: "code", Config: cli.IntegerConfig{Base: 10}, Local: true},
		&cli.StringFlag{Name: "1", Local: true},
	}, Action: func(_ context.Context, cmd *cli.Command) error {
		called = true
		if !reflect.DeepEqual(cmd.IntSlice("code"), []int{-1, 2, 3}) || cmd.String("persistent") != "-1" || cmd.String("1") != "value" {
			t.Fatalf("native flag values: codes=%v persistent=%q numeric=%q", cmd.IntSlice("code"), cmd.String("persistent"), cmd.String("1"))
		}
		return nil
	}}
	Configure(child)
	root := &cli.Command{Name: "test", Commands: []*cli.Command{child}, Flags: []cli.Flag{&cli.StringFlag{Name: "persistent"}}}
	Configure(root)
	// Lists retain the upstream concrete type so its separator configuration
	// applies. Numeric tokens serving as values must never become options.
	err := Run(context.Background(), root, []string{"test", "child", "--persistent", "-1", "--code", "-1", "--code", "2", "--code", "3", "-1", "value"})
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestRunExplicitStopAndSkipParsing(t *testing.T) {
	Install()
	for _, skip := range []bool{false, true} {
		stop := 0
		called := false
		root := &cli.Command{Name: "test", StopOnNthArg: &stop, SkipFlagParsing: skip, Action: func(_ context.Context, cmd *cli.Command) error {
			called = true
			if !reflect.DeepEqual(cmd.Args().Slice(), []string{" --help", "-1", "--help"}) {
				t.Fatalf("args=%#v", cmd.Args().Slice())
			}
			return nil
		}}
		Configure(root)
		if err := Run(context.Background(), root, []string{"test", " --help", "-1", "--help"}); err != nil || !called {
			t.Fatalf("skip=%v err=%v called=%v", skip, err, called)
		}
	}
}

type undocumentedBoolFlag struct{ cli.Flag }

func (*undocumentedBoolFlag) IsBoolFlag() bool { return true }

func TestRunCustomNativeBooleanFlag(t *testing.T) {
	Install()
	for _, native := range []bool{true, false} {
		called := false
		root := &cli.Command{Name: "test", Flags: []cli.Flag{&undocumentedBoolFlag{Flag: &cli.BoolFlag{Name: "toggle"}}}, Action: func(_ context.Context, cmd *cli.Command) error {
			called = true
			if !cmd.Bool("toggle") || !reflect.DeepEqual(cmd.Args().Slice(), []string{"value"}) {
				t.Fatalf("native=%v toggle=%v args=%#v", native, cmd.Bool("toggle"), cmd.Args().Slice())
			}
			return nil
		}}
		Configure(root)
		args := []string{"test", "--toggle", "value"}
		var err error
		if native {
			err = root.Run(context.Background(), args)
		} else {
			err = Run(context.Background(), root, args)
		}
		if err != nil || !called {
			t.Fatalf("native=%v err=%v called=%v", native, err, called)
		}
	}
}
