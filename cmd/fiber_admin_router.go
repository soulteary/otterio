/*
 * MinIO Cloud Storage, (C) 2016, 2017, 2018, 2019 MinIO, Inc.
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
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
)

func registerAdminRouterFiber(app *fiber.App, enableConfigOps, enableIAMOps bool) {
	adminAPI := adminAPIHandlers{}

	adminVersions := []string{
		adminAPIVersionPrefix,
		adminAPIVersionV2Prefix,
	}

	for _, adminVersion := range adminVersions {
		verPrefix := adminPathPrefix + adminVersion

		registerAdminRoute(app, verPrefix+"/service", []routeRule{
			adminRule(http.MethodPost, adminAPI.ServiceHandler, false, map[string]string{"action": ".*"}),
		})
		registerAdminRoute(app, verPrefix+"/update", []routeRule{
			adminRule(http.MethodPost, adminAPI.ServerUpdateHandler, false, map[string]string{"updateURL": ".*"}),
		})
		registerAdminRoute(app, verPrefix+"/info", []routeRule{
			adminRule(http.MethodGet, adminAPI.ServerInfoHandler, false, nil),
		})
		registerAdminRoute(app, verPrefix+"/storageinfo", []routeRule{
			adminRule(http.MethodGet, adminAPI.StorageInfoHandler, false, nil),
		})
		registerAdminRoute(app, verPrefix+"/datausageinfo", []routeRule{
			adminRule(http.MethodGet, adminAPI.DataUsageInfoHandler, false, nil),
		})

		if globalIsDistErasure || globalIsErasure {
			registerAdminHealRoutes(app, verPrefix, adminAPI)
			registerAdminRoute(app, verPrefix+"/background-heal/status", []routeRule{
				adminRule(http.MethodPost, adminAPI.BackgroundHealStatusHandler, false, nil),
			})
		}

		registerAdminRoute(app, verPrefix+"/profiling/start", []routeRule{
			adminRule(http.MethodPost, adminAPI.StartProfilingHandler, false, map[string]string{"profilerType": ".*"}),
		})
		registerAdminRoute(app, verPrefix+"/profiling/download", []routeRule{
			adminRule(http.MethodGet, adminAPI.DownloadProfilingHandler, false, nil),
		})

		if enableConfigOps {
			registerAdminRoute(app, verPrefix+"/get-config-kv", []routeRule{
				adminRule(http.MethodGet, adminAPI.GetConfigKVHandler, true, map[string]string{"key": ".*"}),
			})
			registerAdminRoute(app, verPrefix+"/set-config-kv", []routeRule{
				adminRule(http.MethodPut, adminAPI.SetConfigKVHandler, true, nil),
			})
			registerAdminRoute(app, verPrefix+"/del-config-kv", []routeRule{
				adminRule(http.MethodDelete, adminAPI.DelConfigKVHandler, true, nil),
			})
		}

		registerAdminRoute(app, verPrefix+"/help-config-kv", []routeRule{
			adminRule(http.MethodGet, adminAPI.HelpConfigKVHandler, false, map[string]string{
				"subSys": ".*",
				"key":    ".*",
			}),
		})

		if enableConfigOps {
			registerAdminRoute(app, verPrefix+"/list-config-history-kv", []routeRule{
				adminRule(http.MethodGet, adminAPI.ListConfigHistoryKVHandler, false, map[string]string{"count": "[0-9]+"}),
			})
			registerAdminRoute(app, verPrefix+"/clear-config-history-kv", []routeRule{
				adminRule(http.MethodDelete, adminAPI.ClearConfigHistoryKVHandler, true, map[string]string{"restoreId": ".*"}),
			})
			registerAdminRoute(app, verPrefix+"/restore-config-history-kv", []routeRule{
				adminRule(http.MethodPut, adminAPI.RestoreConfigHistoryKVHandler, true, map[string]string{"restoreId": ".*"}),
			})
			registerAdminRoute(app, verPrefix+"/config", []routeRule{
				adminRule(http.MethodGet, adminAPI.GetConfigHandler, true, nil),
				adminRule(http.MethodPut, adminAPI.SetConfigHandler, true, nil),
			})
		}

		if enableIAMOps {
			registerAdminIAMRoutes(app, verPrefix, adminVersion, adminAPI)
		}

		if globalIsDistErasure || globalIsErasure {
			registerAdminRoute(app, verPrefix+"/get-bucket-quota", []routeRule{
				adminRule(http.MethodGet, adminAPI.GetBucketQuotaConfigHandler, true, map[string]string{"bucket": ".*"}),
			})
			registerAdminRoute(app, verPrefix+"/set-bucket-quota", []routeRule{
				adminRule(http.MethodPut, adminAPI.PutBucketQuotaConfigHandler, true, map[string]string{"bucket": ".*"}),
			})
			registerAdminRoute(app, verPrefix+"/list-remote-targets", []routeRule{
				adminRule(http.MethodGet, adminAPI.ListRemoteTargetsHandler, true, map[string]string{
					"bucket": ".*",
					"type":   ".*",
				}),
			})
			registerAdminRoute(app, verPrefix+"/set-remote-target", []routeRule{
				adminRule(http.MethodPut, adminAPI.SetRemoteTargetHandler, true, map[string]string{"bucket": ".*"}),
			})
			registerAdminRoute(app, verPrefix+"/remove-remote-target", []routeRule{
				adminRule(http.MethodDelete, adminAPI.RemoveRemoteTargetHandler, true, map[string]string{
					"bucket": ".*",
					"arn":    ".*",
				}),
			})
		}

		if globalIsDistErasure {
			registerAdminRoute(app, verPrefix+"/top/locks", []routeRule{
				adminRule(http.MethodGet, adminAPI.TopLocksHandler, true, nil),
			})
			registerAdminRoute(app, verPrefix+"/force-unlock", []routeRule{
				adminRule(http.MethodPost, adminAPI.ForceUnlockHandler, true, map[string]string{"paths": ".*"}),
			})
		}

		registerAdminRoute(app, verPrefix+"/trace", []routeRule{{
			methods:   []string{http.MethodGet},
			handler:   toMinioStreamHandler(adminAPI.TraceHandler),
			skipTrace: true,
		}})
		registerAdminRoute(app, verPrefix+"/log", []routeRule{
			adminStreamRule(http.MethodGet, adminAPI.ConsoleLogHandler, nil),
		})

		registerAdminRoute(app, verPrefix+"/kms/key/create", []routeRule{
			adminRule(http.MethodPost, adminAPI.KMSCreateKeyHandler, false, map[string]string{"key-id": ".*"}),
		})
		registerAdminRoute(app, verPrefix+"/kms/key/status", []routeRule{
			adminRule(http.MethodGet, adminAPI.KMSKeyStatusHandler, false, nil),
		})

		if !globalIsGateway {
			// HealthInfo streams partial results with a 5s keepalive and a deadline
			// of up to 1h; BandwidthMonitor is an unbounded event stream. Both must
			// use the streaming bridge so output is delivered incrementally, the
			// keepalive reaches the client, memory stays bounded, and (for
			// BandwidthMonitor) the handler terminates on client disconnect.
			registerAdminRoute(app, verPrefix+"/obdinfo", []routeRule{
				adminStreamRule(http.MethodGet, adminAPI.HealthInfoHandler, nil),
			})
			registerAdminRoute(app, verPrefix+"/healthinfo", []routeRule{
				adminStreamRule(http.MethodGet, adminAPI.HealthInfoHandler, nil),
			})
			registerAdminRoute(app, verPrefix+"/bandwidth", []routeRule{
				adminStreamRule(http.MethodGet, adminAPI.BandwidthMonitorHandler, nil),
			})
		}
	}

	app.All(adminPathPrefix+"/*", func(c fiber.Ctx) error {
		if !strings.HasPrefix(c.Path(), adminPathPrefix) {
			return c.Next()
		}
		return errorResponseHandlerFiber(c)
	})
}

func registerAdminHealRoutes(app *fiber.App, verPrefix string, adminAPI adminAPIHandlers) {
	healRule := []routeRule{
		adminRule(http.MethodPost, adminAPI.HealHandler, false, nil),
	}
	registerAdminRoute(app, verPrefix+"/heal/", healRule)
	app.All(verPrefix+"/heal/:bucket", func(c fiber.Ctx) error {
		c.Locals(fiberBucketParam, c.Params("bucket"))
		matched, err := dispatchRules(c, healRule)
		if !matched {
			return methodNotAllowedHandlerFiber("Admin")(c)
		}
		return err
	})
	app.All(verPrefix+"/heal/:bucket/*", func(c fiber.Ctx) error {
		c.Locals(fiberBucketParam, c.Params("bucket"))
		c.Locals(fiberPrefixParam, strings.TrimPrefix(c.Params("*"), "/"))
		matched, err := dispatchRules(c, healRule)
		if !matched {
			return methodNotAllowedHandlerFiber("Admin")(c)
		}
		return err
	})
}

func registerAdminIAMRoutes(app *fiber.App, verPrefix, adminVersion string, adminAPI adminAPIHandlers) {
	registerAdminRoute(app, verPrefix+"/add-canned-policy", []routeRule{
		adminRule(http.MethodPut, adminAPI.AddCannedPolicy, false, map[string]string{"name": ".*"}),
	})
	registerAdminRoute(app, verPrefix+"/accountinfo", []routeRule{
		adminRule(http.MethodGet, adminAPI.AccountInfoHandler, false, nil),
	})
	registerAdminRoute(app, verPrefix+"/add-user", []routeRule{
		adminRule(http.MethodPut, adminAPI.AddUser, true, map[string]string{"accessKey": ".*"}),
	})
	registerAdminRoute(app, verPrefix+"/set-user-status", []routeRule{
		adminRule(http.MethodPut, adminAPI.SetUserStatus, true, map[string]string{
			"accessKey": ".*",
			"status":    ".*",
		}),
	})
	registerAdminRoute(app, verPrefix+"/add-service-account", []routeRule{
		adminRule(http.MethodPut, adminAPI.AddServiceAccount, true, nil),
	})
	registerAdminRoute(app, verPrefix+"/update-service-account", []routeRule{
		adminRule(http.MethodPost, adminAPI.UpdateServiceAccount, true, map[string]string{"accessKey": ".*"}),
	})
	registerAdminRoute(app, verPrefix+"/info-service-account", []routeRule{
		adminRule(http.MethodGet, adminAPI.InfoServiceAccount, true, map[string]string{"accessKey": ".*"}),
	})
	registerAdminRoute(app, verPrefix+"/list-service-accounts", []routeRule{
		adminRule(http.MethodGet, adminAPI.ListServiceAccounts, true, nil),
	})
	registerAdminRoute(app, verPrefix+"/delete-service-account", []routeRule{
		adminRule(http.MethodDelete, adminAPI.DeleteServiceAccount, true, map[string]string{"accessKey": ".*"}),
	})

	if adminVersion == adminAPIVersionV2Prefix {
		registerAdminRoute(app, verPrefix+"/info-canned-policy", []routeRule{
			adminRule(http.MethodGet, adminAPI.InfoCannedPolicyV2, true, map[string]string{"name": ".*"}),
		})
		registerAdminRoute(app, verPrefix+"/list-canned-policies", []routeRule{
			adminRule(http.MethodGet, adminAPI.ListCannedPoliciesV2, true, nil),
		})
	} else {
		registerAdminRoute(app, verPrefix+"/info-canned-policy", []routeRule{
			adminRule(http.MethodGet, adminAPI.InfoCannedPolicy, true, map[string]string{"name": ".*"}),
		})
		registerAdminRoute(app, verPrefix+"/list-canned-policies", []routeRule{
			adminRule(http.MethodGet, adminAPI.ListCannedPolicies, true, nil),
		})
	}

	registerAdminRoute(app, verPrefix+"/remove-canned-policy", []routeRule{
		adminRule(http.MethodDelete, adminAPI.RemoveCannedPolicy, true, map[string]string{"name": ".*"}),
	})
	registerAdminRoute(app, verPrefix+"/set-user-or-group-policy", []routeRule{
		adminRule(http.MethodPut, adminAPI.SetPolicyForUserOrGroup, true, map[string]string{
			"policyName":  ".*",
			"userOrGroup": ".*",
			"isGroup":     "true|false",
		}),
	})
	registerAdminRoute(app, verPrefix+"/remove-user", []routeRule{
		adminRule(http.MethodDelete, adminAPI.RemoveUser, true, map[string]string{"accessKey": ".*"}),
	})
	registerAdminRoute(app, verPrefix+"/list-users", []routeRule{
		adminRule(http.MethodGet, adminAPI.ListUsers, true, nil),
	})
	registerAdminRoute(app, verPrefix+"/user-info", []routeRule{
		adminRule(http.MethodGet, adminAPI.GetUserInfo, true, map[string]string{"accessKey": ".*"}),
	})
	registerAdminRoute(app, verPrefix+"/update-group-members", []routeRule{
		adminRule(http.MethodPut, adminAPI.UpdateGroupMembers, true, nil),
	})
	registerAdminRoute(app, verPrefix+"/group", []routeRule{
		adminRule(http.MethodGet, adminAPI.GetGroup, true, map[string]string{"group": ".*"}),
	})
	registerAdminRoute(app, verPrefix+"/groups", []routeRule{
		adminRule(http.MethodGet, adminAPI.ListGroups, true, nil),
	})
	registerAdminRoute(app, verPrefix+"/set-group-status", []routeRule{
		adminRule(http.MethodPut, adminAPI.SetGroupStatus, true, map[string]string{
			"group":  ".*",
			"status": ".*",
		}),
	})
}

func registerAdminRoute(app *fiber.App, routePath string, rules []routeRule) {
	app.All(routePath, func(c fiber.Ctx) error {
		matched, err := dispatchRules(c, rules)
		if !matched {
			return methodNotAllowedHandlerFiber("Admin")(c)
		}
		return err
	})
}

func adminRule(method string, h func(http.ResponseWriter, *http.Request), traceHdrs bool, queries map[string]string) routeRule {
	return routeRule{
		methods:      []string{method},
		queries:      queries,
		handler:      toMinioHandler(h),
		traceHeaders: traceHdrs,
	}
}

// adminStreamRule registers a long-lived streaming admin handler (e.g. the
// console log stream) through the streaming bridge so that per-write Flush is
// delivered to the client and client disconnects propagate back to the handler.
// skipTrace is set because the trace-all wrapper would otherwise buffer the
// entire (unbounded) streamed body whenever a trace subscriber is active.
func adminStreamRule(method string, h func(http.ResponseWriter, *http.Request), queries map[string]string) routeRule {
	return routeRule{
		methods:   []string{method},
		queries:   queries,
		handler:   toMinioStreamHandler(h),
		skipTrace: true,
	}
}
