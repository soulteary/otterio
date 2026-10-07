package cmd

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/soulteary/otterio/cmd/config"
	"github.com/soulteary/otterio/internal/clisupport"
	"github.com/urfave/cli/v3"
)

func TestCLICommandStateIsolation(t *testing.T) {
	for _, value := range []string{":19001", ":19002"} {
		app := newApp("otterio")
		app.Commands[0].Action = func(_ context.Context, command *cli.Command) error {
			if command.String("address") != value {
				t.Fatalf("address: got %q, want %q", command.String("address"), value)
			}
			return nil
		}
		if err := clisupport.Run(context.Background(), app, []string{"otterio", "server", "--address", value, "disk"}); err != nil {
			t.Fatal(err)
		}
	}
	app := newApp("otterio")
	app.Commands[0].Action = func(_ context.Context, command *cli.Command) error {
		if command.IsSet("address") || command.String("address") != ":"+GlobalOtterioDefaultPort {
			t.Fatalf("a previous command leaked address state")
		}
		return nil
	}
	if err := clisupport.Run(context.Background(), app, []string{"otterio", "server", "disk"}); err != nil {
		t.Fatal(err)
	}
}

func TestCLIGatewayScope(t *testing.T) {
	original := gatewayCommandFactories
	t.Cleanup(func() { gatewayCommandFactories = original })
	for _, test := range []struct {
		name    string
		args    []string
		address string
		quiet   bool
	}{
		{"parent address wins", []string{"gateway", "--address", ":19001", "scope-test", "--address", ":19002"}, ":19001", false},
		{"default parent falls back", []string{"gateway", "scope-test", "--address", ":19002"}, ":19002", false},
		{"root false is present", []string{"--quiet=false", "gateway", "scope-test"}, ":" + GlobalOtterioDefaultPort, true},
		{"leaf false is present", []string{"gateway", "scope-test", "--quiet=false"}, ":" + GlobalOtterioDefaultPort, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			gatewayCommandFactories = original
			if err := RegisterGatewayCommand(func() *cli.Command {
				return &cli.Command{Name: "scope-test", Action: func(_ context.Context, command *cli.Command) error {
					if actual := GatewayServerAddress(command); actual != test.address {
						t.Fatalf("address: got %q, want %q", actual, test.address)
					}
					if actual := command.IsSet("quiet") || cliParentIsSet(command, "quiet"); actual != test.quiet {
						t.Fatalf("quiet presence: got %v, want %v", actual, test.quiet)
					}
					return nil
				}}
			}); err != nil {
				t.Fatal(err)
			}
			if err := clisupport.Run(context.Background(), newApp("otterio"), append([]string{"otterio"}, test.args...)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCLIConfigDirectoryScope(t *testing.T) {
	original := gatewayCommandFactories
	t.Cleanup(func() { gatewayCommandFactories = original })
	rootDir, leafDir := t.TempDir(), t.TempDir()
	if err := RegisterGatewayCommand(func() *cli.Command {
		return &cli.Command{Name: "config-scope-test", Action: func(_ context.Context, command *cli.Command) error {
			dir, set := newConfigDirFromCtx(command, "config-dir", defaultConfigDir.Get)
			if !set || dir.Get() != leafDir {
				t.Fatalf("local explicit directory must win: %q %v", dir.Get(), set)
			}
			return nil
		}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := clisupport.Run(context.Background(), newApp("otterio"), []string{"otterio", "--config-dir", rootDir, "gateway", "config-scope-test", "--config-dir", leafDir}); err != nil {
		t.Fatal(err)
	}
	// The intermediate gateway default must not hide the root explicit directory.
	app := newApp("otterio")
	app.Commands[1].Commands[0].Action = func(_ context.Context, command *cli.Command) error {
		dir, set := newConfigDirFromCtx(command, "config-dir", defaultConfigDir.Get)
		if !set || dir.Get() != rootDir {
			t.Fatalf("root explicit directory must survive parent default: %q %v", dir.Get(), set)
		}
		return nil
	}
	if err := clisupport.Run(context.Background(), app, []string{"otterio", "--config-dir", rootDir, "gateway", "config-scope-test"}); err != nil {
		t.Fatal(err)
	}
}

func TestCLIServerEndpointSources(t *testing.T) {
	for _, test := range []struct {
		name, argsEnv, endpointsEnv string
		want                        []string
	}{
		{"CLI", "", "", []string{"disk1", "disk2"}},
		{"legacy env", "", "legacy1 legacy2", []string{"legacy1", "legacy2"}},
		{"args precedence", "new1 new2", "legacy1", []string{"new1", "new2"}},
		{"whitespace is selected", "  ", "legacy1", []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(config.EnvArgs, test.argsEnv)
			t.Setenv(config.EnvEndpoints, test.endpointsEnv)
			app := newApp("otterio")
			app.Commands[0].Action = func(_ context.Context, command *cli.Command) error {
				if got := serverCmdArgs(command); !reflect.DeepEqual(got, test.want) {
					t.Fatalf("endpoints: got %#v, want %#v", got, test.want)
				}
				return nil
			}
			if err := clisupport.Run(context.Background(), app, []string{"otterio", "server", "disk1", "disk2"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCLIHelpAndUsageStreams(t *testing.T) {
	app := newApp("otterio")
	var output, errorOutput bytes.Buffer
	app.Writer, app.ErrWriter = &output, &errorOutput
	if err := clisupport.Run(context.Background(), app, []string{"otterio", "server", "--help"}); err != nil {
		var exit cli.ExitCoder
		if !errors.As(err, &exit) || exit.ExitCode() != 0 {
			t.Fatal(err)
		}
	}
	if !strings.Contains(output.String(), "otterio server") || !strings.Contains(output.String(), "OTTERIO_ROOT_USER") || strings.Contains(output.String(), "<no value>") || errorOutput.Len() != 0 {
		t.Fatalf("invalid help streams: stdout=%q stderr=%q", output.String(), errorOutput.String())
	}
	output.Reset()
	app = newApp("otterio")
	app.Writer, app.ErrWriter = &output, &errorOutput
	if err := clisupport.Run(context.Background(), app, []string{"otterio", "server", "--unknown"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
	if !strings.HasPrefix(output.String(), "Incorrect Usage:") || strings.Contains(output.String(), "EXAMPLES:") || errorOutput.Len() != 0 {
		t.Fatalf("invalid usage streams: stdout=%q stderr=%q", output.String(), errorOutput.String())
	}
}

func TestCLIVersionValueAndChildParsing(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		version bool
		child   bool
	}{
		{"true stops before invalid child", []string{"--version", "server", "--unknown"}, true, false},
		{"false executes child", []string{"--version=false", "server", "disk"}, false, true},
		{"false has ordinary root behavior", []string{"--version=false"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newApp("otterio")
			var output, errorOutput bytes.Buffer
			app.Writer, app.ErrWriter = &output, &errorOutput
			childCalled := false
			app.Commands[0].Action = func(context.Context, *cli.Command) error {
				childCalled = true
				return nil
			}
			err := clisupport.Run(context.Background(), app, append([]string{"otterio"}, test.args...))
			if test.version {
				var exit cli.ExitCoder
				if !errors.As(err, &exit) || exit.ExitCode() != 0 || output.String() != "otterio version "+ReleaseTag+"\n" {
					t.Fatalf("version: error=%v, stdout=%q", err, output.String())
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if childCalled != test.child || errorOutput.Len() != 0 {
				t.Fatalf("child=%v, stderr=%q", childCalled, errorOutput.String())
			}
		})
	}
}

func TestCLIHelpDoesNotMaskInvalidArguments(t *testing.T) {
	original := gatewayCommandFactories
	t.Cleanup(func() { gatewayCommandFactories = original })
	if err := RegisterGatewayCommand(func() *cli.Command {
		return &cli.Command{Name: "help-contract", Action: func(context.Context, *cli.Command) error {
			t.Fatal("invalid invocation reached its action")
			return nil
		}}
	}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"root", []string{"--help", "--unknown"}, "Incorrect Usage. flag provided but not defined: -unknown\n\n"},
		{"parent", []string{"gateway", "--help", "--unknown"}, "Incorrect Usage. flag provided but not defined: -unknown\n\n"},
		{"leaf", []string{"server", "--help", "--unknown"}, "Incorrect Usage: flag provided but not defined: -unknown\n\n"},
		{"deep leaf", []string{"gateway", "help-contract", "--help", "--unknown"}, "Incorrect Usage: flag provided but not defined: -unknown\n\n"},
		{"root help spellings", []string{"--help", "-h"}, "Cannot use two forms of the same flag: h help\n"},
		{"leaf help spellings", []string{"server", "--help", "-h"}, "Cannot use two forms of the same flag: h help\n\n"},
		{"version spellings", []string{"--version", "-v"}, "Cannot use two forms of the same flag: v version\n"},
		{"directory spellings before unknown", []string{"server", "--config-dir", "a", "-C", "b", "--unknown"}, "Cannot use two forms of the same flag: C config-dir\n\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newApp("otterio")
			var output, errorOutput bytes.Buffer
			app.Writer, app.ErrWriter = &output, &errorOutput
			app.Commands[0].Action = func(context.Context, *cli.Command) error {
				t.Fatal("invalid invocation reached server startup")
				return nil
			}
			err := clisupport.Run(context.Background(), app, append([]string{"otterio"}, test.args...))
			if err == nil {
				t.Fatal("invalid arguments succeeded")
			}
			var exit cli.ExitCoder
			if errors.As(err, &exit) && exit.ExitCode() == 0 {
				t.Fatalf("help masked the invalid arguments: %v", err)
			}
			if output.String() != test.want || errorOutput.Len() != 0 {
				t.Fatalf("stdout=%q, stderr=%q", output.String(), errorOutput.String())
			}
		})
	}
}
