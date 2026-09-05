// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package config — конфигурация kacho-geo, загружается из переменных окружения
// через corelib config.LoadPrefixed("KACHO_GEO"). Поля с абсолютным тегом
// читаются как есть; вложенные per-edge TLS-структуры (grpcclient.TLSClient /
// grpcsrv.TLSServer) получают независимые имена KACHO_GEO_<EDGE>_<NAME> (префикс
// на каждое ребро — без общего на весь процесс TLS-синглтона).
package config

import (
	"time"

	"fmt"

	"google.golang.org/grpc"

	corecfg "github.com/PRO-Robotech/kacho/pkg/config"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// envPrefix — корневой сегмент env-имен kacho-geo (KACHO_<DOMAIN>).
const envPrefix = "KACHO_GEO"

// Config — конфигурация kacho-geo.
type Config struct {
	DBHost     string `envconfig:"KACHO_GEO_DB_HOST" default:"localhost"`
	DBPort     string `envconfig:"KACHO_GEO_DB_PORT" default:"5432"`
	DBUser     string `envconfig:"KACHO_GEO_DB_USER" default:"geo"`
	DBPassword string `envconfig:"KACHO_GEO_DB_PASSWORD" required:"true"`
	DBName     string `envconfig:"KACHO_GEO_DB_NAME" default:"kacho_geo"`
	// DBSSLMode — sslmode для DSN. dev по умолчанию `disable`; в проде обязателен
	// require|verify-ca|verify-full.
	DBSSLMode string `envconfig:"KACHO_GEO_DB_SSLMODE" default:"disable"`
	// DBMaxConns — лимит pgx-пула (0 = дефолт pgx max(4, NumCPU)).
	DBMaxConns int `envconfig:"KACHO_GEO_DB_MAX_CONNS" default:"0"`

	// GrpcPort — публичный read-only листенер (RegionService/ZoneService).
	GrpcPort string `envconfig:"KACHO_GEO_GRPC_PORT" default:"9090"`
	// InternalGrpcPort — cluster-internal листенер (InternalRegion/ZoneService).
	// Не выставляется на внешнем endpoint api-gateway — только cluster-internal.
	InternalGrpcPort string `envconfig:"KACHO_GEO_INTERNAL_PORT" default:"9091"`

	// MetricsAddr — адрес cluster-internal diagnostic HTTP-listener'а (/metrics).
	// Default ":9095" — отдельный internal-порт (НЕ маршрутизируется на external
	// endpoint; cluster-internal Prometheus-scrape LRO-durability метрик worker'а и
	// reconciler'а). Пустое значение явно отключает listener (back-compat).
	MetricsAddr string `envconfig:"KACHO_GEO_METRICS_ADDR" default:":9095"`

	// AuthMode — fail-closed режим: dev | production | production-strict.
	// Дефолт — production (secure-by-default): при незаданном env raw-деплой
	// поднимается в fail-closed-режиме (dev-опт-ин на несужение круга inert),
	// как iam/vpc/nlb. dev — явный opt-in: локальные фикстуры и dev-профиль
	// deploy-стенда выставляют его через env (security.md «любой деплой —
	// production-mode; KACHO_*_AUTH_MODE=dev на кластере — security-долг»).
	AuthMode string `envconfig:"KACHO_GEO_AUTH_MODE" default:"production"`

	// AuthZIAMGRPCAddr — internal endpoint kaname для per-RPC Check
	// (ребро geo→iam authz), обычно iam-internal :9091.
	//
	// Пустое значение — ОТКАЗ СТАРТА, а не грациозный подъём без проверки прав:
	// ребро решения о доступе задаётся ЯВНО, обеими половинами (адрес и
	// транспорт), и «не задал» режимом не является. Держит это конструктор
	// дескриптора, а не собственный страж сервиса.
	AuthZIAMGRPCAddr string `envconfig:"KACHO_GEO_AUTHZ_IAM_GRPC_ADDR" default:""`

	// AuthZCheckTimeout — срок ОДНОГО вопроса о правах владельцу модели.
	//
	// Ручка заведена не ради нового поведения: значение то же, что было
	// умолчанием платформы. Изменилось то, что оно стало ВЫБРАННЫМ. Неотвечающий
	// сосед без срока вешает горутину навсегда, и звено не доходит до своей
	// ветки fail-closed — горутины копятся до исчерпания процесса. Величина,
	// которой никто не выбирал, не может быть ни обсуждена, ни сужена на
	// конкретной посадке.
	AuthZCheckTimeout time.Duration `envconfig:"KACHO_GEO_AUTHZ_CHECK_TIMEOUT" default:"2s"`

	// AuthZTrustedForwarderSANs — allow-list cert-identity SAN'ов, которым разрешено
	// форвардить end-user principal в x-kacho-principal-* metadata (обычно
	// единственный — api-gateway SA, SAN spiffe://kacho.cloud/ns/<ns>/sa/kacho-api-gateway).
	// Принимает comma-separated список. Пустой (default) allow-list — НЕ молчаливый
	// trust-any: старт fail-closed ОТКАЗЫВАЕТ (конструктор дескриптора, О1),
	// пока не запинен хотя бы один SAN — либо, только в dev, не выставлен явный
	// AuthZTrustAnyForwarder opt-in (в production/production-strict trust-any не honored
	// вовсе — обязателен непустой SAN).
	// Задаётся в production для defense-in-depth против confused-deputy/principal-
	// spoofing: внутренний сервис со своим валидным client-cert'ом не сможет выдать
	// себя за пользователя — ни эскалировать до admin-CRUD Region/Zone на internal-
	// листенере (:9091), ни подделать viewer-principal на публичном read-endpoint
	// (:9090). На ОБОИХ листенерах principal trust-gated через
	// grpcsrv.UnaryCertIdentityExtract + UnaryTrustedPrincipalExtract(
	// WithTrustedForwarders(...)) — без verified cert'а (или вне allow-list)
	// forwarded principal снимается → authz видит no-principal → fail-closed deny.
	// Единственный легитимный форвардер end-user principal'а — api-gateway;
	// consumer'ы vpc/compute/nlb ходят на публичный :9090 со своим cert'ом.
	AuthZTrustedForwarderSANs []string `envconfig:"KACHO_GEO_AUTHZ_TRUSTED_FORWARDER_SANS"`

	// AuthZTrustAnyForwarder — ЯВНЫЙ dev-опт-ин на «доверять ЛЮБОМУ mTLS-verified
	// peer'у как форвардеру end-user principal'а» (пустой allow-list). Secure-by-
	// default: без этого флага (и без запиненного SAN) старт
	// ОТКАЗЫВАЕТ — пустой allow-list больше НЕ молчаливый дефолт. Нужен только для
	// локального dev без api-gateway-SAN; в production/production-strict НЕ
	// honored (там обязателен непустой SAN — trust-any недопустим). Оставленный
	// незаданным (false) = fail-closed. Решение принимает конструктор
	// дескриптора — один отказ на все сервисы, а не свой у каждого.
	AuthZTrustAnyForwarder bool `envconfig:"KACHO_GEO_AUTHZ_TRUST_ANY_FORWARDER" default:"false"`

	// AuthZCacheTTL — окно кеша положительных вердиктов авторизации, оно же ОКНО
	// ОТЗЫВА: столько субъект, у которого право уже отобрали, продолжает
	// проходить. Отрицательные вердикты не кешируются никогда, поэтому свежая
	// выдача видна сразу, а ждёт один лишь отзыв.
	//
	// Ручка заведена не ради нового поведения: значение то же, что было
	// умолчанием платформы. Изменилось то, что число стало ВЫБРАННЫМ — параметр
	// безопасности, которого никто не выбирал, нельзя ни обсудить, ни отозвать,
	// ни сузить на конкретной посадке. Потолок объявлен политикой
	// (pkg/authz.RevocationPolicy.Ceiling), перепись — там же; смена значения без
	// правки политики роняет гейт и называет оба числа.
	//
	// Ноль означает «беру объявленную политику», а не «кеша нет».
	AuthZCacheTTL time.Duration `envconfig:"KACHO_GEO_AUTHZ_CACHE_TTL" default:"5s"`

	// AuthZDenyBudgetPerSec — устойчивый темп (в секунду на принципала) проверок,
	// чей исход кэш НЕ поглощает: отказ, сокрытие существования, промах «нет
	// пути», недоступность модели. По исчерпании звено отвечает
	// `ResourceExhausted`, не обращаясь к iam, — то есть сбрасывает шторм с него.
	//
	// Величина 100 не выдумана: это то же число, которое платформа уже выбрала
	// для того же механизма — литерал в композиционном корне nlb и умолчание
	// ручки vpc. Умолчания у самого механизма НЕТ (неположительное значение он
	// читает как «ограничения нет»), поэтому число обязано быть названо здесь.
	//
	// Почему отсечка нужна и geo, хотя его читает каждый тенант: бюджет тратят
	// ТОЛЬКО непоглощаемые кэшем исходы, а законное чтение справочника
	// разрешается и кэшируется — то есть штатный тенант её не видит вовсе.
	// Платит ровно тот, кто штурмует отказами, и платит за него не kaname.
	AuthZDenyBudgetPerSec float64 `envconfig:"KACHO_GEO_AUTHZ_DENY_BUDGET_PER_SEC" default:"100"`

	// AdmissionPublic / AdmissionInternal — ПОТОЛОК ТЕМПА и ОДНОВРЕМЕННОСТИ на
	// вызывающего, по одному набору на слушатель. Не путать с бюджетом отказов
	// выше: тот сбрасывает шторм ОТКАЗОВ с владельца модели, этот ограничивает
	// ПОТОК запросов к нам самим, и стоимость запроса здесь высокая по
	// построению — мутация есть три строки в базе, чтение есть до тысячи
	// объектов на страницу с проверкой прав партиями.
	//
	// # Почему у ручек НЕТ умолчаний в тегах
	//
	// Полы у слушателей РАЗНЫЕ, поэтому умолчание пришлось бы написать дважды —
	// то есть завести седьмую пропись чисел, которые уже названы в фундаменте
	// (grpcsrv.PlatformPublicAdmission / PlatformInternalAdmission). Молчание
	// посадки разрешается там же, где живут числа: пустой набор означает ПОЛ
	// ПЛАТФОРМЫ, а не ноль. Ноль механизм читает как «не ограничиваем», и
	// слушатель выглядел бы защищённым, ни разу не отказав.
	//
	// # Что посадка вправе сказать
	//
	// Ничего (берётся пол) либо ВЕСЬ набор из четырёх осей. Частичное
	// объявление отвергается стартом с именем слушателя: оператор, задавший темп
	// и забывший одновременность, считает предел выставленным, а незаполненная
	// ось не ограничивает ничего. Своя величина осмысленна потому, что вёдра
	// живут В ПРОЦЕССЕ: при N репликах эффективный предел равен N × объявленного,
	// и запас у стенда из одной реплики и из двадцати разный.
	//
	// Имена: KACHO_GEO_ADMISSION_{PUBLIC,INTERNAL}_{READ_PER_SEC,
	// MUTATION_PER_SEC,BURST_FACTOR,IN_FLIGHT}.
	AdmissionPublic   grpcsrv.AdmissionKnobs `envconfig:"ADMISSION_PUBLIC"`
	AdmissionInternal grpcsrv.AdmissionKnobs `envconfig:"ADMISSION_INTERNAL"`

	// HandlingBudget — верхняя граница обработки ОДНОГО вызова (серверный срок).
	// Более строгий срок вызывающего уважается; окно не расширяется никогда.
	//
	// 30s — то же число, что платформа выбрала для той же величины у vpc
	// (`request-timeout` в его чарте). Это ПОТОЛОК, а не цель: он обязан с
	// запасом накрывать вопрос о правах (KACHO_GEO_AUTHZ_CHECK_TIMEOUT, 2s) плюс
	// запрос к своей БД, а предмет его — не задержка, а вызов БЕЗ срока, который
	// держит соединение из ограниченного пула столько, сколько выполняется его
	// запрос: MaxConns таких вызовов отказывают весь сервис (CWE-770).
	//
	// «Не применимо» у величины нет и быть не может — сказать «границы не надо»
	// значит сказать «мой процесс вправе держать чужой ресурс сколько угодно».
	// Неположительное значение отвергает конструктор дескриптора.
	HandlingBudget time.Duration `envconfig:"KACHO_GEO_HANDLING_BUDGET" default:"30s"`

	// ===== per-edge mTLS =====

	// IAMAuthzMTLS — client-creds для ребра geo→iam Check (:9091).
	IAMAuthzMTLS grpcclient.TLSClient `envconfig:"IAM_AUTHZ_MTLS"`

	// PublicServerMTLS — server-creds для публичного листенера (:9090).
	PublicServerMTLS grpcsrv.TLSServer `envconfig:"PUBLIC_SERVER_MTLS"`

	// InternalServerMTLS — server-creds для cluster-internal листенера (:9091).
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

// schemaOptionsParam — URL-encoded libpq `options=-c search_path=kacho_geo,public`.
// Каждое соединение (pgxpool + goose database/sql) видит схему kacho_geo без
// отдельного SET search_path на каждый стейтмент.
const schemaOptionsParam = "options=-c%20search_path%3Dkacho_geo%2Cpublic"

// baseDSN — стандартный postgres DSN (годится и для pgxpool, и для database/sql),
// несет search_path kacho_geo через libpq options.
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

// TrustedForwarders — круг отправителей, который РЕАЛЬНО уезжает в
// grpcsrv.WithTrustedForwarders на обоих листенерах.
//
// Единственный источник этого значения на процесс: его читает и проводка
// (cmd/kacho-geo/serve.go), и стража старта (Validate), и самоотчёт о посадке
// (cmd/kacho-geo/bootposture.go). Все трое спрашивают ОДИН объект и ОДИН его
// предикат, поэтому «стража пропустила» ⟺ «круг реально сужен» — по построению,
// а не по совпадению одинаково написанных тел.
//
// Нормализация круга (пустые записи, пробелы по краям, повторы) живёт в
// конструкторе типа и здесь не пересказывается: два места об одном предмете
// разъезжаются молча. См. grpcsrv.NewTrustedForwarders.
func (c Config) TrustedForwarders() grpcsrv.TrustedForwarders {
	return grpcsrv.NewTrustedForwarders(c.AuthZTrustedForwarderSANs...)
}
