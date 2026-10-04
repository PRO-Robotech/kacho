// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package config — конфигурация пробы-источника notify-probe: переменные
// окружения KACHO_NOTIFYPROBE_*, разобранные ОДИН раз при старте.
//
// Проба — стендовый объект (в цепочке `prod` её нет): служба kacho, подключённая
// к notify процедурой «подключить службу». Оттого здесь три ручки, которых нет у
// прочих служб, и у каждой ровно один читатель:
//
//   - KACHO_NOTIFYPROBE_NOTIFICATIONS_ENABLED — флаг доставки извещений.
//     Умолчания нет: незаданная переменная — отказ старта с её именем, потому
//     что false из незаданного неотличим от выключенного модуля. Разбирает его
//     функция фундамента [feed.ParseEnabled] (ровно "true" либо "false");
//   - KACHO_NOTIFYPROBE_NOTIFICATIONS_FEED_KEYRING — путь к файлу кольца ключей
//     ленты; обязателен при включённом флаге и не читается при выключенном;
//   - KACHO_NOTIFYPROBE_NOTIFY_SAN — SPIFFE-идентификатор notify: единственная
//     строка таблицы звена идентичности служб `{SAN notify → notify}`;
//     обязателен при включённом флаге.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/corelib/authz"
	corecfg "github.com/PRO-Robotech/corelib/config"
	"github.com/PRO-Robotech/corelib/grpcclient"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/notify/feed"
)

// envPrefix — корневой сегмент имён переменных пробы (KACHO_<DOMAIN>).
const envPrefix = "KACHO_NOTIFYPROBE"

// Имена ручек, которые разбираются мимо тегов: их читает функция фундамента
// либо страж согласия, и имя нужно тексту отказа.
const (
	// FlagKnob — флаг доставки извещений источника (Р9).
	FlagKnob = "KACHO_NOTIFYPROBE_NOTIFICATIONS_ENABLED"
	// KeyringKnob — путь к файлу кольца ключей ленты (З11).
	KeyringKnob = "KACHO_NOTIFYPROBE_NOTIFICATIONS_FEED_KEYRING"
	// NotifySANKnob — SPIFFE-идентификатор notify (строка таблицы звена Р2).
	NotifySANKnob = "KACHO_NOTIFYPROBE_NOTIFY_SAN"
	// authZCacheTTLKnob — окно отзыва; имя нужно тексту отказа загрузки.
	authZCacheTTLKnob = "KACHO_NOTIFYPROBE_AUTHZ_CACHE_TTL"
	// revocationWindowKey — запись окна этой ручки в политике платформы
	// (corelib/authz.RevocationPolicy.Windows): текст отказа берёт значение
	// оттуда, а не литералом, и не солжёт при смене политики.
	revocationWindowKey = "notify " + authZCacheTTLKnob
)

// Config — конфигурация notify-probe.
type Config struct {
	DBHost     string `envconfig:"KACHO_NOTIFYPROBE_DB_HOST" default:"localhost"`
	DBPort     string `envconfig:"KACHO_NOTIFYPROBE_DB_PORT" default:"5432"`
	DBUser     string `envconfig:"KACHO_NOTIFYPROBE_DB_USER" default:"notifyprobe"`
	DBPassword string `envconfig:"KACHO_NOTIFYPROBE_DB_PASSWORD" required:"true"`
	DBName     string `envconfig:"KACHO_NOTIFYPROBE_DB_NAME" default:"kacho_notifyprobe"`
	// DBSSLMode — sslmode строки подключения. Боевую посадку судит конструктор
	// дескриптора (require|verify-ca|verify-full).
	DBSSLMode string `envconfig:"KACHO_NOTIFYPROBE_DB_SSLMODE" default:"disable"`
	// DBMaxConns — предел пула pgx (0 — умолчание pgx max(4, NumCPU)).
	DBMaxConns int `envconfig:"KACHO_NOTIFYPROBE_DB_MAX_CONNS" default:"0"`

	// GrpcPort — публичный слушатель. Публичных служб у пробы нет: слушатель
	// поднимается носителем парой с внутренним и служит пустой набор.
	GrpcPort string `envconfig:"KACHO_NOTIFYPROBE_GRPC_PORT" default:"9090"`
	// InternalGrpcPort — внутренний слушатель: сервер ленты и сервер подписки
	// (Internal*-службы, на внешний край не выходят — ban #6).
	InternalGrpcPort string `envconfig:"KACHO_NOTIFYPROBE_INTERNAL_PORT" default:"9091"`

	// MetricsAddr — адрес внутренней диагностической поверхности
	// (/metrics, /healthz, /readyz). Пустое значение — поверхность не
	// поднимается, и это объявляется словами при старте.
	MetricsAddr string `envconfig:"KACHO_NOTIFYPROBE_METRICS_ADDR" default:":9095"`

	// AuthMode — dev | production | production-strict; умолчание — боевое.
	AuthMode string `envconfig:"KACHO_NOTIFYPROBE_AUTH_MODE" default:"production"`

	// AuthZIAMGRPCAddr — внутренний адрес владельца модели прав для проверки
	// Claim/Ack и сужения потока подписки. Пустое — отказ старта (судит
	// конструктор дескриптора).
	AuthZIAMGRPCAddr string `envconfig:"KACHO_NOTIFYPROBE_AUTHZ_IAM_GRPC_ADDR" default:""`
	// AuthZCheckTimeout — срок одного вопроса о правах владельцу модели; тот же
	// срок у одного запроса сужателя потока.
	AuthZCheckTimeout time.Duration `envconfig:"KACHO_NOTIFYPROBE_AUTHZ_CHECK_TIMEOUT" default:"2s"`
	// AuthZTrustedForwarderSANs — круг пересылающих личность пользователя.
	// Пустой круг без явного dev-опт-ина — отказ старта (конструктор
	// дескриптора).
	AuthZTrustedForwarderSANs []string `envconfig:"KACHO_NOTIFYPROBE_AUTHZ_TRUSTED_FORWARDER_SANS"`
	// AuthZTrustAnyForwarder — dev-опт-ин «доверять любому mTLS-пиру»; в
	// боевой посадке не принимается.
	AuthZTrustAnyForwarder bool `envconfig:"KACHO_NOTIFYPROBE_AUTHZ_TRUST_ANY_FORWARDER" default:"false"`
	// AuthZTrustDomain — домен доверия установки; умолчания нет, пустое — отказ
	// старта.
	AuthZTrustDomain string `envconfig:"KACHO_NOTIFYPROBE_AUTHZ_TRUST_DOMAIN"`
	// AuthZCacheTTL — окно положительных вердиктов (оно же окно отзыва).
	// Умолчание 5s — значение corelib/authz.RevocationPolicy (запись
	// revocationWindowKey); его сверяет перепись
	// окна отзыва (tools/revocationwindowgate). Неположительное значение —
	// отказ загрузки с именем ручки: у окна два читателя (дескриптор отвергает
	// ≤0, сужатель потока подставил бы своё умолчание), и смысл у нуля один —
	// отказ старта, а не «политика по умолчанию».
	AuthZCacheTTL time.Duration `envconfig:"KACHO_NOTIFYPROBE_AUTHZ_CACHE_TTL" default:"5s"`
	// AuthZDenyBudgetPerSec — темп непоглощаемых кешем исходов проверки на
	// принципала; по исчерпании звено отвечает ResourceExhausted, не спрашивая
	// владельца модели. 100 — число, выбранное платформой для того же механизма
	// у соседних служб.
	AuthZDenyBudgetPerSec float64 `envconfig:"KACHO_NOTIFYPROBE_AUTHZ_DENY_BUDGET_PER_SEC" default:"100"`

	// AdmissionPublic / AdmissionInternal — потолок темпа и одновременности на
	// вызывающего; пустой набор — пол платформы.
	AdmissionPublic   grpcsrv.AdmissionKnobs `envconfig:"ADMISSION_PUBLIC"`
	AdmissionInternal grpcsrv.AdmissionKnobs `envconfig:"ADMISSION_INTERNAL"`

	// HandlingBudget — верхняя граница обработки одного унарного вызова.
	HandlingBudget time.Duration `envconfig:"KACHO_NOTIFYPROBE_HANDLING_BUDGET" default:"30s"`

	// SubscriptionMaxStreams — потолок одновременных потоков подписки процесса;
	// каждый держит своё соединение вне пула. Читатель потока у пробы один —
	// notify (2 реплики), запас — на перекат.
	SubscriptionMaxStreams int `envconfig:"KACHO_NOTIFYPROBE_SUBSCRIPTION_MAX_STREAMS" default:"8"`
	// SubscriptionStreamBudget — срок жизни одного потока; по истечении поток
	// закрывается чисто, и подписчик возобновляется со своей позиции.
	SubscriptionStreamBudget time.Duration `envconfig:"KACHO_NOTIFYPROBE_SUBSCRIPTION_STREAM_BUDGET" default:"30m"`
	// SubscriptionIdlePoll — холостой перепрос журнала без пробуждения.
	SubscriptionIdlePoll time.Duration `envconfig:"KACHO_NOTIFYPROBE_SUBSCRIPTION_IDLE_POLL" default:"5s"`

	// NotifySAN — SPIFFE-идентификатор notify (см. NotifySANKnob).
	NotifySAN string `envconfig:"KACHO_NOTIFYPROBE_NOTIFY_SAN"`

	// ===== рёбра mTLS =====

	// IAMAuthzMTLS — клиентские креды ребра проба→владелец модели прав.
	IAMAuthzMTLS grpcclient.TLSClient `envconfig:"IAM_AUTHZ_MTLS"`
	// PublicServerMTLS — серверные креды публичного слушателя.
	PublicServerMTLS grpcsrv.TLSServer `envconfig:"PUBLIC_SERVER_MTLS"`
	// InternalServerMTLS — серверные креды внутреннего слушателя.
	InternalServerMTLS grpcsrv.TLSServer `envconfig:"INTERNAL_SERVER_MTLS"`

	// Notifications — флаг доставки, разобранный [feed.ParseEnabled].
	// Тега нет: у флага один читатель, и это функция фундамента.
	Notifications feed.Enabled `ignored:"true"`
	// Keyring — кольцо ключей ленты; nil при выключенной доставке.
	Keyring *feed.Keyring `ignored:"true"`
}

// Load загружает конфигурацию из окружения процесса.
func Load() (Config, error) {
	return load(os.LookupEnv, os.ReadFile, policyWindow())
}

// policyWindow — значение политики платформы для окна этой ручки
// (corelib/authz.RevocationPolicy.Windows). Load и проба берут его одной
// функцией; load получает значение параметром, чтобы проба могла подать
// значение, отличное от умолчания тега, и отличить подстановку от литерала.
func policyWindow() time.Duration {
	return authz.RevocationPolicy.Windows[revocationWindowKey]
}

// load — Load с поданными окружением и файловой системой: страж согласия
// флага, кольца и SAN судится одной функцией, и проба зовёт её, а не копию.
// window — значение политики окна отзыва, которое называет текст отказа.
func load(lookup func(string) (string, bool), readFile func(string) ([]byte, error), window time.Duration) (Config, error) {
	var c Config
	if err := corecfg.LoadPrefixed(envPrefix, &c); err != nil {
		return Config{}, err
	}
	if c.AuthZCacheTTL <= 0 {
		return Config{}, fmt.Errorf("%s: окно отзыва %s неположительно — окно обязано быть больше нуля "+
			"(значение политики платформы — %s)", authZCacheTTLKnob, c.AuthZCacheTTL, window)
	}
	// Флаг — первым из ручек ленты: без него не решается, обязательны ли
	// кольцо и SAN.
	en, err := feed.ParseEnabled(FlagKnob, lookup)
	if err != nil {
		return Config{}, err
	}
	c.Notifications = en
	if !en.On() {
		return c, nil
	}
	ring, err := feed.LoadKeyring(KeyringKnob, lookup, readFile)
	if err != nil {
		return Config{}, err
	}
	c.Keyring = ring
	if strings.TrimSpace(c.NotifySAN) == "" {
		return Config{}, errors.New(NotifySANKnob + " не задана — при включённой доставке " +
			"таблица звена идентичности служб пуста, и notify к ленте не пройдёт ни разу")
	}
	return c, nil
}

// PublicServerCreds — grpc.ServerOption публичного слушателя.
func (c Config) PublicServerCreds() (grpc.ServerOption, error) {
	return grpcsrv.TLSServerCreds(c.PublicServerMTLS)
}

// InternalServerCreds — grpc.ServerOption внутреннего слушателя.
func (c Config) InternalServerCreds() (grpc.ServerOption, error) {
	return grpcsrv.TLSServerCreds(c.InternalServerMTLS)
}

// schemaOptionsParam — libpq `options=-c search_path=kacho_notifyprobe,public`.
const schemaOptionsParam = "options=-c%20search_path%3Dkacho_notifyprobe%2Cpublic"

// baseDSN — строка соединения. Имя, пароль и база экранируются построителем
// адреса, а не склейкой строки: пароль с `@`, `/`, `?`, `#`, `%` иначе дал бы
// иную строку соединения (форма та же, что у шлюза каталога,
// services/notify/internal/config).
func (c Config) baseDSN() string {
	mode := c.DBSSLMode
	if mode == "" {
		mode = "disable"
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.DBUser, c.DBPassword),
		Host:     net.JoinHostPort(c.DBHost, c.DBPort),
		Path:     "/" + c.DBName,
		RawQuery: "sslmode=" + url.QueryEscape(mode) + "&" + schemaOptionsParam,
	}
	return u.String()
}

// DSN — строка подключения пула pgx (несёт pool_max_conns).
func (c Config) DSN() string {
	dsn := c.baseDSN()
	if c.DBMaxConns > 0 {
		dsn += fmt.Sprintf("&pool_max_conns=%d", c.DBMaxConns)
	}
	return dsn
}

// SingleConnDSN — строка ВЫДЕЛЕННОГО соединения вне пула (LISTEN потока
// подписки): параметр пула вне пула — неизвестный PG-параметр и FATAL.
func (c Config) SingleConnDSN() string {
	return c.baseDSN()
}

// TrustDomain — домен доверия, уезжающий в пару звеньев извлечения личности.
func (c Config) TrustDomain() grpcsrv.TrustDomain {
	return grpcsrv.NewTrustDomain(c.AuthZTrustDomain)
}

// TrustedForwarders — круг пересылающих, уезжающий в оба слушателя.
func (c Config) TrustedForwarders() grpcsrv.TrustedForwarders {
	return grpcsrv.NewTrustedForwarders(c.AuthZTrustedForwarderSANs...)
}
