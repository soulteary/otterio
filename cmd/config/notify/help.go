/*
 * MinIO Cloud Storage, (C) 2019 MinIO, Inc.
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

package notify

import (
	"github.com/soulteary/otterio/cmd/config"
	"github.com/soulteary/otterio/pkg/event/target"
)

const (
	formatComment     = `'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'`
	queueDirComment   = `staging dir for undelivered messages e.g. '/home/events'`
	queueLimitComment = `maximum limit for undelivered messages, defaults to '100000'`
)

// Help template inputs for all notification targets
var (
	HelpWebhook = config.HelpKVS{
		config.HelpKV{
			Key:         target.WebhookEndpoint,
			Description: "webhook server endpoint e.g. http://localhost:8080/otterio/events",
			Type:        "url",
		},
		config.HelpKV{
			Key:         target.WebhookAuthToken,
			Description: "opaque string or JWT authorization token",
			Optional:    true,
			Type:        "string",
		},
		config.HelpKV{
			Key:         target.WebhookQueueDir,
			Description: queueDirComment,
			Optional:    true,
			Type:        "path",
		},
		config.HelpKV{
			Key:         target.WebhookQueueLimit,
			Description: queueLimitComment,
			Optional:    true,
			Type:        "number",
		},
		config.HelpKV{
			Key:         config.Comment,
			Description: config.DefaultComment,
			Optional:    true,
			Type:        "sentence",
		},
		config.HelpKV{
			Key:         target.WebhookClientCert,
			Description: "client cert for Webhook mTLS auth",
			Optional:    true,
			Type:        "string",
		},
		config.HelpKV{
			Key:         target.WebhookClientKey,
			Description: "client cert key for Webhook mTLS auth",
			Optional:    true,
			Type:        "string",
		},
	}

	HelpPostgres = config.HelpKVS{
		config.HelpKV{
			Key:         target.PostgresConnectionString,
			Description: `Postgres server connection-string e.g. "host=localhost port=5432 dbname=otterio_events user=postgres password=password sslmode=disable"`,
			Type:        "string",
		},
		config.HelpKV{
			Key:         target.PostgresTable,
			Description: "DB table name to store/update events, table is auto-created",
			Type:        "string",
		},
		config.HelpKV{
			Key:         target.PostgresFormat,
			Description: formatComment,
			Type:        "namespace*|access",
		},
		config.HelpKV{
			Key:         target.PostgresQueueDir,
			Description: queueDirComment,
			Optional:    true,
			Type:        "path",
		},
		config.HelpKV{
			Key:         target.PostgresQueueLimit,
			Description: queueLimitComment,
			Optional:    true,
			Type:        "number",
		},
		config.HelpKV{
			Key:         config.Comment,
			Description: config.DefaultComment,
			Optional:    true,
			Type:        "sentence",
		},
		config.HelpKV{
			Key:         target.PostgresMaxOpenConnections,
			Description: "To set the maximum number of open connections to the database. The value is set to `2` by default.",
			Optional:    true,
			Type:        "number",
		},
	}

	HelpMySQL = config.HelpKVS{
		config.HelpKV{
			Key:         target.MySQLDSNString,
			Description: `MySQL data-source-name connection string e.g. "<user>:<password>@tcp(<host>:<port>)/<database>"`,
			Optional:    true,
			Type:        "string",
		},
		config.HelpKV{
			Key:         target.MySQLTable,
			Description: "DB table name to store/update events, table is auto-created",
			Type:        "string",
		},
		config.HelpKV{
			Key:         target.MySQLFormat,
			Description: formatComment,
			Type:        "namespace*|access",
		},
		config.HelpKV{
			Key:         target.MySQLQueueDir,
			Description: queueDirComment,
			Optional:    true,
			Type:        "path",
		},
		config.HelpKV{
			Key:         target.MySQLQueueLimit,
			Description: queueLimitComment,
			Optional:    true,
			Type:        "number",
		},
		config.HelpKV{
			Key:         config.Comment,
			Description: config.DefaultComment,
			Optional:    true,
			Type:        "sentence",
		},
		config.HelpKV{
			Key:         target.MySQLMaxOpenConnections,
			Description: "To set the maximum number of open connections to the database. The value is set to `2` by default.",
			Optional:    true,
			Type:        "number",
		},
	}

	HelpES = config.HelpKVS{
		config.HelpKV{
			Key:         target.ElasticURL,
			Description: "Elasticsearch server's address, with optional authentication info",
			Type:        "url",
		},
		config.HelpKV{
			Key:         target.ElasticIndex,
			Description: `Elasticsearch index to store/update events, index is auto-created`,
			Type:        "string",
		},
		config.HelpKV{
			Key:         target.ElasticFormat,
			Description: formatComment,
			Type:        "namespace*|access",
		},
		config.HelpKV{
			Key:         target.ElasticQueueDir,
			Description: queueDirComment,
			Optional:    true,
			Type:        "path",
		},
		config.HelpKV{
			Key:         target.ElasticQueueLimit,
			Description: queueLimitComment,
			Optional:    true,
			Type:        "number",
		},
		config.HelpKV{
			Key:         target.ElasticUsername,
			Description: "username for Elasticsearch basic-auth",
			Optional:    true,
			Type:        "string",
		},
		config.HelpKV{
			Key:         target.ElasticPassword,
			Description: "password for Elasticsearch basic-auth",
			Optional:    true,
			Type:        "string",
		},
		config.HelpKV{
			Key:         config.Comment,
			Description: config.DefaultComment,
			Optional:    true,
			Type:        "sentence",
		},
	}

	HelpRedis = config.HelpKVS{
		config.HelpKV{
			Key:         target.RedisAddress,
			Description: "Redis server's address. For example: `localhost:6379`",
			Type:        "address",
		},
		config.HelpKV{
			Key:         target.RedisKey,
			Description: "Redis key to store/update events, key is auto-created",
			Type:        "string",
		},
		config.HelpKV{
			Key:         target.RedisFormat,
			Description: formatComment,
			Type:        "namespace*|access",
		},
		config.HelpKV{
			Key:         target.RedisPassword,
			Description: "Redis server password",
			Optional:    true,
			Type:        "string",
		},
		config.HelpKV{
			Key:         target.RedisQueueDir,
			Description: queueDirComment,
			Optional:    true,
			Type:        "path",
		},
		config.HelpKV{
			Key:         target.RedisQueueLimit,
			Description: queueLimitComment,
			Optional:    true,
			Type:        "number",
		},
		config.HelpKV{
			Key:         config.Comment,
			Description: config.DefaultComment,
			Optional:    true,
			Type:        "sentence",
		},
	}
)
