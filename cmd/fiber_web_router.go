/*
 * MinIO Cloud Storage, (C) 2016 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
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

package cmd

import (
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/compress"

	"github.com/soulteary/otterio/browser"
	"github.com/soulteary/otterio/cmd/logger"
	jsonrpc "github.com/soulteary/otterio/pkg/rpc"
	"github.com/soulteary/otterio/pkg/rpc/json2"
)

var webMozillaRegex = regexp.MustCompile(`.*Mozilla.*`)
var webSpecialAssetsRegex = regexp.MustCompile(`^(` + specialAssets + `)$`)

func registerWebRouterFiber(app *fiber.App) error {
	web := &webAPIHandlers{
		ObjectAPI: newObjectLayerFn,
		CacheAPI:  newCachedObjectLayerFn,
	}

	codec := json2.NewCodec()

	webRPC := jsonrpc.NewServer()
	webRPC.RegisterCodec(codec, "application/json")
	webRPC.RegisterCodec(codec, "application/json; charset=UTF-8")
	webRPC.RegisterAfterFunc(func(ri *jsonrpc.RequestInfo) {
		if ri != nil {
			claims, _, _ := webRequestAuthenticate(ri.Request)
			bucketName, objectName := extractBucketObject(ri.Args)
			ri.Request = setURLVarsOnRequest(ri.Request, map[string]string{
				"bucket": bucketName,
				"object": objectName,
			})
			if globalTrace.NumSubscribers() > 0 {
				globalTrace.Publish(WebTrace(ri))
			}
			ctx := newContext(ri.Request, ri.ResponseWriter, ri.Method)
			logger.AuditLog(ctx, ri.ResponseWriter, ri.Request, claims.Map())
		}
	})

	if err := webRPC.RegisterService(web, "web"); err != nil {
		return err
	}

	assetFS, err := fs.Sub(browser.GetStaticAssets(), assetPrefix)
	if err != nil {
		return err
	}

	fileServer := http.StripPrefix(minioReservedBucketPath, http.FileServer(http.FS(assetFS)))
	serveAssets := adaptor.HTTPHandler(fileServer)

	prefix := minioReservedBucketPath

	webRules := []routeRule{
		{
			methods: []string{http.MethodPost},
			handler: func(c fiber.Ctx) error {
				return adaptor.HTTPHandler(webRPC)(c)
			},
			skipTrace: true,
		},
	}
	webUploadRules := []routeRule{
		{
			methods:      []string{http.MethodPut},
			handler:      toMinioHandler(web.Upload),
			traceHeaders: true,
		},
	}
	webDownloadRules := []routeRule{
		{
			methods: []string{http.MethodGet},
			queries: map[string]string{"token": ".*"},
			// Streams a single object (io.Copy) to the client; like GetObject it
			// must use the streaming bridge so large downloads are not buffered
			// entirely in memory by the buffered bridge.
			handler:      toMinioStreamHandler(web.Download),
			traceHeaders: true,
		},
	}
	webZipRules := []routeRule{
		{
			methods: []string{http.MethodPost},
			queries: map[string]string{"token": ".*"},
			// Streams a (potentially large) multi-object zip archive; must stream
			// for the same reason as the single-object download above.
			handler:      toMinioStreamHandler(web.DownloadZip),
			traceHeaders: true,
		},
	}

	app.Use(prefix, compress.New(), func(c fiber.Ctx) error {
		if !webMozillaRegex.MatchString(c.Get("User-Agent")) {
			return c.Next()
		}

		relPath := strings.TrimPrefix(c.Path(), prefix)
		if relPath == "" {
			relPath = "/"
		}

		switch {
		case relPath == "/webrpc":
			matched, err := dispatchRules(c, webRules)
			if matched {
				return err
			}
		case strings.HasPrefix(relPath, "/upload/"):
			parts := strings.SplitN(strings.TrimPrefix(relPath, "/upload/"), "/", 2)
			if len(parts) == 2 {
				setPathVars(c, parts[0], parts[1])
				matched, err := dispatchRules(c, webUploadRules)
				if matched {
					return err
				}
			}
		case strings.HasPrefix(relPath, "/download/"):
			parts := strings.SplitN(strings.TrimPrefix(relPath, "/download/"), "/", 2)
			if len(parts) == 2 {
				setPathVars(c, parts[0], parts[1])
				matched, err := dispatchRules(c, webDownloadRules)
				if matched {
					return err
				}
			}
		case relPath == "/zip":
			matched, err := dispatchRules(c, webZipRules)
			if matched {
				return err
			}
		case webSpecialAssetsRegex.MatchString(strings.TrimPrefix(relPath, "/")):
			return serveAssets(c)
		default:
			c.Request().URI().SetPath(path.Join(minioReservedBucketPath, "/"))
			return serveAssets(c)
		}

		return c.Next()
	})

	return nil
}
