/*
 * MinIO Cloud Storage, (C) 2018-2020 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/minio
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
	"net/http"
	"regexp"

	"github.com/gofiber/fiber/v3"
	xhttp "github.com/minio/minio/cmd/http"
)

func registerSTSRouterFiber(app *fiber.App) {
	sts := &stsAPIHandlers{}

	formContentType := regexp.MustCompile(`(?i)application/x-www-form-urlencoded.*`)
	signV4Auth := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(signV4Algorithm) + `.*`)

	stsRules := []routeRule{
		{
			methods: []string{http.MethodPost},
			queries: map[string]string{
				stsAction:       ldapIdentity,
				stsVersion:      stsAPIVersion,
				stsLDAPUsername: ".*",
				stsLDAPPassword: ".*",
			},
			handler: toMinioHandler(sts.AssumeRoleWithLDAPIdentity),
		},
		{
			methods: []string{http.MethodPost},
			queries: map[string]string{
				stsAction:           webIdentity,
				stsVersion:          stsAPIVersion,
				stsWebIdentityToken: ".*",
			},
			handler: toMinioHandler(sts.AssumeRoleWithWebIdentity),
		},
		{
			methods: []string{http.MethodPost},
			queries: map[string]string{
				stsAction:  clientGrants,
				stsVersion: stsAPIVersion,
				stsToken:   ".*",
			},
			handler: toMinioHandler(sts.AssumeRoleWithClientGrants),
		},
		{
			methods:           []string{http.MethodPost},
			requireEmptyQuery: true,
			headerRegex: map[string]*regexp.Regexp{
				xhttp.ContentType:   formContentType,
				xhttp.Authorization: signV4Auth,
			},
			handler: toMinioHandler(sts.AssumeRole),
		},
		{
			methods:           []string{http.MethodPost},
			requireEmptyQuery: true,
			headerRegex: map[string]*regexp.Regexp{
				xhttp.ContentType: formContentType,
			},
			handler: toMinioHandler(sts.AssumeRoleWithSSO),
		},
	}

	app.Use(func(c fiber.Ctx) error {
		matched, err := dispatchRules(c, stsRules)
		if !matched {
			return c.Next()
		}
		return err
	})
}
