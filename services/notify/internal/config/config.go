// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package config — конфигурация процесса notify и его страж старта.
//
// Значения приходят переменными окружения `KACHO_NOTIFY_*`, которые выводит
// чарт notify из values установки (З20, З28). У КАЖДОЙ ручки нет умолчания:
// незаданная, пустая или вне границы — отказ старта с именем ручки и границей
// (`sec-no-silent-default-for-guarded-knob`). Загрузчик значения не
// подставляет никогда.
//
// Страж — [Config.Validate]: он собирает ВСЕ находки разом и возвращает
// [*RefusalError]. Посадку, которую судит общий дескриптор
// (`servicecontract.New`: режим, `sslmode`), здесь повторно не судят — второе
// место об одном предмете, из которых верно одно. Здесь — оси, которых у
// общего дескриптора нет: боевой режим как единственно допустимый, mTLS
// клиента к источникам и kaname, секрет почты, ручки предмета notify.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"

	corecfg "github.com/PRO-Robotech/corelib/config"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/servicecontract"
)

// Границы ручек — §8 замысла NTF-1.
const (
	ClaimIntervalMin      = time.Second
	ClaimIntervalMax      = 5 * time.Minute
	ResolveSendTimeoutMin = 100 * time.Millisecond
	ResolveSendTimeoutMax = 30 * time.Second
	SMTPSessionTimeoutMin = time.Second
	SMTPSessionTimeoutMax = 120 * time.Second
)

// AckMargin — запас на `Ack` перед концом аренды строки. Константа, не ручка
// (§8 замысла): её читает сумма сроков стража (УК31) и крайний момент строки
// цикла обработки (З21).
const AckMargin = 5 * time.Second

// Config — значения ручек процесса notify. Поля заполняет [Load]; читать их
// до [Config.Validate] нельзя — незаданная ручка здесь неотличима от нулевой.
//
// Тег `knob` — имя ручки в values и в тексте отказа; тег `envconfig` — имя
// переменной. Оба тега — единственный перечень ручек: по нему загрузчик читает
// окружение, страж проверяет заданность, а перепись [Knobs] печатает состав.
type Config struct {
	// ── посадка ─────────────────────────────────────────────────────────────

	// AuthMode — режим посадки. notify поднимается только в боевом
	// (`production`, `production-strict`): вне боевой посадки у него нет ни
	// фикстуры, ни стенда, которым она была бы нужна (NTF1-G15).
	AuthMode string `envconfig:"KACHO_NOTIFY_AUTH_MODE" knob:"notify.authMode"`

	// PeerTLSCertFile / PeerTLSKeyFile / PeerTLSCAFile — удостоверение notify
	// для mTLS к источникам и kaname (сертификат службы, выпущенный политикой
	// З30) и УЦ, которым проверяется сертификат сервера. Выключателя mTLS нет:
	// без файлов — отказ старта по оси mTLS (NTF1-G15).
	PeerTLSCertFile string `envconfig:"KACHO_NOTIFY_PEER_TLS_CERT_FILE" knob:"notify.peerTLS.certFile"`
	PeerTLSKeyFile  string `envconfig:"KACHO_NOTIFY_PEER_TLS_KEY_FILE" knob:"notify.peerTLS.keyFile"`
	PeerTLSCAFile   string `envconfig:"KACHO_NOTIFY_PEER_TLS_CA_FILE" knob:"notify.peerTLS.caFile"`

	// DB* — соединение с собственной базой `kacho_notify`. Режим шифрования
	// судит общий дескриптор (ось DBSSLMode) — на боевой посадке только
	// безопасные значения.
	DBHost     string `envconfig:"KACHO_NOTIFY_DB_HOST" knob:"notify.db.host"`
	DBPort     string `envconfig:"KACHO_NOTIFY_DB_PORT" knob:"notify.db.port"`
	DBUser     string `envconfig:"KACHO_NOTIFY_DB_USER" knob:"notify.db.user"`
	DBPassword string `envconfig:"KACHO_NOTIFY_DB_PASSWORD" knob:"notify.db.password"`
	DBName     string `envconfig:"KACHO_NOTIFY_DB_NAME" knob:"notify.db.name"`
	DBSSLMode  string `envconfig:"KACHO_NOTIFY_DB_SSLMODE" knob:"notify.db.sslMode"`

	// DiagAddr — адрес диагностической поверхности (`/healthz`, `/readyz`,
	// `/metrics`), досягаемой только внутри кластера (З15).
	DiagAddr string `envconfig:"KACHO_NOTIFY_DIAG_ADDR" knob:"notify.diagAddr"`

	// ── предмет notify ───────────────────────────────────────────────────────

	// ClaimInterval — такт `Claim` по таймеру, в [1s..5m] (NTF1-E04).
	ClaimInterval time.Duration `envconfig:"KACHO_NOTIFY_CLAIM_INTERVAL" knob:"notify.claimInterval"`

	// ResolveSendTimeout — срок вызова `ResolveSend`, в [100ms..30s].
	ResolveSendTimeout time.Duration `envconfig:"KACHO_NOTIFY_RESOLVE_SEND_TIMEOUT" knob:"notify.resolveSendTimeout"`

	// SMTPSessionTimeout — срок одной SMTP-сессии, в [1s..120s].
	SMTPSessionTimeout time.Duration `envconfig:"KACHO_NOTIFY_SMTP_SESSION_TIMEOUT" knob:"notify.smtp.sessionTimeout"`

	// Origin — origin установки: абсолютный `https://` без пути, база ссылок
	// писем (NTF1-G06).
	Origin string `envconfig:"KACHO_NOTIFY_ORIGIN" knob:"notify.origin"`

	// Sources — перечень источников, выведенный чартом (З28): JSON-массив
	// записей `{module, feedAddr, san, classes, recipientForms, authorization}`
	// (NTF1-G01). Разбор — [Config.SourceRoster].
	Sources string `envconfig:"KACHO_NOTIFY_SOURCES" knob:"notify.sources"`

	// SMTPConnectionURI — адрес ретранслятора. В этой полосе страж читает из
	// него только имя пользователя — для пары «имя ⇔ удостоверение» (Д45,
	// NTF1-G15). Обязательность ручки и закрытый разбор адреса заводит полоса
	// N13 (Д44, CX1-77, CX1-79), поэтому тег `optional`.
	SMTPConnectionURI string `envconfig:"KACHO_NOTIFY_SMTP_CONNECTION_URI" knob:"notify.smtp.connectionURI,optional"`

	// credential — удостоверение ретранслятора: три состояния, а не строка
	// (CX1-81 (а)). Читается [os.LookupEnv], а не загрузчиком с подстановкой
	// пустой строки, поэтому тега `envconfig` у поля нет; ручка — [credentialKnob].
	credential Credential

	// sources — разобранный перечень; заполняет [Config.Validate].
	sources []Source

	// unset — ручки, переменной которых нет в окружении вовсе.
	unset map[string]bool
}

// credentialKnob — ручка удостоверения ретранслятора. Переменная — только
// ссылка `secretKeyRef` на объект узла `global.kacho.identity.smtp.credentialSecret`.
var credentialKnob = Knob{
	Name: "notify.smtp.credential",
	Env:  "KACHO_NOTIFY_SMTP_CREDENTIAL",
	Kind: reflect.String,
}

// Credential — удостоверение ретранслятора. Значение в журнал не попадает:
// [Credential.String] его не раскрывает.
type Credential struct {
	present bool
	value   string
}

// Present — переменная удостоверения есть в окружении процесса.
func (c Credential) Present() bool { return c.present }

// Value — удостоверение для `AUTH`. Пустое при ![Credential.Present].
func (c Credential) Value() string { return c.value }

// String не раскрывает значения.
func (c Credential) String() string {
	if !c.present {
		return "не задано"
	}
	return "задано"
}

// Knob — ручка процесса: имя в values и тексте отказа, переменная окружения и
// вид значения.
type Knob struct {
	Name string
	Env  string
	Kind reflect.Kind
	// Optional — незаданная ручка не отказ (решение — у предмета ручки).
	Optional bool
}

func (k Knob) String() string { return k.Name + " (" + k.Env + ")" }

// Knobs — перечень ручек процесса: выводится из тегов [Config] плюс ручка
// удостоверения, которую загрузчик читает отдельно. Второго перечня нет.
func Knobs() []Knob {
	t := reflect.TypeFor[Config]()
	out := make([]Knob, 0, t.NumField()+1)
	for i := range t.NumField() {
		f := t.Field(i)
		env := f.Tag.Get("envconfig")
		if env == "" {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("knob"), ",")
		out = append(out, Knob{Name: name, Env: env, Kind: f.Type.Kind(), Optional: opts == "optional"})
	}
	return append(out, credentialKnob)
}

func knobByEnv(env string) Knob {
	for _, k := range Knobs() {
		if k.Env == env {
			return k
		}
	}
	return Knob{Name: env, Env: env}
}

func knobOfField(field string) Knob {
	f, ok := reflect.TypeFor[Config]().FieldByName(field)
	if !ok {
		panic("config: поля " + field + " нет — перечень ручек разошёлся с кодом стража")
	}
	return knobByEnv(f.Tag.Get("envconfig"))
}

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
	return "notify отказывается стартовать:\n  " + strings.Join(lines, "\n  ")
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

// Load читает ручки из окружения процесса. Значение, которое не разбирается в
// вид ручки, — отказ с её именем; незаданные ручки отмечаются и судятся
// [Config.Validate].
func Load() (Config, error) {
	var c Config
	if err := corecfg.Load(&c); err != nil {
		var pe *envconfig.ParseError
		if errors.As(err, &pe) {
			var fs findings
			fs.add(knobByEnv(pe.KeyName), "значение %q не разбирается: %v", pe.Value, pe.Err)
			return Config{}, fs.err()
		}
		return Config{}, fmt.Errorf("загрузка конфигурации notify: %w", err)
	}
	c.unset = map[string]bool{}
	for _, k := range Knobs() {
		if _, ok := os.LookupEnv(k.Env); !ok {
			c.unset[k.Env] = true
		}
	}
	v, ok := os.LookupEnv(credentialKnob.Env)
	c.credential = Credential{present: ok, value: v}
	return c, nil
}

// Credential — удостоверение ретранслятора (три состояния).
func (c Config) Credential() Credential { return c.credential }

// SourceRoster — перечень источников, разобранный стражем. До успешного
// [Config.Validate] пуст.
func (c Config) SourceRoster() []Source {
	out := make([]Source, len(c.sources))
	copy(out, c.sources)
	return out
}

// Mode — режим посадки для общего дескриптора.
func (c Config) Mode() (servicecontract.Mode, error) { return servicecontract.ParseMode(c.AuthMode) }

// DSN — строка соединения с собственной базой. Пароль и имя экранируются
// построителем адреса, а не склейкой строки.
func (c Config) DSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.DBUser, c.DBPassword),
		Host:     net.JoinHostPort(c.DBHost, c.DBPort),
		Path:     "/" + c.DBName,
		RawQuery: url.Values{"sslmode": []string{c.DBSSLMode}}.Encode(),
	}
	return u.String()
}

// Validate — страж старта notify (ban #16, fail-closed): все находки разом.
//
// Порядок проверок значения не имеет: каждая судит свою ручку и не опирается
// на исход соседней, кроме суммы сроков — она судится только при годных
// слагаемых, иначе отказ одной ручки назывался бы дважды.
func (c *Config) Validate() error {
	var fs findings

	for _, k := range Knobs() {
		if k.Optional || k == credentialKnob {
			continue
		}
		if c.unset[k.Env] {
			fs.add(k, "ручка не задана; умолчания у неё нет")
		}
	}

	c.validatePosture(&fs)
	c.validateDB(&fs)

	c.checkDuration(&fs, "ClaimInterval", c.ClaimInterval, ClaimIntervalMin, ClaimIntervalMax)
	resolveOK := c.checkDuration(&fs, "ResolveSendTimeout", c.ResolveSendTimeout, ResolveSendTimeoutMin, ResolveSendTimeoutMax)
	sessionOK := c.checkDuration(&fs, "SMTPSessionTimeout", c.SMTPSessionTimeout, SMTPSessionTimeoutMin, SMTPSessionTimeoutMax)
	if resolveOK && sessionOK {
		if err := DeadlineSum(c.ResolveSendTimeout, c.SMTPSessionTimeout, feed.LeaseTTL); err != nil {
			fs.add(knobOfField("ResolveSendTimeout"), "%v", err)
		}
	}

	c.validateOrigin(&fs)
	c.validateSources(&fs)
	c.validateRelayCredential(&fs)

	return fs.err()
}

// checkDuration судит длительность по границе. Незаданная уже названа выше и
// второй раз не называется.
func (c *Config) checkDuration(fs *findings, field string, v, lo, hi time.Duration) bool {
	k := knobOfField(field)
	if c.unset[k.Env] {
		return false
	}
	if v < lo || v > hi {
		fs.add(k, "значение %s вне границы [%s..%s]", v, lo, hi)
		return false
	}
	return true
}

// DeadlineSum — страж суммы сроков (УК31, CX1-27): обработка строки под
// сроком `resolveSendTimeout + smtp.sessionTimeout + ackMargin` обязана
// заканчиваться раньше аренды `lease`. Константа аренды здесь — нижняя
// граница ожидания, а не истина: истина — остаток аренды в ответе `Claim` (З21).
func DeadlineSum(resolveSend, session, lease time.Duration) error {
	sum := resolveSend + session + AckMargin
	if sum < lease {
		return nil
	}
	return fmt.Errorf("сумма сроков обработки строки %s (%s %s + %s %s + ackMargin %s) не меньше аренды "+
		"feed.LeaseTTL %s: строка истекла бы раньше, чем её обработка обязана закончиться",
		sum, knobOfField("ResolveSendTimeout"), resolveSend,
		knobOfField("SMTPSessionTimeout"), session, AckMargin, lease)
}

func (c *Config) validatePosture(fs *findings) {
	k := knobOfField("AuthMode")
	if !c.unset[k.Env] {
		mode, err := c.Mode()
		switch {
		case err != nil:
			fs.add(k, "ось authMode: %v", err)
		case !mode.IsProduction():
			fs.add(k, "ось authMode: посадка %q не боевая; notify поднимается только в production "+
				"или production-strict (NTF1-G15)", mode)
		}
	}
	for _, field := range []string{"PeerTLSCertFile", "PeerTLSKeyFile", "PeerTLSCAFile"} {
		fk := knobOfField(field)
		if c.unset[fk.Env] {
			// Незаданность уже названа общим перебором; ось называем в той же
			// находке, а не второй: заменяем текст последней находки этой ручки.
			fs.renameAxis(fk, "ось mTLS: mTLS клиента к источникам и kaname выключен — ручка не задана")
			continue
		}
		if strings.TrimSpace(reflect.ValueOf(*c).FieldByName(field).String()) == "" {
			fs.add(fk, "ось mTLS: mTLS клиента к источникам и kaname выключен — путь файла пуст")
		}
	}
}

// renameAxis заменяет текст уже сделанной находки ручки: незаданная ручка
// оси называется один раз, но с именем оси.
func (fs *findings) renameAxis(k Knob, why string) {
	for i := range *fs {
		if (*fs)[i].Knob == k {
			(*fs)[i].Why = why
			return
		}
	}
	fs.add(k, "%s", why)
}

func (c *Config) validateDB(fs *findings) {
	for _, field := range []string{"DBHost", "DBUser", "DBPassword", "DBName", "DBSSLMode"} {
		k := knobOfField(field)
		if !c.unset[k.Env] && strings.TrimSpace(reflect.ValueOf(*c).FieldByName(field).String()) == "" {
			fs.add(k, "значение пусто")
		}
	}
	if k := knobOfField("DBPort"); !c.unset[k.Env] {
		if _, err := parsePort(c.DBPort); err != nil {
			fs.add(k, "%v", err)
		}
	}
	if k := knobOfField("DiagAddr"); !c.unset[k.Env] {
		_, port, err := net.SplitHostPort(c.DiagAddr)
		if err != nil {
			fs.add(k, "адрес %q не в форме узел:порт: %v", c.DiagAddr, err)
		} else if _, perr := parsePort(port); perr != nil {
			fs.add(k, "%v", perr)
		}
	}
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

func (c *Config) validateOrigin(fs *findings) {
	k := knobOfField("Origin")
	if c.unset[k.Env] {
		return
	}
	u, err := url.Parse(c.Origin)
	switch {
	case err != nil:
		fs.add(k, "origin %q не разбирается: %v", c.Origin, err)
	case u.Scheme != "https":
		fs.add(k, "origin %q: схема обязана быть https", c.Origin)
	case u.Host == "" || u.Hostname() == "":
		fs.add(k, "origin %q: нет узла", c.Origin)
	case u.User != nil:
		fs.add(k, "origin %q: удостоверение в origin не допускается", c.Origin)
	case u.Path != "" || u.RawPath != "":
		fs.add(k, "origin %q: путь не допускается — origin абсолютный без пути", c.Origin)
	case u.RawQuery != "" || u.ForceQuery:
		fs.add(k, "origin %q: параметры запроса не допускаются", c.Origin)
	case u.Fragment != "" || strings.Contains(c.Origin, "#"):
		fs.add(k, "origin %q: фрагмент не допускается", c.Origin)
	}
}

// validateRelayCredential — пара «имя пользователя в адресе ⇔ удостоверение»
// (Д45) в обе стороны и три состояния удостоверения (CX1-81 (а)). Ось G15
// «секрет почты не смонтирован» — половина этой пары (CX1-82 (б)): имя в адресе
// есть, переменной удостоверения нет. Адрес без имени и без удостоверения
// стартует — это приёмник стенда, сессия без `AUTH`.
func (c *Config) validateRelayCredential(fs *findings) {
	cred := c.credential
	if cred.present && cred.value == "" {
		fs.add(credentialKnob, "переменная удостоверения задана, но значение пусто; отсутствие "+
			"удостоверения выражается отсутствием переменной, а не пустой строкой")
		return
	}
	user, ok := c.relayUser(fs)
	if !ok {
		return
	}
	switch {
	case user != "" && !cred.present:
		fs.add(credentialKnob, "секрет почты не смонтирован: в адресе ретранслятора (%s) есть имя "+
			"пользователя, а удостоверения нет (ось G15, пара Д45)", knobOfField("SMTPConnectionURI"))
	case user == "" && cred.present:
		fs.add(credentialKnob, "удостоверение задано, а имени пользователя в адресе ретранслятора (%s) "+
			"нет: удостоверение без имени не применяется ни к какой сессии (пара Д45)",
			knobOfField("SMTPConnectionURI"))
	}
}

// relayUser — раскодированное имя пользователя адреса ретранслятора; пустое,
// если адреса или имени нет. Неразбираемый адрес — находка.
func (c *Config) relayUser(fs *findings) (string, bool) {
	k := knobOfField("SMTPConnectionURI")
	if c.unset[k.Env] || c.SMTPConnectionURI == "" {
		return "", true
	}
	u, err := url.Parse(c.SMTPConnectionURI)
	if err != nil {
		fs.add(k, "адрес ретранслятора не разбирается: %v", redactURLError(err))
		return "", false
	}
	if u.User == nil {
		return "", true
	}
	return u.User.Username(), true
}

// redactURLError снимает из ошибки разбора исходную строку: в адресе может
// стоять пароль, а текст отказа уходит в журнал.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
