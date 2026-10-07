package clisupport

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
)

type aliasValue string

func (value *aliasValue) Set(text string) error {
	if text == "invalid" {
		return fmt.Errorf("invalid test value")
	}
	*value = aliasValue(text)
	return nil
}
func (value *aliasValue) String() string { return string(*value) }
func (value *aliasValue) Get() any       { return string(*value) }
func (value *aliasValue) MarshalText() ([]byte, error) {
	return []byte(*value), nil
}
func (value *aliasValue) UnmarshalText(text []byte) error { return value.Set(string(text)) }

func TestNativeFlagAliasScopes(t *testing.T) {
	Install()
	for _, test := range []struct {
		name, valid string
		factory     func() cli.Flag
	}{
		{"bool", "false", func() cli.Flag { return &cli.BoolFlag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"string", "text", func() cli.Flag { return &cli.StringFlag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"int", "1", func() cli.Flag { return &cli.IntFlag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"int8", "1", func() cli.Flag { return &cli.Int8Flag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"int16", "1", func() cli.Flag { return &cli.Int16Flag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"int32", "1", func() cli.Flag { return &cli.Int32Flag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"int64", "1", func() cli.Flag { return &cli.Int64Flag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"uint", "1", func() cli.Flag { return &cli.UintFlag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"uint8", "1", func() cli.Flag { return &cli.Uint8Flag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"uint16", "1", func() cli.Flag { return &cli.Uint16Flag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"uint32", "1", func() cli.Flag { return &cli.Uint32Flag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"uint64", "1", func() cli.Flag { return &cli.Uint64Flag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"float32", "1.5", func() cli.Flag { return &cli.Float32Flag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"float64", "1.5", func() cli.Flag { return &cli.Float64Flag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"duration", "1s", func() cli.Flag { return &cli.DurationFlag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"timestamp", "2026-10-08T00:00:00Z", func() cli.Flag {
			return &cli.TimestampFlag{Name: "value", Aliases: []string{"x"}, Config: cli.TimestampConfig{Layouts: []string{time.RFC3339}}, Local: true}
		}},
		{"generic", "text", func() cli.Flag {
			return &cli.GenericFlag{Name: "value", Aliases: []string{"x"}, Value: new(aliasValue), Local: true}
		}},
		{"text", "text", func() cli.Flag {
			return &cli.TextFlag{Name: "value", Aliases: []string{"x"}, Value: new(aliasValue), Local: true}
		}},
		{"string list", "text", func() cli.Flag { return &cli.StringSliceFlag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"int list", "1", func() cli.Flag { return &cli.IntSliceFlag{Name: "value", Aliases: []string{"x"}, Local: true} }},
		{"int64 list", "1", func() cli.Flag { return &cli.Int64SliceFlag{Name: "value", Aliases: []string{"x"}, Local: true} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, args := range [][]string{{"--value=" + test.valid, "-x=" + test.valid}, {"--value=" + test.valid, "-x=" + test.valid, "--unknown"}} {
				var output bytes.Buffer
				command := &cli.Command{Name: "test", Flags: []cli.Flag{test.factory()}, Writer: &output, Action: func(context.Context, *cli.Command) error {
					t.Fatal("alias conflict reached action")
					return nil
				}}
				Configure(command)
				err := Run(context.Background(), command, append([]string{"test"}, args...))
				if err == nil || output.String() != "Cannot use two forms of the same flag: x value\n" {
					t.Fatalf("args=%q err=%v stdout=%q", args, err, output.String())
				}
			}
			for _, invalid := range []bool{false, true} {
				var output bytes.Buffer
				called := false
				command := &cli.Command{Name: "test", Flags: []cli.Flag{test.factory()}, Writer: &output, Action: func(context.Context, *cli.Command) error { called = true; return nil }}
				Configure(command)
				second := "--value=" + test.valid
				if invalid {
					second = "-x=invalid"
				}
				err := Run(context.Background(), command, []string{"test", "--value=" + test.valid, second})
				if invalid && test.name == "string" || invalid && test.name == "string list" {
					if err == nil || called {
						t.Fatal("two successful string spellings were accepted")
					}
				} else if invalid {
					if err == nil || called || strings.Contains(output.String(), "Cannot use two forms") {
						t.Fatalf("failed Set counted as an alias: err=%v called=%v stdout=%q", err, called, output.String())
					}
				} else if err != nil || !called {
					t.Fatalf("same spelling was rejected: err=%v called=%v", err, called)
				}
			}
		})
	}
}

func TestTypedListAliasWrapperPreservesNativeSeparators(t *testing.T) {
	Install()
	for _, disable := range []bool{true, false} {
		for _, wrapped := range []bool{false, true} {
			list := &cli.StringSliceFlag{Name: "list", Aliases: []string{"l"}, Local: true}
			called := false
			command := &cli.Command{Name: "test", Flags: []cli.Flag{list}, Action: func(_ context.Context, cmd *cli.Command) error {
				called = true
				want := []string{"a|b", "c"}
				if !disable {
					want = []string{"a", "b", "c"}
				}
				if !reflect.DeepEqual(cmd.StringSlice("list"), want) {
					t.Fatalf("disable=%v wrapped=%v got=%#v want=%#v", disable, wrapped, cmd.StringSlice("list"), want)
				}
				return nil
			}}
			Configure(command)
			if !wrapped {
				command.Flags[0] = list
			}
			command.DisableSliceFlagSeparator, command.SliceFlagSeparator = disable, "|"
			if err := Run(context.Background(), command, []string{"test", "--list", "a|b", "--list", "c"}); err != nil || !called {
				t.Fatalf("err=%v called=%v", err, called)
			}
		}
	}
}

func TestTypedListAliasWrapperPreservesExplicitIntegerBase(t *testing.T) {
	Install()
	for _, base := range []int{0, 10} {
		for _, value := range []string{"010", "0x10"} {
			called := false
			command := &cli.Command{Name: "test", Flags: []cli.Flag{
				&cli.IntSliceFlag{Name: "numbers", Aliases: []string{"n"}, Config: cli.IntegerConfig{Base: base}, Local: true},
				&cli.Int64SliceFlag{Name: "wide", Aliases: []string{"w"}, Config: cli.IntegerConfig{Base: base}, Local: true},
			}, Action: func(_ context.Context, cmd *cli.Command) error {
				called = true
				want := 10
				if base == 0 {
					want = 8
					if value == "0x10" {
						want = 16
					}
				}
				if !reflect.DeepEqual(cmd.IntSlice("numbers"), []int{want}) || !reflect.DeepEqual(cmd.Int64Slice("wide"), []int64{int64(want)}) {
					t.Fatalf("base=%d value=%q numbers=%v wide=%v want=%d", base, value, cmd.IntSlice("numbers"), cmd.Int64Slice("wide"), want)
				}
				return nil
			}}
			Configure(command)
			err := Run(context.Background(), command, []string{"test", "--numbers", value, "--wide", value})
			if base == 10 && value == "0x10" {
				if err == nil || called {
					t.Fatalf("decimal list accepted hexadecimal value: err=%v called=%v", err, called)
				}
			} else if err != nil || !called {
				t.Fatalf("explicit base was changed: base=%d value=%q err=%v called=%v", base, value, err, called)
			}
		}
	}
}
