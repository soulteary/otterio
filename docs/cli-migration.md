# CLI framework migration

OtterIO uses `github.com/urfave/cli/v3 v3.14.0`. The command grammar and
startup lifecycle remain compatible with the previous `minio/cli v1.24.2`
program. In particular a present `--quiet=false`, `--json=false`, or
`--anonymous=false` still enables the respective logging mode. Address and
directory inheritance retain their existing field-specific precedence.

## Go gateway API

The gateway registration API now accepts a factory. A factory must return a
fresh command and fresh flag objects on every call; v3 flags contain parsed
state. A factory must also return a consistent command name and complete
command tree each time. Registration belongs in `init` before `cmd.Main` is called. Gateway
names must be nonempty and unique.

```go
import (
    "context"
    otterio "github.com/soulteary/otterio/cmd"
    "github.com/urfave/cli/v3"
)

func init() {
    if err := otterio.RegisterGatewayCommand(func() *cli.Command {
        return &cli.Command{
            Name: "example",
            Usage: "example gateway",
            Flags: []cli.Flag{&cli.StringFlag{Name: "backend", Local: true}},
            Action: func(ctx context.Context, command *cli.Command) error {
                // Resolve backend options, then construct your Gateway.
                return otterio.StartGateway(ctx, command, newExampleGateway())
            },
        }
    }); err != nil {
        panic(err)
    }
}
```

`StartGateway(ctx context.Context, command *cli.Command, gateway Gateway) error`
replaces the old `*cli.Context` entrypoint. Startup still uses OtterIO's
existing global cancellation context and signal supervisor.

`GlobalFlags()` and `ServerFlags()` replace shared exported slices and return
new flag objects. Registration adds both sets to the backend command; callers
should only provide their backend-specific flags. `GatewayServerAddress`
returns the effective address with the existing gateway-parent precedence.

Upstream v3 flags are persistent unless `Local: true` is set. Backend-specific
flags should normally use `Local: true` to preserve the previous per-command
scope. A backend intentionally exposing flags to its own children can retain
the upstream persistent behavior. The flags added by OtterIO are explicitly
local at each root, gateway, and backend scope.

When porting an old `IntSliceFlag` or `Int64SliceFlag`, the factory must set
`Config: cli.IntegerConfig{Base: 10}` explicitly:

```go
Flags: []cli.Flag{
    &cli.IntSliceFlag{
        Name: "numbers", Aliases: []string{"N"}, Local: true,
        Config: cli.IntegerConfig{Base: 10},
    },
    &cli.Int64SliceFlag{
        Name: "large-numbers", Aliases: []string{"L"}, Local: true,
        Config: cli.IntegerConfig{Base: 10},
    },
}
```

The old framework parsed these lists in decimal: `010` means ten and `0x10`
is rejected. Native v3 defaults to `Base: 0`, which accepts base prefixes
and interprets those values as eight and sixteen. OtterIO preserves the
factory's explicit native configuration, so it does not rewrite `Base: 0`.
Its built-in server and gateway commands do not declare integer list flags.
Repeated list flags retain the native separator configuration. Long and short
spellings of an existing scalar or legacy list flag must not be mixed at one
command scope; repeating one spelling remains valid.

No compatibility facade or vendored CLI fork is maintained. Custom help
example fields and legacy flag presentation live in `internal/clisupport`.
Its `Run` entrypoint performs only token classification from declared command
and flag metadata before calling the native runner. It preserves the first
positional stopping point of root/group commands, interspersed leaf flags,
quoted whitespace, `--` per command scope, and the previous diagnostics for
non-letter or malformed options and explicitly empty boolean values. Flag
value parsing and the startup lifecycle remain owned by upstream v3.

## Deliberate compatibility boundaries

The container entrypoint remains a separate credential gate. Its help bypass
rules are tested separately from the Go command parser. The English xl.meta
tool assumes `xl.meta` when invoked without files; the Chinese tool prints
help without arguments. These two behaviors are preserved.
