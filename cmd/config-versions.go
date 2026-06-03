/*
 * MinIO Cloud Storage, (C) 2016, 2017, 2018 MinIO, Inc.
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
	"sync"

	"github.com/soulteary/otterio/cmd/config"
	"github.com/soulteary/otterio/cmd/config/cache"
	"github.com/soulteary/otterio/cmd/config/compress"
	xldap "github.com/soulteary/otterio/cmd/config/identity/ldap"
	"github.com/soulteary/otterio/cmd/config/identity/openid"
	"github.com/soulteary/otterio/cmd/config/notify"
	"github.com/soulteary/otterio/cmd/config/policy/opa"
	"github.com/soulteary/otterio/cmd/config/storageclass"
	"github.com/soulteary/otterio/cmd/crypto"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/event/target"
	"github.com/soulteary/otterio/pkg/quick"
)

// ///////////////// Config V1 ///////////////////
type configV1 struct {
	Version   string `json:"version"`
	AccessKey string `json:"accessKeyId"`
	SecretKey string `json:"secretAccessKey"`
}

// ///////////////// Config V2 ///////////////////
type configV2 struct {
	Version     string `json:"version"`
	Credentials struct {
		AccessKey string `json:"accessKeyId"`
		SecretKey string `json:"secretAccessKey"`
		Region    string `json:"region"`
	} `json:"credentials"`
	MongoLogger struct {
		Addr       string `json:"addr"`
		DB         string `json:"db"`
		Collection string `json:"collection"`
	} `json:"mongoLogger"`
	SyslogLogger struct {
		Network string `json:"network"`
		Addr    string `json:"addr"`
	} `json:"syslogLogger"`
	FileLogger struct {
		Filename string `json:"filename"`
	} `json:"fileLogger"`
}

// ///////////////// Config V3 ///////////////////
// backendV3 type.
type backendV3 struct {
	Type  string   `json:"type"`
	Disk  string   `json:"disk,omitempty"`
	Disks []string `json:"disks,omitempty"`
}

// syslogLogger v3
type syslogLoggerV3 struct {
	Enable bool   `json:"enable"`
	Addr   string `json:"address"`
	Level  string `json:"level"`
}

// loggerV3 type.
type loggerV3 struct {
	Console struct {
		Enable bool   `json:"enable"`
		Level  string `json:"level"`
	}
	File struct {
		Enable   bool   `json:"enable"`
		Filename string `json:"fileName"`
		Level    string `json:"level"`
	}
	Syslog struct {
		Enable bool   `json:"enable"`
		Addr   string `json:"address"`
		Level  string `json:"level"`
	} `json:"syslog"`
	// Add new loggers here.
}

// configV3 server configuration version '3'.
type configV3 struct {
	Version string `json:"version"`

	// Backend configuration.
	Backend backendV3 `json:"backend"`

	// http Server configuration.
	Addr string `json:"address"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger loggerV3 `json:"logger"`
}

// logger type representing version '4' logger config.
type loggerV4 struct {
	Console struct {
		Enable bool   `json:"enable"`
		Level  string `json:"level"`
	} `json:"console"`
	File struct {
		Enable   bool   `json:"enable"`
		Filename string `json:"fileName"`
		Level    string `json:"level"`
	} `json:"file"`
	Syslog struct {
		Enable bool   `json:"enable"`
		Addr   string `json:"address"`
		Level  string `json:"level"`
	} `json:"syslog"`
}

// configV4 server configuration version '4'.
type configV4 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger loggerV4 `json:"logger"`
}

// logger type representing version '5' logger config.
type loggerV5 struct {
	Console struct {
		Enable bool   `json:"enable"`
		Level  string `json:"level"`
	} `json:"console"`
	File struct {
		Enable   bool   `json:"enable"`
		Filename string `json:"fileName"`
		Level    string `json:"level"`
	} `json:"file"`
	Syslog struct {
		Enable bool   `json:"enable"`
		Addr   string `json:"address"`
		Level  string `json:"level"`
	} `json:"syslog"`
	ElasticSearch struct {
		Enable bool   `json:"enable"`
		Level  string `json:"level"`
		URL    string `json:"url"`
		Index  string `json:"index"`
	} `json:"elasticsearch"`
	Redis struct {
		Enable   bool   `json:"enable"`
		Level    string `json:"level"`
		Addr     string `json:"address"`
		Password string `json:"password"`
		Key      string `json:"key"`
	} `json:"redis"`
}

// configV5 server configuration version '5'.
type configV5 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger loggerV5 `json:"logger"`
}

// consoleLogger - default logger if not other logging is enabled.
type consoleLoggerV1 struct {
	Enable bool   `json:"enable"`
	Level  string `json:"level"`
}

type fileLoggerV1 struct {
	Enable   bool   `json:"enable"`
	Filename string `json:"fileName"`
	Level    string `json:"level"`
}

type loggerV6 struct {
	Console consoleLoggerV1 `json:"console"`
	File    fileLoggerV1    `json:"file"`
	Syslog  syslogLoggerV3  `json:"syslog"`
}

// configV6 server configuration version '6'.
type configV6 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger loggerV6 `json:"logger"`

	// Notification queue configuration.
	Notify notifierV1 `json:"notify"`
}

// Notifier represents collection of supported notification queues in version 1.
type notifierV1 struct {
	ElasticSearch map[string]target.ElasticsearchArgs `json:"elasticsearch"`
	Redis         map[string]target.RedisArgs         `json:"redis"`
	PostgreSQL    map[string]target.PostgreSQLArgs    `json:"postgresql"`
}

// Notifier represents collection of supported notification queues in version 2.
type notifierV2 struct {
	ElasticSearch map[string]target.ElasticsearchArgs `json:"elasticsearch"`
	Redis         map[string]target.RedisArgs         `json:"redis"`
	PostgreSQL    map[string]target.PostgreSQLArgs    `json:"postgresql"`
}

// configV7 server configuration version '7'.
type serverConfigV7 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger loggerV6 `json:"logger"`

	// Notification queue configuration.
	Notify notifierV1 `json:"notify"`
}

// serverConfigV8 server configuration version '8'.
type serverConfigV8 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger loggerV6 `json:"logger"`

	// Notification queue configuration.
	Notify notifierV1 `json:"notify"`
}

// serverConfigV9 server configuration version '9'. Adds PostgreSQL
// notify.Config configuration.
type serverConfigV9 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger loggerV6 `json:"logger"`

	// Notification queue configuration.
	Notify notifierV1 `json:"notify"`
}

type loggerV7 struct {
	sync.RWMutex
	Console consoleLoggerV1 `json:"console"`
	File    fileLoggerV1    `json:"file"`
}

// serverConfigV10 server configuration version '10' which is like
// version '9' except it drops support of syslog config, and makes the
// RWMutex global (so it does not exist in this struct).
type serverConfigV10 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger loggerV7 `json:"logger"`

	// Notification queue configuration.
	Notify notifierV1 `json:"notify"`
}

// serverConfigV11 server configuration version '11' which is like
// version '10'.
type serverConfigV11 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger loggerV7 `json:"logger"`

	// Notification queue configuration.
	Notify notifierV1 `json:"notify"`
}

// serverConfigV12 server configuration version '12' which is like version '11'.
type serverConfigV12 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger loggerV7 `json:"logger"`

	// Notification queue configuration.
	Notify notifierV2 `json:"notify"`
}

type notifierV3 struct {
	Elasticsearch map[string]target.ElasticsearchArgs `json:"elasticsearch"`
	MySQL         map[string]target.MySQLArgs         `json:"mysql"`
	PostgreSQL    map[string]target.PostgreSQLArgs    `json:"postgresql"`
	Redis         map[string]target.RedisArgs         `json:"redis"`
	Webhook       map[string]target.WebhookArgs       `json:"webhook"`
}

// serverConfigV13 server configuration version '13' which is like
// version '12' except it adds support for webhook notification.
type serverConfigV13 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`

	// Additional error logging configuration.
	Logger *loggerV7 `json:"logger"`

	// Notification queue configuration.
	Notify *notifierV3 `json:"notify"`
}

// serverConfigV14 server configuration version '14' which is like
// version '13' except it adds support of browser param.
type serverConfigV14 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`

	// Additional error logging configuration.
	Logger *loggerV7 `json:"logger"`

	// Notification queue configuration.
	Notify *notifierV3 `json:"notify"`
}

// serverConfigV15 server configuration version '15' which is like
// version '14' except it adds mysql support
type serverConfigV15 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`

	// Additional error logging configuration.
	Logger *loggerV7 `json:"logger"`

	// Notification queue configuration.
	Notify *notifierV3 `json:"notify"`
}

// FileLogger is introduced to workaround the dependency about logrus
type FileLogger struct {
	Enable   bool   `json:"enable"`
	Filename string `json:"filename"`
}

// ConsoleLogger is introduced to workaround the dependency about logrus
type ConsoleLogger struct {
	Enable bool `json:"enable"`
}

// Loggers struct is defined with FileLogger and ConsoleLogger
// although they are removed from logging logic. They are
// kept here just to workaround the dependency migration
// code/logic has on them.
type loggers struct {
	sync.RWMutex
	Console ConsoleLogger `json:"console"`
	File    FileLogger    `json:"file"`
}

// serverConfigV16 server configuration version '16' which is like
// version '15' except it makes a change to logging configuration.
type serverConfigV16 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`

	// Additional error logging configuration.
	Logger *loggers `json:"logger"`

	// Notification queue configuration.
	Notify *notifierV3 `json:"notify"`
}

// serverConfigV17 server configuration version '17' which is like
// version '16' except it adds support for "format" parameter in
// database event notification targets: PostgreSQL, MySQL, Redis and
// Elasticsearch.
type serverConfigV17 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`

	// Additional error logging configuration.
	Logger *loggers `json:"logger"`

	// Notification queue configuration.
	Notify *notifierV3 `json:"notify"`
}

// serverConfigV18 server configuration version '18' which is like
// version '17'.
type serverConfigV18 struct {
	sync.RWMutex
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`

	// Additional error logging configuration.
	Logger *loggers `json:"logger"`

	// Notification queue configuration.
	Notify *notifierV3 `json:"notify"`
}

// serverConfigV19 server configuration version '19' which is like
// version '18' except it adds support for MQTT notifications.
type serverConfigV19 struct {
	sync.RWMutex
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`

	// Additional error logging configuration.
	Logger *loggers `json:"logger"`

	// Notification queue configuration.
	Notify *notifierV3 `json:"notify"`
}

// serverConfigV20 server configuration version '20' which is like
// version '19' except it adds support for VirtualHostDomain
type serverConfigV20 struct {
	sync.RWMutex
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`
	Domain     string           `json:"domain"`

	// Additional error logging configuration.
	Logger *loggers `json:"logger"`

	// Notification queue configuration.
	Notify *notifierV3 `json:"notify"`
}

// serverConfigV21 is just like version '20' without logger field
type serverConfigV21 struct {
	sync.RWMutex
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`
	Domain     string           `json:"domain"`

	// Notification queue configuration.
	Notify *notifierV3 `json:"notify"`
}

// serverConfigV22 is just like version '21' with added support
// for StorageClass.
type serverConfigV22 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`
	Domain     string           `json:"domain"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`
}

// serverConfigV23 is just like version '22' with addition of cache field.
type serverConfigV23 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`
	Domain     string           `json:"domain"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`
}

// serverConfigV24 is just like version '23', we had to revert
// the changes which were made in 6fb06045028b7a57c37c60a612c8e50735279ab4
type serverConfigV24 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`
	Domain     string           `json:"domain"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`
}

// serverConfigV25 is just like version '24', stores additionally
// worm variable.
type serverConfigV25 struct {
	quick.Config `json:"-"` // ignore interfaces

	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`
	Worm       config.BoolFlag  `json:"worm"`
	Domain     string           `json:"domain"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`
}

// serverConfigV26 is just like version '25', stores additionally
// cache max use value in 'cache.Config'.
type serverConfigV26 struct {
	quick.Config `json:"-"` // ignore interfaces

	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`
	Worm       config.BoolFlag  `json:"worm"`
	Domain     string           `json:"domain"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`
}

// serverConfigV27 is just like version '26', stores additionally
// the logger field
type serverConfigV27 struct {
	quick.Config `json:"-"` // ignore interfaces

	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`
	Worm       config.BoolFlag  `json:"worm"`
	Domain     string           `json:"domain"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`
}

// serverConfigV28 is just like version '27', additionally
// storing KMS config
type serverConfigV28 struct {
	quick.Config `json:"-"` // ignore interfaces

	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Worm       config.BoolFlag  `json:"worm"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// KMS configuration
	KMS crypto.KMSConfig `json:"kms"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`
}

// serverConfigV29 is just like version '28'.
type serverConfigV29 serverConfigV28

// serverConfigV30 is just like version '29', stores additionally
// extensions and mimetypes fields for compression.
type serverConfigV30 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Worm       config.BoolFlag  `json:"worm"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// KMS configuration
	KMS crypto.KMSConfig `json:"kms"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`

	// Compression configuration
	Compression compress.Config `json:"compress"`
}

// serverConfigV31 is just like version '30', with OPA and OpenID configuration.
type serverConfigV31 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Worm       config.BoolFlag  `json:"worm"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// KMS configuration
	KMS crypto.KMSConfig `json:"kms"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`

	// Compression configuration
	Compression compress.Config `json:"compress"`

	// OpenID configuration
	OpenID openid.Config `json:"openid"`

	// External policy enforcements.
	Policy struct {
		// OPA configuration.
		OPA opa.Args `json:"opa"`

		// Add new external policy enforcements here.
	} `json:"policy"`
}

// serverConfigV32 is just like version '31' with updated notification config.
type serverConfigV32 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Worm       config.BoolFlag  `json:"worm"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// KMS configuration
	KMS crypto.KMSConfig `json:"kms"`

	// Notification queue configuration.
	Notify notify.Config `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`

	// Compression configuration
	Compression compress.Config `json:"compress"`

	// OpenID configuration
	OpenID openid.Config `json:"openid"`

	// External policy enforcements.
	Policy struct {
		// OPA configuration.
		OPA opa.Args `json:"opa"`

		// Add new external policy enforcements here.
	} `json:"policy"`
}

// serverConfigV33 is just like version '32', removes clientID from MQTT, and adds queueDir, queueLimit in all notification targets.
type serverConfigV33 struct {
	quick.Config `json:"-"` // ignore interfaces

	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Worm       config.BoolFlag  `json:"worm"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// KMS configuration
	KMS crypto.KMSConfig `json:"kms"`

	// Notification queue configuration.
	Notify notify.Config `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`

	// Compression configuration
	Compression compress.Config `json:"compress"`

	// OpenID configuration
	OpenID openid.Config `json:"openid"`

	// External policy enforcements.
	Policy struct {
		// OPA configuration.
		OPA opa.Args `json:"opa"`

		// Add new external policy enforcements here.
	} `json:"policy"`

	LDAPServerConfig xldap.Config `json:"ldapserverconfig"`
}
