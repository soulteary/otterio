/*
 * MinIO Cloud Storage, (C) 2019 MinIO, Inc.
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

package config

// Config value separator
const (
	ValueSeparator = ","
)

// Top level common ENVs
const (
	EnvAccessKey       = "OTTERIO_ACCESS_KEY"
	EnvSecretKey       = "OTTERIO_SECRET_KEY"
	EnvRootUser        = "OTTERIO_ROOT_USER"
	EnvRootPassword    = "OTTERIO_ROOT_PASSWORD"
	EnvAccessKeyOld    = "OTTERIO_ACCESS_KEY_OLD"
	EnvSecretKeyOld    = "OTTERIO_SECRET_KEY_OLD"
	EnvRootUserOld     = "OTTERIO_ROOT_USER_OLD"
	EnvRootPasswordOld = "OTTERIO_ROOT_PASSWORD_OLD"
	EnvBrowser         = "OTTERIO_BROWSER"
	EnvDomain          = "OTTERIO_DOMAIN"
	EnvRegionName      = "OTTERIO_REGION_NAME"
	EnvPublicIPs       = "OTTERIO_PUBLIC_IPS"
	EnvFSOSync         = "OTTERIO_FS_OSYNC"
	EnvArgs            = "OTTERIO_ARGS"
	EnvDNSWebhook      = "OTTERIO_DNS_WEBHOOK_ENDPOINT"

	EnvEndpoints = "OTTERIO_ENDPOINTS" // legacy
	EnvWorm      = "OTTERIO_WORM"      // legacy
	EnvRegion    = "OTTERIO_REGION"    // legacy
)
