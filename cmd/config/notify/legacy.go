/*
 * MinIO Cloud Storage, (C) 2019 MinIO, Inc.
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

package notify

import (
	"strconv"

	"github.com/minio/minio/cmd/config"
	"github.com/minio/minio/pkg/event/target"
)

// SetNotifyES - helper for config migration from older config.
func SetNotifyES(s config.Config, esName string, cfg target.ElasticsearchArgs) error {
	if !cfg.Enable {
		return nil
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	s[config.NotifyESSubSys][esName] = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOn,
		},
		config.KV{
			Key:   target.ElasticFormat,
			Value: cfg.Format,
		},
		config.KV{
			Key:   target.ElasticURL,
			Value: cfg.URL.String(),
		},
		config.KV{
			Key:   target.ElasticIndex,
			Value: cfg.Index,
		},
		config.KV{
			Key:   target.ElasticQueueDir,
			Value: cfg.QueueDir,
		},
		config.KV{
			Key:   target.ElasticQueueLimit,
			Value: strconv.Itoa(int(cfg.QueueLimit)),
		},
		config.KV{
			Key:   target.ElasticUsername,
			Value: cfg.Username,
		},
		config.KV{
			Key:   target.ElasticPassword,
			Value: cfg.Password,
		},
	}

	return nil
}

// SetNotifyRedis - helper for config migration from older config.
func SetNotifyRedis(s config.Config, redisName string, cfg target.RedisArgs) error {
	if !cfg.Enable {
		return nil
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	s[config.NotifyRedisSubSys][redisName] = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOn,
		},
		config.KV{
			Key:   target.RedisFormat,
			Value: cfg.Format,
		},
		config.KV{
			Key:   target.RedisAddress,
			Value: cfg.Addr.String(),
		},
		config.KV{
			Key:   target.RedisPassword,
			Value: cfg.Password,
		},
		config.KV{
			Key:   target.RedisKey,
			Value: cfg.Key,
		},
		config.KV{
			Key:   target.RedisQueueDir,
			Value: cfg.QueueDir,
		},
		config.KV{
			Key:   target.RedisQueueLimit,
			Value: strconv.Itoa(int(cfg.QueueLimit)),
		},
	}

	return nil
}

// SetNotifyWebhook - helper for config migration from older config.
func SetNotifyWebhook(s config.Config, whName string, cfg target.WebhookArgs) error {
	if !cfg.Enable {
		return nil
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	s[config.NotifyWebhookSubSys][whName] = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOn,
		},
		config.KV{
			Key:   target.WebhookEndpoint,
			Value: cfg.Endpoint.String(),
		},
		config.KV{
			Key:   target.WebhookAuthToken,
			Value: cfg.AuthToken,
		},
		config.KV{
			Key:   target.WebhookQueueDir,
			Value: cfg.QueueDir,
		},
		config.KV{
			Key:   target.WebhookQueueLimit,
			Value: strconv.Itoa(int(cfg.QueueLimit)),
		},
		config.KV{
			Key:   target.WebhookClientCert,
			Value: cfg.ClientCert,
		},
		config.KV{
			Key:   target.WebhookClientKey,
			Value: cfg.ClientKey,
		},
	}

	return nil
}

// SetNotifyPostgres - helper for config migration from older config.
func SetNotifyPostgres(s config.Config, psqName string, cfg target.PostgreSQLArgs) error {
	if !cfg.Enable {
		return nil
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	s[config.NotifyPostgresSubSys][psqName] = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOn,
		},
		config.KV{
			Key:   target.PostgresFormat,
			Value: cfg.Format,
		},
		config.KV{
			Key:   target.PostgresConnectionString,
			Value: cfg.ConnectionString,
		},
		config.KV{
			Key:   target.PostgresTable,
			Value: cfg.Table,
		},
		config.KV{
			Key:   target.PostgresHost,
			Value: cfg.Host.String(),
		},
		config.KV{
			Key:   target.PostgresPort,
			Value: cfg.Port,
		},
		config.KV{
			Key:   target.PostgresUsername,
			Value: cfg.Username,
		},
		config.KV{
			Key:   target.PostgresPassword,
			Value: cfg.Password,
		},
		config.KV{
			Key:   target.PostgresDatabase,
			Value: cfg.Database,
		},
		config.KV{
			Key:   target.PostgresQueueDir,
			Value: cfg.QueueDir,
		},
		config.KV{
			Key:   target.PostgresQueueLimit,
			Value: strconv.Itoa(int(cfg.QueueLimit)),
		},
		config.KV{
			Key:   target.PostgresMaxOpenConnections,
			Value: strconv.Itoa(cfg.MaxOpenConnections),
		},
	}

	return nil
}

// SetNotifyMySQL - helper for config migration from older config.
func SetNotifyMySQL(s config.Config, sqlName string, cfg target.MySQLArgs) error {
	if !cfg.Enable {
		return nil
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	s[config.NotifyMySQLSubSys][sqlName] = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOn,
		},
		config.KV{
			Key:   target.MySQLFormat,
			Value: cfg.Format,
		},
		config.KV{
			Key:   target.MySQLDSNString,
			Value: cfg.DSN,
		},
		config.KV{
			Key:   target.MySQLTable,
			Value: cfg.Table,
		},
		config.KV{
			Key:   target.MySQLHost,
			Value: cfg.Host.String(),
		},
		config.KV{
			Key:   target.MySQLPort,
			Value: cfg.Port,
		},
		config.KV{
			Key:   target.MySQLUsername,
			Value: cfg.User,
		},
		config.KV{
			Key:   target.MySQLPassword,
			Value: cfg.Password,
		},
		config.KV{
			Key:   target.MySQLDatabase,
			Value: cfg.Database,
		},
		config.KV{
			Key:   target.MySQLQueueDir,
			Value: cfg.QueueDir,
		},
		config.KV{
			Key:   target.MySQLQueueLimit,
			Value: strconv.Itoa(int(cfg.QueueLimit)),
		},
		config.KV{
			Key:   target.MySQLMaxOpenConnections,
			Value: strconv.Itoa(cfg.MaxOpenConnections),
		},
	}

	return nil
}
