// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package config — конфигурация kacho-storage из переменных окружения через corelib
// config.LoadPrefixed("KACHO_STORAGE"). Per-edge TLS-структуры (grpcclient.TLSClient
// / grpcsrv.TLSServer) получают независимые имена KACHO_STORAGE_<EDGE>_<NAME> —
// префикс на каждое ребро, без общего TLS-синглтона на процесс.
package config

import (
	"fmt"
	"time"

	"google.golang.org/grpc"

	corecfg "github.com/PRO-Robotech/kacho/pkg/config"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// envPrefix — корневой сегмент env-имён kacho-storage (KACHO_<DOMAIN>).
const envPrefix = "KACHO_STORAGE"

// DBSchema — Postgres-схема (database-per-service): своя БД kacho_storage.
const DBSchema = "kacho_storage"

// Config — конфигурация kacho-storage.
type Config struct {
	DBHost     string `envconfig:"KACHO_STORAGE_DB_HOST" default:"localhost"`
	DBPort     string `envconfig:"KACHO_STORAGE_DB_PORT" default:"5432"`
	DBUser     string `envconfig:"KACHO_STORAGE_DB_USER" default:"storage"`
	DBPassword string `envconfig:"KACHO_STORAGE_DB_PASSWORD" required:"true"`
	DBName     string `envconfig:"KACHO_STORAGE_DB_NAME" default:"kacho_storage"`
	// DBSSLMode — sslmode для DSN. dev по умолчанию disable; в проде обязателен
	// require|verify-ca|verify-full.
	DBSSLMode string `envconfig:"KACHO_STORAGE_DB_SSLMODE" default:"disable"`
	// DBMaxConns — лимит pgx-пула (0 = дефолт pgx max(4, NumCPU)).
	DBMaxConns int `envconfig:"KACHO_STORAGE_DB_MAX_CONNS" default:"0"`

	// GrpcPort — публичный листенер (Volume/Snapshot/DiskType Service).
	GrpcPort string `envconfig:"KACHO_STORAGE_GRPC_PORT" default:"9090"`
	// InternalGrpcPort — cluster-internal листенер (InternalVolume/InternalDiskType).
	// Не выставляется на внешнем endpoint api-gateway — только cluster-internal.
	InternalGrpcPort string `envconfig:"KACHO_STORAGE_INTERNAL_PORT" default:"9091"`
	// MetricsAddr — адрес cluster-internal diagnostic HTTP-listener'а (/healthz,
	// /metrics). Пустое значение явно отключает listener (back-compat).
	MetricsAddr string `envconfig:"KACHO_STORAGE_METRICS_ADDR" default:":9095"`

	// AuthMode — fail-closed режим: dev | production | production-strict.
	AuthMode string `envconfig:"KACHO_STORAGE_AUTH_MODE" default:"production"`

	// ===== peer-адреса (runtime cross-domain edges) =====

	// GeoGRPCAddr — public endpoint kacho-geo для валидации zone_id
	// (ребро storage→geo, ZoneService.Get). Пусто → GeoClient fail-closed.
	GeoGRPCAddr string `envconfig:"KACHO_STORAGE_GEO_GRPC_ADDR" default:""`
	// IAMGRPCAddr — endpoint kacho-iam для валидации project_id
	// (ребро storage→iam, ProjectService.Get). Пусто → IAMClient fail-closed.
	IAMGRPCAddr string `envconfig:"KACHO_STORAGE_IAM_GRPC_ADDR" default:""`
	// AuthZIAMGRPCAddr — internal endpoint kacho-iam для per-RPC Check
	// (ребро storage→iam authz, InternalIAMService.Check). Пусто → authz-интерсептор
	// не подключается (грациозный dev-старт без kacho-iam). Тот же endpoint несёт
	// InternalIAMService.RegisterResource/UnregisterResource (FGA-proxy, Internal-only
	// :9091) — его переиспользует register-drainer + sync-registrar.
	AuthZIAMGRPCAddr string `envconfig:"KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR" default:""`

	// FGARegisterDrainerEnabled — включает register-drainer owner-tuple'ов (SEC-D):
	// применяет fga_register_outbox-intents через kacho-iam RegisterResource/
	// UnregisterResource по ребру storage→iam (AuthZIAMGRPCAddr, mTLS). Default true;
	// без него созданные Volume/Snapshot не получают owner-tuple → анти-BOLA
	// scope_extractor не резолвит target→project. false → intents копятся
	// неприменёнными (dev/degraded). Требует непустой AuthZIAMGRPCAddr.
	FGARegisterDrainerEnabled bool `envconfig:"KACHO_STORAGE_FGA_REGISTER_DRAINER_ENABLED" default:"true"`

	// OwnerConfirmDeadline — верхняя граница ожидания read-after-register confirm
	// owner-tuple в Volume.Create (opgate P5): Create-Operation достигает
	// success-`done` только после подтверждения owner-tuple в FGA (creator ↦ editor@
	// storage_volume:<id>). Не достигнуто за deadline → op.error UNAVAILABLE
	// "owner-tuple registration not confirmed" (fail-closed; ресурс/intent durable,
	// drainer добивает at-least-once). Должно быть ≫ нормальной FGA-пропагации и ≪
	// operation max-lifetime / OrphanGrace (см. operations.defaultConfirmDeadline).
	OwnerConfirmDeadline time.Duration `envconfig:"KACHO_STORAGE_OWNER_CONFIRM_DEADLINE" default:"30s"`

	// ===== per-edge mTLS =====

	// GeoClientMTLS — client-creds ребра storage→geo (:9090).
	GeoClientMTLS grpcclient.TLSClient `envconfig:"GEO_CLIENT_MTLS"`
	// IAMClientMTLS — client-creds ребра storage→iam (:9090 / :9091 authz).
	IAMClientMTLS grpcclient.TLSClient `envconfig:"IAM_CLIENT_MTLS"`
	// PublicServerMTLS — server-creds публичного листенера (:9090).
	PublicServerMTLS grpcsrv.TLSServer `envconfig:"PUBLIC_SERVER_MTLS"`
	// InternalServerMTLS — server-creds cluster-internal листенера (:9091).
	InternalServerMTLS grpcsrv.TLSServer `envconfig:"INTERNAL_SERVER_MTLS"`
}

// PublicServerCreds возвращает grpc.ServerOption для публичного листенера (:9090).
func (c Config) PublicServerCreds() (grpc.ServerOption, error) {
	return grpcsrv.TLSServerCreds(c.PublicServerMTLS)
}

// InternalServerCreds возвращает grpc.ServerOption для internal-листенера (:9091).
func (c Config) InternalServerCreds() (grpc.ServerOption, error) {
	return grpcsrv.TLSServerCreds(c.InternalServerMTLS)
}

// schemaOptionsParam — URL-encoded libpq options=-c search_path=kacho_storage,public.
// Каждое соединение (pgxpool + goose database/sql) видит схему без отдельного SET.
const schemaOptionsParam = "options=-c%20search_path%3Dkacho_storage%2Cpublic"

// baseDSN — стандартный postgres DSN (для pgxpool и database/sql), несёт search_path
// kacho_storage через libpq options.
func (c Config) baseDSN() string {
	mode := c.DBSSLMode
	if mode == "" {
		mode = "disable"
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s&%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, mode, schemaOptionsParam,
	)
}

// DSN — строка подключения для pgxpool (поддерживает pool_max_conns). НЕ для
// database/sql (pool_max_conns → неизвестный PG-параметр → FATAL).
func (c Config) DSN() string {
	dsn := c.baseDSN()
	if c.DBMaxConns > 0 {
		dsn += fmt.Sprintf("&pool_max_conns=%d", c.DBMaxConns)
	}
	return dsn
}

// MigrateDSN — строка подключения для goose/database/sql (без pgxpool-параметров).
func (c Config) MigrateDSN() string {
	return c.baseDSN()
}

// Load загружает конфигурацию из переменных окружения.
func Load() (Config, error) {
	var c Config
	err := corecfg.LoadPrefixed(envPrefix, &c)
	return c, err
}
