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
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/soulteary/otterio/cmd/config"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/env"
	"github.com/soulteary/otterio/pkg/event"
	"github.com/soulteary/otterio/pkg/event/target"
	xnet "github.com/soulteary/otterio/pkg/net"
)

const (
	formatNamespace = "namespace"
)

// ErrTargetsOffline - Indicates single/multiple target failures.
var ErrTargetsOffline = errors.New("one or more targets are offline. Please use `mc admin info --json` to check the offline targets")

// TestNotificationTargets is similar to GetNotificationTargets()
// avoids explicit registration.
func TestNotificationTargets(ctx context.Context, cfg config.Config, transport *http.Transport, targetIDs []event.TargetID) error {
	test := true
	returnOnTargetError := true
	targets, err := RegisterNotificationTargets(ctx, cfg, transport, targetIDs, test, returnOnTargetError)
	if err == nil {
		// Close all targets since we are only testing connections.
		for _, t := range targets.TargetMap() {
			_ = t.Close()
		}
	}

	return err
}

// GetNotificationTargets registers and initializes all notification
// targets, returns error if any.
func GetNotificationTargets(ctx context.Context, cfg config.Config, transport *http.Transport, test bool) (*event.TargetList, error) {
	returnOnTargetError := false
	return RegisterNotificationTargets(ctx, cfg, transport, nil, test, returnOnTargetError)
}

// RegisterNotificationTargets - returns TargetList which contains enabled targets in serverConfig.
// A new notification target is added like below
// * Add a new target in pkg/event/target package.
// * Add newly added target configuration to serverConfig.Notify.<TARGET_NAME>.
// * Handle the configuration in this function to create/add into TargetList.
func RegisterNotificationTargets(ctx context.Context, cfg config.Config, transport *http.Transport, targetIDs []event.TargetID, test bool, returnOnTargetError bool) (*event.TargetList, error) {
	targetList, err := FetchRegisteredTargets(ctx, cfg, transport, test, returnOnTargetError)
	if err != nil {
		return targetList, err
	}

	if test {
		// Verify if user is trying to disable already configured
		// notification targets, based on their target IDs
		for _, targetID := range targetIDs {
			if !targetList.Exists(targetID) {
				return nil, config.Errorf(
					"Unable to disable configured targets '%v'",
					targetID)
			}
		}
	}

	return targetList, nil
}

// FetchRegisteredTargets - Returns a set of configured TargetList
// If `returnOnTargetError` is set to true, The function returns when a target initialization fails
// Else, the function will return a complete TargetList irrespective of errors
func FetchRegisteredTargets(ctx context.Context, cfg config.Config, transport *http.Transport, test bool, returnOnTargetError bool) (_ *event.TargetList, err error) {
	targetList := event.NewTargetList()
	var targetsOffline bool

	defer func() {
		// Automatically close all connections to targets when an error occur.
		// Close all the targets if returnOnTargetError is set
		// Else, close only the failed targets
		if err != nil && returnOnTargetError {
			for _, t := range targetList.TargetMap() {
				_ = t.Close()
			}
		}
	}()

	if err = checkValidNotificationKeys(cfg); err != nil {
		return nil, err
	}

	esTargets, err := GetNotifyES(cfg[config.NotifyESSubSys], transport)
	if err != nil {
		return nil, err
	}

	mysqlTargets, err := GetNotifyMySQL(cfg[config.NotifyMySQLSubSys])
	if err != nil {
		return nil, err
	}

	postgresTargets, err := GetNotifyPostgres(cfg[config.NotifyPostgresSubSys])
	if err != nil {
		return nil, err
	}

	redisTargets, err := GetNotifyRedis(cfg[config.NotifyRedisSubSys])
	if err != nil {
		return nil, err
	}

	webhookTargets, err := GetNotifyWebhook(cfg[config.NotifyWebhookSubSys], transport)
	if err != nil {
		return nil, err
	}

	for id, args := range esTargets {
		if !args.Enable {
			continue
		}
		newTarget, err := target.NewElasticsearchTarget(id, args, ctx.Done(), logger.LogOnceIf, test)
		if err != nil {
			targetsOffline = true
			if returnOnTargetError {
				return nil, err
			}
			_ = newTarget.Close()
		}
		if err = targetList.Add(newTarget); err != nil {
			logger.LogIf(context.Background(), err)
			if returnOnTargetError {
				return nil, err
			}
		}
	}

	for id, args := range mysqlTargets {
		if !args.Enable {
			continue
		}
		newTarget, err := target.NewMySQLTarget(id, args, ctx.Done(), logger.LogOnceIf, test)
		if err != nil {
			targetsOffline = true
			if returnOnTargetError {
				return nil, err
			}
			_ = newTarget.Close()
		}
		if err = targetList.Add(newTarget); err != nil {
			logger.LogIf(context.Background(), err)
			if returnOnTargetError {
				return nil, err
			}
		}
	}

	for id, args := range postgresTargets {
		if !args.Enable {
			continue
		}
		newTarget, err := target.NewPostgreSQLTarget(id, args, ctx.Done(), logger.LogOnceIf, test)
		if err != nil {
			targetsOffline = true
			if returnOnTargetError {
				return nil, err
			}
			_ = newTarget.Close()
		}
		if err = targetList.Add(newTarget); err != nil {
			logger.LogIf(context.Background(), err)
			if returnOnTargetError {
				return nil, err
			}
		}
	}

	for id, args := range redisTargets {
		if !args.Enable {
			continue
		}
		newTarget, err := target.NewRedisTarget(id, args, ctx.Done(), logger.LogOnceIf, test)
		if err != nil {
			targetsOffline = true
			if returnOnTargetError {
				return nil, err
			}
			_ = newTarget.Close()
		}
		if err = targetList.Add(newTarget); err != nil {
			logger.LogIf(context.Background(), err)
			if returnOnTargetError {
				return nil, err
			}
		}
	}

	for id, args := range webhookTargets {
		if !args.Enable {
			continue
		}
		newTarget, err := target.NewWebhookTarget(ctx, id, args, logger.LogOnceIf, transport, test)
		if err != nil {
			targetsOffline = true
			if returnOnTargetError {
				return nil, err
			}
			_ = newTarget.Close()
		}
		if err = targetList.Add(newTarget); err != nil {
			logger.LogIf(context.Background(), err)
			if returnOnTargetError {
				return nil, err
			}
		}
	}

	if targetsOffline {
		return targetList, ErrTargetsOffline
	}

	return targetList, nil
}

// DefaultNotificationKVS - default notification list of kvs.
var (
	DefaultNotificationKVS = map[string]config.KVS{
		config.NotifyMySQLSubSys:    DefaultMySQLKVS,
		config.NotifyPostgresSubSys: DefaultPostgresKVS,
		config.NotifyRedisSubSys:    DefaultRedisKVS,
		config.NotifyWebhookSubSys:  DefaultWebhookKVS,
		config.NotifyESSubSys:       DefaultESKVS,
	}
)

func checkValidNotificationKeys(cfg config.Config) error {
	for subSys, tgt := range cfg {
		validKVS, ok := DefaultNotificationKVS[subSys]
		if !ok {
			continue
		}
		for tname, kv := range tgt {
			subSysTarget := subSys
			if tname != config.Default {
				subSysTarget = subSys + config.SubSystemSeparator + tname
			}
			if v, ok := kv.Lookup(config.Enable); ok && v == config.EnableOn {
				if err := config.CheckValidKeys(subSysTarget, kv, validKVS); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func mergeTargets(cfgTargets map[string]config.KVS, envname string, defaultKVS config.KVS) map[string]config.KVS {
	newCfgTargets := make(map[string]config.KVS)
	for _, e := range env.List(envname) {
		tgt := strings.TrimPrefix(e, envname+config.Default)
		if tgt == envname {
			tgt = config.Default
		}
		newCfgTargets[tgt] = defaultKVS
	}
	for tgt, kv := range cfgTargets {
		newCfgTargets[tgt] = kv
	}
	return newCfgTargets
}

// DefaultMySQLKVS - default KV for MySQL
var (
	DefaultMySQLKVS = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOff,
		},
		config.KV{
			Key:   target.MySQLFormat,
			Value: formatNamespace,
		},
		config.KV{
			Key:   target.MySQLDSNString,
			Value: "",
		},
		config.KV{
			Key:   target.MySQLTable,
			Value: "",
		},
		config.KV{
			Key:   target.MySQLQueueDir,
			Value: "",
		},
		config.KV{
			Key:   target.MySQLQueueLimit,
			Value: "0",
		},
		config.KV{
			Key:   target.MySQLMaxOpenConnections,
			Value: "2",
		},
	}
)

// GetNotifyMySQL - returns a map of registered notification 'mysql' targets
func GetNotifyMySQL(mysqlKVS map[string]config.KVS) (map[string]target.MySQLArgs, error) {
	mysqlTargets := make(map[string]target.MySQLArgs)
	for k, kv := range mergeTargets(mysqlKVS, target.EnvMySQLEnable, DefaultMySQLKVS) {
		enableEnv := target.EnvMySQLEnable
		if k != config.Default {
			enableEnv = enableEnv + config.Default + k
		}

		enabled, err := config.ParseBool(env.Get(enableEnv, kv.Get(config.Enable)))
		if err != nil {
			return nil, err
		}
		if !enabled {
			continue
		}

		queueLimitEnv := target.EnvMySQLQueueLimit
		if k != config.Default {
			queueLimitEnv = queueLimitEnv + config.Default + k
		}
		queueLimit, err := strconv.ParseUint(env.Get(queueLimitEnv, kv.Get(target.MySQLQueueLimit)), 10, 64)
		if err != nil {
			return nil, err
		}

		formatEnv := target.EnvMySQLFormat
		if k != config.Default {
			formatEnv = formatEnv + config.Default + k
		}

		dsnStringEnv := target.EnvMySQLDSNString
		if k != config.Default {
			dsnStringEnv = dsnStringEnv + config.Default + k
		}

		tableEnv := target.EnvMySQLTable
		if k != config.Default {
			tableEnv = tableEnv + config.Default + k
		}

		queueDirEnv := target.EnvMySQLQueueDir
		if k != config.Default {
			queueDirEnv = queueDirEnv + config.Default + k
		}

		maxOpenConnectionsEnv := target.EnvMySQLMaxOpenConnections
		if k != config.Default {
			maxOpenConnectionsEnv = maxOpenConnectionsEnv + config.Default + k
		}

		maxOpenConnections, cErr := strconv.Atoi(env.Get(maxOpenConnectionsEnv, kv.Get(target.MySQLMaxOpenConnections)))
		if cErr != nil {
			return nil, cErr
		}

		mysqlArgs := target.MySQLArgs{
			Enable:             enabled,
			Format:             env.Get(formatEnv, kv.Get(target.MySQLFormat)),
			DSN:                env.Get(dsnStringEnv, kv.Get(target.MySQLDSNString)),
			Table:              env.Get(tableEnv, kv.Get(target.MySQLTable)),
			QueueDir:           env.Get(queueDirEnv, kv.Get(target.MySQLQueueDir)),
			QueueLimit:         queueLimit,
			MaxOpenConnections: maxOpenConnections,
		}
		if err = mysqlArgs.Validate(); err != nil {
			return nil, err
		}
		mysqlTargets[k] = mysqlArgs
	}
	return mysqlTargets, nil
}

// DefaultPostgresKVS - default Postgres KV for server config.
var (
	DefaultPostgresKVS = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOff,
		},
		config.KV{
			Key:   target.PostgresFormat,
			Value: formatNamespace,
		},
		config.KV{
			Key:   target.PostgresConnectionString,
			Value: "",
		},
		config.KV{
			Key:   target.PostgresTable,
			Value: "",
		},
		config.KV{
			Key:   target.PostgresQueueDir,
			Value: "",
		},
		config.KV{
			Key:   target.PostgresQueueLimit,
			Value: "0",
		},
		config.KV{
			Key:   target.PostgresMaxOpenConnections,
			Value: "2",
		},
	}
)

// GetNotifyPostgres - returns a map of registered notification 'postgres' targets
func GetNotifyPostgres(postgresKVS map[string]config.KVS) (map[string]target.PostgreSQLArgs, error) {
	psqlTargets := make(map[string]target.PostgreSQLArgs)
	for k, kv := range mergeTargets(postgresKVS, target.EnvPostgresEnable, DefaultPostgresKVS) {
		enableEnv := target.EnvPostgresEnable
		if k != config.Default {
			enableEnv = enableEnv + config.Default + k
		}

		enabled, err := config.ParseBool(env.Get(enableEnv, kv.Get(config.Enable)))
		if err != nil {
			return nil, err
		}
		if !enabled {
			continue
		}

		queueLimitEnv := target.EnvPostgresQueueLimit
		if k != config.Default {
			queueLimitEnv = queueLimitEnv + config.Default + k
		}

		queueLimit, err := strconv.Atoi(env.Get(queueLimitEnv, kv.Get(target.PostgresQueueLimit)))
		if err != nil {
			return nil, err
		}

		formatEnv := target.EnvPostgresFormat
		if k != config.Default {
			formatEnv = formatEnv + config.Default + k
		}

		connectionStringEnv := target.EnvPostgresConnectionString
		if k != config.Default {
			connectionStringEnv = connectionStringEnv + config.Default + k
		}

		tableEnv := target.EnvPostgresTable
		if k != config.Default {
			tableEnv = tableEnv + config.Default + k
		}

		queueDirEnv := target.EnvPostgresQueueDir
		if k != config.Default {
			queueDirEnv = queueDirEnv + config.Default + k
		}

		maxOpenConnectionsEnv := target.EnvPostgresMaxOpenConnections
		if k != config.Default {
			maxOpenConnectionsEnv = maxOpenConnectionsEnv + config.Default + k
		}

		maxOpenConnections, cErr := strconv.Atoi(env.Get(maxOpenConnectionsEnv, kv.Get(target.PostgresMaxOpenConnections)))
		if cErr != nil {
			return nil, cErr
		}

		psqlArgs := target.PostgreSQLArgs{
			Enable:             enabled,
			Format:             env.Get(formatEnv, kv.Get(target.PostgresFormat)),
			ConnectionString:   env.Get(connectionStringEnv, kv.Get(target.PostgresConnectionString)),
			Table:              env.Get(tableEnv, kv.Get(target.PostgresTable)),
			QueueDir:           env.Get(queueDirEnv, kv.Get(target.PostgresQueueDir)),
			QueueLimit:         uint64(queueLimit),
			MaxOpenConnections: maxOpenConnections,
		}
		if err = psqlArgs.Validate(); err != nil {
			return nil, err
		}
		psqlTargets[k] = psqlArgs
	}

	return psqlTargets, nil
}

// DefaultRedisKVS - default KV for redis config
var (
	DefaultRedisKVS = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOff,
		},
		config.KV{
			Key:   target.RedisFormat,
			Value: formatNamespace,
		},
		config.KV{
			Key:   target.RedisAddress,
			Value: "",
		},
		config.KV{
			Key:   target.RedisKey,
			Value: "",
		},
		config.KV{
			Key:   target.RedisPassword,
			Value: "",
		},
		config.KV{
			Key:   target.RedisQueueDir,
			Value: "",
		},
		config.KV{
			Key:   target.RedisQueueLimit,
			Value: "0",
		},
	}
)

// GetNotifyRedis - returns a map of registered notification 'redis' targets
func GetNotifyRedis(redisKVS map[string]config.KVS) (map[string]target.RedisArgs, error) {
	redisTargets := make(map[string]target.RedisArgs)
	for k, kv := range mergeTargets(redisKVS, target.EnvRedisEnable, DefaultRedisKVS) {
		enableEnv := target.EnvRedisEnable
		if k != config.Default {
			enableEnv = enableEnv + config.Default + k
		}

		enabled, err := config.ParseBool(env.Get(enableEnv, kv.Get(config.Enable)))
		if err != nil {
			return nil, err
		}
		if !enabled {
			continue
		}

		addressEnv := target.EnvRedisAddress
		if k != config.Default {
			addressEnv = addressEnv + config.Default + k
		}
		addr, err := xnet.ParseHost(env.Get(addressEnv, kv.Get(target.RedisAddress)))
		if err != nil {
			return nil, err
		}
		queueLimitEnv := target.EnvRedisQueueLimit
		if k != config.Default {
			queueLimitEnv = queueLimitEnv + config.Default + k
		}
		queueLimit, err := strconv.Atoi(env.Get(queueLimitEnv, kv.Get(target.RedisQueueLimit)))
		if err != nil {
			return nil, err
		}
		formatEnv := target.EnvRedisFormat
		if k != config.Default {
			formatEnv = formatEnv + config.Default + k
		}
		passwordEnv := target.EnvRedisPassword
		if k != config.Default {
			passwordEnv = passwordEnv + config.Default + k
		}
		keyEnv := target.EnvRedisKey
		if k != config.Default {
			keyEnv = keyEnv + config.Default + k
		}
		queueDirEnv := target.EnvRedisQueueDir
		if k != config.Default {
			queueDirEnv = queueDirEnv + config.Default + k
		}
		redisArgs := target.RedisArgs{
			Enable:     enabled,
			Format:     env.Get(formatEnv, kv.Get(target.RedisFormat)),
			Addr:       *addr,
			Password:   env.Get(passwordEnv, kv.Get(target.RedisPassword)),
			Key:        env.Get(keyEnv, kv.Get(target.RedisKey)),
			QueueDir:   env.Get(queueDirEnv, kv.Get(target.RedisQueueDir)),
			QueueLimit: uint64(queueLimit),
		}
		if err = redisArgs.Validate(); err != nil {
			return nil, err
		}
		redisTargets[k] = redisArgs
	}
	return redisTargets, nil
}

// DefaultWebhookKVS - default KV for webhook config
var (
	DefaultWebhookKVS = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOff,
		},
		config.KV{
			Key:   target.WebhookEndpoint,
			Value: "",
		},
		config.KV{
			Key:   target.WebhookAuthToken,
			Value: "",
		},
		config.KV{
			Key:   target.WebhookQueueLimit,
			Value: "0",
		},
		config.KV{
			Key:   target.WebhookQueueDir,
			Value: "",
		},
		config.KV{
			Key:   target.WebhookClientCert,
			Value: "",
		},
		config.KV{
			Key:   target.WebhookClientKey,
			Value: "",
		},
	}
)

// GetNotifyWebhook - returns a map of registered notification 'webhook' targets
func GetNotifyWebhook(webhookKVS map[string]config.KVS, transport *http.Transport) (
	map[string]target.WebhookArgs, error) {
	webhookTargets := make(map[string]target.WebhookArgs)
	for k, kv := range mergeTargets(webhookKVS, target.EnvWebhookEnable, DefaultWebhookKVS) {
		enableEnv := target.EnvWebhookEnable
		if k != config.Default {
			enableEnv = enableEnv + config.Default + k
		}
		enabled, err := config.ParseBool(env.Get(enableEnv, kv.Get(config.Enable)))
		if err != nil {
			return nil, err
		}
		if !enabled {
			continue
		}
		urlEnv := target.EnvWebhookEndpoint
		if k != config.Default {
			urlEnv = urlEnv + config.Default + k
		}
		url, err := xnet.ParseHTTPURL(env.Get(urlEnv, kv.Get(target.WebhookEndpoint)))
		if err != nil {
			return nil, err
		}
		queueLimitEnv := target.EnvWebhookQueueLimit
		if k != config.Default {
			queueLimitEnv = queueLimitEnv + config.Default + k
		}
		queueLimit, err := strconv.Atoi(env.Get(queueLimitEnv, kv.Get(target.WebhookQueueLimit)))
		if err != nil {
			return nil, err
		}
		queueDirEnv := target.EnvWebhookQueueDir
		if k != config.Default {
			queueDirEnv = queueDirEnv + config.Default + k
		}
		authEnv := target.EnvWebhookAuthToken
		if k != config.Default {
			authEnv = authEnv + config.Default + k
		}
		clientCertEnv := target.EnvWebhookClientCert
		if k != config.Default {
			clientCertEnv = clientCertEnv + config.Default + k
		}

		clientKeyEnv := target.EnvWebhookClientKey
		if k != config.Default {
			clientKeyEnv = clientKeyEnv + config.Default + k
		}

		webhookArgs := target.WebhookArgs{
			Enable:     enabled,
			Endpoint:   *url,
			Transport:  transport,
			AuthToken:  env.Get(authEnv, kv.Get(target.WebhookAuthToken)),
			QueueDir:   env.Get(queueDirEnv, kv.Get(target.WebhookQueueDir)),
			QueueLimit: uint64(queueLimit),
			ClientCert: env.Get(clientCertEnv, kv.Get(target.WebhookClientCert)),
			ClientKey:  env.Get(clientKeyEnv, kv.Get(target.WebhookClientKey)),
		}
		if err = webhookArgs.Validate(); err != nil {
			return nil, err
		}
		webhookTargets[k] = webhookArgs
	}
	return webhookTargets, nil
}

// DefaultESKVS - default KV config for Elasticsearch target
var (
	DefaultESKVS = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOff,
		},
		config.KV{
			Key:   target.ElasticURL,
			Value: "",
		},
		config.KV{
			Key:   target.ElasticFormat,
			Value: formatNamespace,
		},
		config.KV{
			Key:   target.ElasticIndex,
			Value: "",
		},
		config.KV{
			Key:   target.ElasticQueueDir,
			Value: "",
		},
		config.KV{
			Key:   target.ElasticQueueLimit,
			Value: "0",
		},
		config.KV{
			Key:   target.ElasticUsername,
			Value: "",
		},
		config.KV{
			Key:   target.ElasticPassword,
			Value: "",
		},
	}
)

// GetNotifyES - returns a map of registered notification 'elasticsearch' targets
func GetNotifyES(esKVS map[string]config.KVS, transport *http.Transport) (map[string]target.ElasticsearchArgs, error) {
	esTargets := make(map[string]target.ElasticsearchArgs)
	for k, kv := range mergeTargets(esKVS, target.EnvElasticEnable, DefaultESKVS) {
		enableEnv := target.EnvElasticEnable
		if k != config.Default {
			enableEnv = enableEnv + config.Default + k
		}
		enabled, err := config.ParseBool(env.Get(enableEnv, kv.Get(config.Enable)))
		if err != nil {
			return nil, err
		}
		if !enabled {
			continue
		}

		urlEnv := target.EnvElasticURL
		if k != config.Default {
			urlEnv = urlEnv + config.Default + k
		}

		url, err := xnet.ParseHTTPURL(env.Get(urlEnv, kv.Get(target.ElasticURL)))
		if err != nil {
			return nil, err
		}

		queueLimitEnv := target.EnvElasticQueueLimit
		if k != config.Default {
			queueLimitEnv = queueLimitEnv + config.Default + k
		}

		queueLimit, err := strconv.Atoi(env.Get(queueLimitEnv, kv.Get(target.ElasticQueueLimit)))
		if err != nil {
			return nil, err
		}

		formatEnv := target.EnvElasticFormat
		if k != config.Default {
			formatEnv = formatEnv + config.Default + k
		}

		indexEnv := target.EnvElasticIndex
		if k != config.Default {
			indexEnv = indexEnv + config.Default + k
		}

		queueDirEnv := target.EnvElasticQueueDir
		if k != config.Default {
			queueDirEnv = queueDirEnv + config.Default + k
		}

		usernameEnv := target.EnvElasticUsername
		if k != config.Default {
			usernameEnv = usernameEnv + config.Default + k
		}

		passwordEnv := target.EnvElasticPassword
		if k != config.Default {
			passwordEnv = passwordEnv + config.Default + k
		}

		esArgs := target.ElasticsearchArgs{
			Enable:     enabled,
			Format:     env.Get(formatEnv, kv.Get(target.ElasticFormat)),
			URL:        *url,
			Index:      env.Get(indexEnv, kv.Get(target.ElasticIndex)),
			QueueDir:   env.Get(queueDirEnv, kv.Get(target.ElasticQueueDir)),
			QueueLimit: uint64(queueLimit),
			Transport:  transport,
			Username:   env.Get(usernameEnv, kv.Get(target.ElasticUsername)),
			Password:   env.Get(passwordEnv, kv.Get(target.ElasticPassword)),
		}
		if err = esArgs.Validate(); err != nil {
			return nil, err
		}
		esTargets[k] = esArgs
	}
	return esTargets, nil
}
