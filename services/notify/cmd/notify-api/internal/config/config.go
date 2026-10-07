// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package config — конфигурация развёртывания notify-api и его страж старта.
//
// Значения приходят переменными окружения `KACHO_NOTIFY_*` из values
// развёртывания notify-api. Ручки, общие с notify-sender (посадка, база,
// удостоверение пира), носят те же переменные, но объявлены здесь — каждый корень
// объявляет ровно то, что читает (замысел issue-2924 З20; правило Д74:
// конфигурация корня живёт под его каталогом `cmd/<корень>/internal/`).
// Умолчание есть у одной ручки — окна сужателя
// (`KACHO_NOTIFY_LIST_FILTER_CACHE_TTL`, приёмка NTF-5 Р17): его величину сверяет
// с записью политики окон отзыва гейт `tools/revocationwindowgate`, а границу
// судит страж старта так же, как у заданного значения. У прочих ручек умолчания
// нет: незаданная ручка или значение вне границы — отказ старта с именем ручки
// (`sec-no-silent-default-for-guarded-knob`).
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"

	"github.com/PRO-Robotech/corelib/authz"
	corecfg "github.com/PRO-Robotech/corelib/config"
	"github.com/PRO-Robotech/corelib/grpcclient"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/servicecontract"
)

// Границы ручек notify-api (приёмка NTF-4 Р16/Р20 — звено прав и слушатель;
// приёмка NTF-5 Р17 — напоминание и окно сужателя).
const (
	AuthzCacheTTLMin       = time.Second
	AuthzCacheTTLMax       = 30 * time.Second
	AuthzCheckTimeoutMin   = 100 * time.Millisecond
	AuthzCheckTimeoutMax   = 10 * time.Second
	AuthzDenyBudgetMin     = 1.0
	AuthzDenyBudgetMax     = 10000.0
	HandlingBudgetMax      = 60 * time.Second
	DBMaxConnsMin          = 1
	DBMaxConnsMax          = 100
	InternalPortMin        = 1024
	InternalPortMax        = 65535
	ForwarderSANsMax       = 8
	ClientCAFilesMax       = 4
	NoticeReminderLeadMin  = time.Hour
	NoticeReminderLeadMax  = 168 * time.Hour
	ListFilterCacheTTLMin  = time.Second
	forwarderSANsMinPerSet = 1
)

// Config — значения ручек развёртывания notify-api. Поля заполняет [Load];
// читать их до [Config.Validate] нельзя — незаданная ручка неотличима от нулевой.
//
// Ручки общие с notify-sender (посадка, база, удостоверение пира) носят те же
// переменные: развёртывания читают каждое свою копию values. Ручку, которую
// notify-api не читает, его загрузчик не объявляет (`api-accepted-ignored`).
type Config struct {
	// AuthMode — посадка; notify-api поднимается только в боевой (NTF1-G15).
	AuthMode string `envconfig:"KACHO_NOTIFY_AUTH_MODE" knob:"notify.authMode"`

	// PeerTLS* — клиентское удостоверение notify-api к службе доступа и УЦ её
	// сервера. Выключателя mTLS нет.
	PeerTLSCertFile string `envconfig:"KACHO_NOTIFY_PEER_TLS_CERT_FILE" knob:"notify.peerTLS.certFile"`
	PeerTLSKeyFile  string `envconfig:"KACHO_NOTIFY_PEER_TLS_KEY_FILE" knob:"notify.peerTLS.keyFile"`
	PeerTLSCAFile   string `envconfig:"KACHO_NOTIFY_PEER_TLS_CA_FILE" knob:"notify.peerTLS.caFile"`

	// DB* — соединение с kacho_notify; режим шифрования судит дескриптор.
	DBHost     string `envconfig:"KACHO_NOTIFY_DB_HOST" knob:"notify.db.host"`
	DBPort     string `envconfig:"KACHO_NOTIFY_DB_PORT" knob:"notify.db.port"`
	DBUser     string `envconfig:"KACHO_NOTIFY_DB_USER" knob:"notify.db.user"`
	DBPassword string `envconfig:"KACHO_NOTIFY_DB_PASSWORD" knob:"notify.db.password"`
	DBName     string `envconfig:"KACHO_NOTIFY_DB_NAME" knob:"notify.db.name"`
	DBSSLMode  string `envconfig:"KACHO_NOTIFY_DB_SSLMODE" knob:"notify.db.sslMode"`
	DBMaxConns int    `envconfig:"KACHO_NOTIFY_DB_MAX_CONNS" knob:"notify.db.maxConns"`

	// DiagAddr — диагностическая поверхность (/metrics, /healthz, /readyz).
	DiagAddr string `envconfig:"KACHO_NOTIFY_DIAG_ADDR" knob:"notify.diagAddr"`

	// AuthzIAMGRPCAddr — внутренний адрес службы доступа: Check звена прав и
	// use-case, пакетная проверка сужателя.
	AuthzIAMGRPCAddr string `envconfig:"KACHO_NOTIFY_AUTHZ_IAM_GRPC_ADDR" knob:"notify.authz.iamGRPCAddr"`

	// Internal* — единственный (внутренний) слушатель и его удостоверение.
	InternalPort                string   `envconfig:"KACHO_NOTIFY_INTERNAL_PORT" knob:"notify.internalPort"`
	InternalServerCertFile      string   `envconfig:"KACHO_NOTIFY_INTERNAL_SERVER_MTLS_CERTFILE" knob:"notify.internalServer.certFile"`
	InternalServerKeyFile       string   `envconfig:"KACHO_NOTIFY_INTERNAL_SERVER_MTLS_KEYFILE" knob:"notify.internalServer.keyFile"`
	InternalServerClientCAFiles []string `envconfig:"KACHO_NOTIFY_INTERNAL_SERVER_MTLS_CLIENTCAFILES" knob:"notify.internalServer.clientCAFiles"`

	// Authz* — звено прав слушателя.
	AuthzTrustDomain          string        `envconfig:"KACHO_NOTIFY_AUTHZ_TRUST_DOMAIN" knob:"notify.authz.trustDomain"`
	AuthzTrustedForwarderSANs []string      `envconfig:"KACHO_NOTIFY_AUTHZ_TRUSTED_FORWARDER_SANS" knob:"notify.authz.trustedForwarderSANs"`
	AuthzTrustAnyForwarder    bool          `envconfig:"KACHO_NOTIFY_AUTHZ_TRUST_ANY_FORWARDER" knob:"notify.authz.trustAnyForwarder"`
	AuthzCacheTTL             time.Duration `envconfig:"KACHO_NOTIFY_AUTHZ_CACHE_TTL" knob:"notify.authz.cacheTTL"`
	AuthzCheckTimeout         time.Duration `envconfig:"KACHO_NOTIFY_AUTHZ_CHECK_TIMEOUT" knob:"notify.authz.checkTimeout"`
	AuthzDenyBudgetPerSec     float64       `envconfig:"KACHO_NOTIFY_AUTHZ_DENY_BUDGET_PER_SEC" knob:"notify.authz.denyBudgetPerSec"`

	// HandlingBudget — граница обработки одного вызова.
	HandlingBudget time.Duration `envconfig:"KACHO_NOTIFY_HANDLING_BUDGET" knob:"notify.handlingBudget"`

	// NoticeReminderLead — за сколько до начала работ напоминание MAINTENANCE.
	NoticeReminderLead time.Duration `envconfig:"KACHO_NOTIFY_NOTICE_REMINDER_LEAD" knob:"notify.notice.reminderLead"`
	// ListFilterCacheTTL — окно положительных вердиктов сужателя затронутых
	// ресурсов (окно отзыва). Верх границы — потолок политики окон отзыва.
	// Умолчание 5s — запись политики «notify KACHO_NOTIFY_LIST_FILTER_CACHE_TTL»
	// (corelib/authz.RevocationPolicy.Windows); расхождение тега с записью — красное
	// гейта окна отзыва с именем процесса и ручки (NTF-5 Р17, DoD 10.1 п.10).
	ListFilterCacheTTL time.Duration `envconfig:"KACHO_NOTIFY_LIST_FILTER_CACHE_TTL" knob:"notify.listFilter.cacheTTL" default:"5s"`

	// unset — ручки без умолчания, переменной которых нет в окружении вовсе.
	unset map[string]bool
}

// Knob — ручка процесса: имя в values и тексте отказа, переменная окружения и
// вид значения.
type Knob struct {
	Name string
	Env  string
	Kind reflect.Kind
	// Defaulted — у ручки есть умолчание загрузчика (тег `default`): незаданная
	// она получает его, а не отказ «не задана».
	Defaulted bool
}

func (k Knob) String() string { return k.Name + " (" + k.Env + ")" }

// Finding — одна причина отказа старта: ручка и почему.
type Finding struct {
	Knob Knob
	Why  string
}

// RefusalError — отказ старта со всеми находками разом.
type RefusalError struct {
	Findings []Finding
}

func (e *RefusalError) Error() string {
	lines := make([]string, 0, len(e.Findings))
	for _, f := range e.Findings {
		lines = append(lines, f.Knob.String()+": "+f.Why)
	}
	return "notify-api отказывается стартовать:\n  " + strings.Join(lines, "\n  ")
}

type findings []Finding

func (fs *findings) add(k Knob, why string, args ...any) {
	*fs = append(*fs, Finding{Knob: k, Why: fmt.Sprintf(why, args...)})
}

func (fs findings) err() error {
	if len(fs) == 0 {
		return nil
	}
	return &RefusalError{Findings: fs}
}

// Knobs — перечень ручек notify-api, выведенный из тегов [Config]. Второго
// перечня нет.
func Knobs() []Knob {
	t := reflect.TypeFor[Config]()
	out := make([]Knob, 0, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		env := f.Tag.Get("envconfig")
		if env == "" {
			continue
		}
		out = append(out, knobOfField(f))
	}
	return out
}

func knobOf(field string) Knob {
	f, ok := reflect.TypeFor[Config]().FieldByName(field)
	if !ok {
		panic("config: поля Config." + field + " нет — перечень ручек notify-api разошёлся с кодом стража")
	}
	return knobOfField(f)
}

func knobOfField(f reflect.StructField) Knob {
	_, defaulted := f.Tag.Lookup("default")
	return Knob{Name: f.Tag.Get("knob"), Env: f.Tag.Get("envconfig"), Kind: f.Type.Kind(), Defaulted: defaulted}
}

// Load читает ручки notify-api из окружения. Значение, не разбирающееся в
// вид ручки, — отказ с её именем; незаданные ручки судит [Config.Validate].
func Load() (Config, error) {
	var c Config
	if err := corecfg.Load(&c); err != nil {
		var pe *envconfig.ParseError
		if errors.As(err, &pe) {
			var fs findings
			fs.add(knobByEnv(pe.KeyName), "значение %q не разбирается: %v", pe.Value, pe.Err)
			return Config{}, fs.err()
		}
		return Config{}, fmt.Errorf("загрузка конфигурации notify-api: %w", err)
	}
	c.unset = map[string]bool{}
	for _, k := range Knobs() {
		if _, ok := os.LookupEnv(k.Env); !ok && !k.Defaulted {
			c.unset[k.Env] = true
		}
	}
	return c, nil
}

func knobByEnv(env string) Knob {
	for _, k := range Knobs() {
		if k.Env == env {
			return k
		}
	}
	return Knob{Name: env, Env: env}
}

// Mode — посадка для общего дескриптора.
func (c Config) Mode() (servicecontract.Mode, error) { return servicecontract.ParseMode(c.AuthMode) }

// DSN — строка соединения с kacho_notify.
func (c Config) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   net.JoinHostPort(c.DBHost, c.DBPort),
		Path:   "/" + c.DBName,
		RawQuery: url.Values{
			"sslmode":        []string{c.DBSSLMode},
			"pool_max_conns": []string{strconv.Itoa(c.DBMaxConns)},
		}.Encode(),
	}
	return u.String()
}

// TrustedForwarders — круг пересылающих принципала: единственное место, где
// читается сырой перечень; решения о круге принимаются по типу фундамента.
func (c Config) TrustedForwarders() grpcsrv.TrustedForwarders {
	return grpcsrv.NewTrustedForwarders(c.AuthzTrustedForwarderSANs...)
}

// TrustDomain — домен доверия пары звеньев личности.
func (c Config) TrustDomain() grpcsrv.TrustDomain { return grpcsrv.NewTrustDomain(c.AuthzTrustDomain) }

// InternalServerTLS — удостоверение единственного слушателя; mTLS всегда.
func (c Config) InternalServerTLS() grpcsrv.TLSServer {
	return grpcsrv.TLSServer{Enable: true, CertFile: c.InternalServerCertFile, KeyFile: c.InternalServerKeyFile,
		ClientCAFiles: c.InternalServerClientCAFiles}
}

// PeerTLS — клиентское удостоверение к службе доступа; mTLS всегда. Имя, которое
// обязан предъявить сертификат сервера, — узел из адреса службы доступа: тот,
// кого набирают, и проверяется, второй ручки об одном предмете нет.
func (c Config) PeerTLS() grpcclient.TLSClient {
	host, _, err := net.SplitHostPort(c.AuthzIAMGRPCAddr)
	if err != nil {
		host = ""
	}
	return grpcclient.TLSClient{Enable: true, CertFile: c.PeerTLSCertFile, KeyFile: c.PeerTLSKeyFile,
		CAFiles: []string{c.PeerTLSCAFile}, ServerName: host}
}

// ListFilter — величины сужателя затронутых ресурсов.
func (c Config) ListFilter() ListFilter {
	return ListFilter{CacheTTL: c.ListFilterCacheTTL, CheckTimeout: c.AuthzCheckTimeout}
}

// Validate — страж старта notify-api (ban #16, fail-closed): все находки
// разом. Посадку, которую судит общий дескриптор (режим против транспорта,
// sslmode), здесь повторно не судят; здесь — заданность каждой ручки и её
// граница.
func (c *Config) Validate() error {
	var fs findings
	for _, k := range Knobs() {
		if c.unset[k.Env] {
			fs.add(k, "ручка не задана; умолчания у неё нет")
		}
	}
	c.validatePosture(&fs)
	c.validateDB(&fs)
	c.validateListener(&fs)
	c.validateAuthz(&fs)
	c.duration(&fs, "NoticeReminderLead", c.NoticeReminderLead, NoticeReminderLeadMin, NoticeReminderLeadMax)
	// Верх окна сужателя — потолок политики окон отзыва платформы: литерала
	// верхней границы здесь нет (замысел issue-2924 З14 п.3).
	c.duration(&fs, "ListFilterCacheTTL", c.ListFilterCacheTTL, ListFilterCacheTTLMin, authz.RevocationPolicy.Ceiling)
	return fs.err()
}

func (c *Config) set(field string) (Knob, bool) {
	k := knobOf(field)
	return k, !c.unset[k.Env]
}

func (c *Config) duration(fs *findings, field string, v, lo, hi time.Duration) bool {
	k, ok := c.set(field)
	if !ok {
		return false
	}
	if v < lo || v > hi {
		fs.add(k, "значение %s вне границы [%s..%s]", v, lo, hi)
		return false
	}
	return true
}

func (c *Config) nonEmpty(fs *findings, fields ...string) {
	for _, field := range fields {
		if k, ok := c.set(field); ok && strings.TrimSpace(reflect.ValueOf(*c).FieldByName(field).String()) == "" {
			fs.add(k, "значение пусто")
		}
	}
}

func (c *Config) validatePosture(fs *findings) {
	if k, ok := c.set("AuthMode"); ok {
		mode, err := c.Mode()
		switch {
		case err != nil:
			fs.add(k, "ось authMode: %v", err)
		case !mode.IsProduction():
			fs.add(k, "ось authMode: посадка %q не боевая; notify поднимается только в production "+
				"или production-strict (NTF1-G15)", mode)
		}
	}
	c.nonEmpty(fs, "PeerTLSCertFile", "PeerTLSKeyFile", "PeerTLSCAFile")
}

func (c *Config) validateDB(fs *findings) {
	c.nonEmpty(fs, "DBHost", "DBUser", "DBPassword", "DBName", "DBSSLMode")
	if k, ok := c.set("DBPort"); ok {
		if _, err := parsePort(c.DBPort); err != nil {
			fs.add(k, "%v", err)
		}
	}
	if k, ok := c.set("DBMaxConns"); ok && (c.DBMaxConns < DBMaxConnsMin || c.DBMaxConns > DBMaxConnsMax) {
		fs.add(k, "значение %d вне границы [%d..%d]", c.DBMaxConns, DBMaxConnsMin, DBMaxConnsMax)
	}
}

func hostPort(fs *findings, k Knob, v string) (string, bool) {
	_, port, err := net.SplitHostPort(v)
	if err != nil {
		fs.add(k, "адрес %q не в форме узел:порт: %v", v, err)
		return "", false
	}
	if _, perr := parsePort(port); perr != nil {
		fs.add(k, "%v", perr)
		return "", false
	}
	return port, true
}

func (c *Config) validateListener(fs *findings) {
	diagPort := ""
	if k, ok := c.set("DiagAddr"); ok {
		diagPort, _ = hostPort(fs, k, c.DiagAddr)
	}
	if k, ok := c.set("AuthzIAMGRPCAddr"); ok {
		hostPort(fs, k, c.AuthzIAMGRPCAddr)
	}
	if k, ok := c.set("InternalPort"); ok {
		p, err := strconv.Atoi(c.InternalPort)
		switch {
		case err != nil:
			fs.add(k, "порт %q не число", c.InternalPort)
		case p < InternalPortMin || p > InternalPortMax:
			fs.add(k, "порт %d вне границы [%d..%d]", p, InternalPortMin, InternalPortMax)
		case c.InternalPort == diagPort:
			fs.add(k, "порт %d совпадает с портом %s", p, knobOf("DiagAddr"))
		}
	}
	for _, field := range []string{"InternalServerCertFile", "InternalServerKeyFile"} {
		if k, ok := c.set(field); ok {
			if v := reflect.ValueOf(*c).FieldByName(field).String(); !filepath.IsAbs(v) {
				fs.add(k, "путь %q не абсолютный", v)
			}
		}
	}
	if k, ok := c.set("InternalServerClientCAFiles"); ok {
		list := c.InternalServerClientCAFiles
		if len(list) < 1 || len(list) > ClientCAFilesMax {
			fs.add(k, "файлов УЦ %d вне границы [1..%d]", len(list), ClientCAFilesMax)
		}
		seen := map[string]bool{}
		for _, p := range list {
			if !filepath.IsAbs(p) {
				fs.add(k, "путь %q не абсолютный", p)
			}
			if seen[p] {
				fs.add(k, "путь %q повторён", p)
			}
			seen[p] = true
		}
	}
}

func (c *Config) validateAuthz(fs *findings) {
	domainOK := false
	if k, ok := c.set("AuthzTrustDomain"); ok {
		if !isDNSName(c.AuthzTrustDomain) {
			fs.add(k, "домен доверия %q — не имя DNS из двух меток и больше", c.AuthzTrustDomain)
		} else {
			domainOK = true
		}
	}
	if k, ok := c.set("AuthzTrustedForwarderSANs"); ok {
		circle := c.TrustedForwarders()
		if n := circle.Len(); n < forwarderSANsMinPerSet || n > ForwarderSANsMax {
			fs.add(k, "записей %d вне границы [%d..%d]", n, forwarderSANsMinPerSet, ForwarderSANsMax)
		}
		if domainOK {
			for _, s := range circle.SANs() {
				if !forwarderInDomain(s, c.AuthzTrustDomain) {
					fs.add(k, "запись %q — не spiffe://%s/ns/<ns>/sa/<sa>", s, c.AuthzTrustDomain)
				}
			}
		}
	}
	c.duration(fs, "AuthzCacheTTL", c.AuthzCacheTTL, AuthzCacheTTLMin, AuthzCacheTTLMax)
	checkOK := c.duration(fs, "AuthzCheckTimeout", c.AuthzCheckTimeout, AuthzCheckTimeoutMin, AuthzCheckTimeoutMax)
	if k, ok := c.set("AuthzDenyBudgetPerSec"); ok &&
		(c.AuthzDenyBudgetPerSec < AuthzDenyBudgetMin || c.AuthzDenyBudgetPerSec > AuthzDenyBudgetMax) {
		fs.add(k, "значение %v вне границы [%v..%v]", c.AuthzDenyBudgetPerSec, AuthzDenyBudgetMin, AuthzDenyBudgetMax)
	}
	if k, ok := c.set("HandlingBudget"); ok && checkOK {
		if c.HandlingBudget <= c.AuthzCheckTimeout || c.HandlingBudget > HandlingBudgetMax {
			fs.add(k, "значение %s вне границы (%s %s..%s]", c.HandlingBudget,
				knobOf("AuthzCheckTimeout"), c.AuthzCheckTimeout, HandlingBudgetMax)
		}
	}
}

// isDNSName — имя DNS по RFC 1123 не меньше чем из двух меток.
func isDNSName(s string) bool {
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if !isDNSLabel(l) {
			return false
		}
	}
	return true
}

// forwarderInDomain — URI SAN формы spiffe://<домен>/ns/<ns>/sa/<sa>.
func forwarderInDomain(s, domain string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "spiffe" || u.Host != domain {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	return len(parts) == 4 && parts[0] == "ns" && parts[1] != "" && parts[2] == "sa" && parts[3] != ""
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("порт %q не число", s)
	}
	if p < 1 || p > 65535 {
		return 0, fmt.Errorf("порт %d вне границы [1..65535]", p)
	}
	return p, nil
}

// isDNSLabel — метка DNS по RFC 1123 (строчные буквы, цифры, дефис не по краям).
func isDNSLabel(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
