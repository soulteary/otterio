/*
 * MinIO Cloud Storage, (C) 2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/soulteary/otterio/internal/clisupport"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/tinylib/msgp/msgp"
	"github.com/urfave/cli/v3"
)

var xlHeader = [4]byte{'X', 'L', '2', ' '}

func main() {
	clisupport.Install()
	stopAfterFirstArgument := 1
	app := &cli.Command{Name: filepath.Base(os.Args[0]), StopOnNthArg: &stopAfterFirstArgument}
	app.Copyright = "MinIO, Inc."
	app.Usage = "xl.meta to JSON"
	app.HideVersion = true
	app.CustomRootCommandHelpTemplate = `NAME:
  {{.Name}} - {{.Usage}}

USAGE:
  {{.Name}} {{if .VisibleFlags}}[FLAGS]{{end}} METAFILES...
{{if .VisibleFlags}}
GLOBAL FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}
`

	app.HideHelpCommand = true

	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Usage: "Print each file as a separate line without formatting",
			Name:  "ndjson",
		},
	}

	clisupport.Configure(app)
	app.Action = func(_ context.Context, c *cli.Command) error {
		if c.Args().Len() == 0 {
			cli.ShowAppHelp(c)
			return nil
		}
		for _, file := range c.Args().Slice() {
			var r io.Reader
			switch file {
			case "-":
				r = os.Stdin
			default:
				f, err := os.Open(file)
				if err != nil {
					return err
				}
				defer f.Close()
				r = f
			}

			// Read header
			var tmp [4]byte
			_, err := io.ReadFull(r, tmp[:])
			if err != nil {
				return err
			}
			if !bytes.Equal(tmp[:], xlHeader[:]) {
				return fmt.Errorf("xlMeta: unknown XLv2 header, expected %v, got %v", xlHeader[:4], tmp[:4])
			}
			// Skip version check for now
			_, err = io.ReadFull(r, tmp[:])
			if err != nil {
				return err
			}

			var buf bytes.Buffer
			_, err = msgp.CopyToJSON(&buf, r)
			if err != nil {
				return err
			}
			if c.Bool("ndjson") {
				fmt.Println(buf.String())
				continue
			}
			var msi map[string]interface{}
			dec := json.NewDecoder(&buf)
			// Use number to preserve integers.
			dec.UseNumber()
			err = dec.Decode(&msi)
			if err != nil {
				return err
			}
			b, err := json.MarshalIndent(msi, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
		}
		return nil
	}
	err := app.Run(context.Background(), os.Args)
	if err != nil {
		var exit cli.ExitCoder
		if errors.As(err, &exit) && exit.ExitCode() == 0 {
			return
		}
		log.Fatal(err)
	}
}
