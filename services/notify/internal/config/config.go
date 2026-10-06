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
	"github.com/PRO-Robotech/corelib/notify/form"
	"github.com/PRO-Robotech/corelib/servicecontract"

	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
)

// Границы ручек — §8 замысла NTF-1.
const (
	DBMaxConnsMin         = 1
	DBMaxConnsMax         = 100
	ClaimIntervalMin      = time.Second
	ClaimIntervalMax      = 5 * time.Minute
	ResolveSendTimeoutMin = 100 * time.Millisecond
	ResolveSendTimeoutMax = 30 * time.Second
	SMTPSessionTimeoutMin = time.Second
	SMTPSessionTimeoutMax = 120 * time.Second
	// WorkersMin, WorkersMax — граница `notify.workers` (З21): размер `Claim`
	// не больше числа исполнителей и меньше предела ленты feed.MaxClaim.
	WorkersMin = 1
	WorkersMax = 256
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

	// DBMaxConns — ширина пула базы, в [1..100] (полоса D6). Объявлена ручкой,
	// а не умолчанием драйвера (max(4, число ядер узла)): произведение «пул ×
	// реплики» против `max_connections` базы судит гейт развёртывания по
	// объявленной величине. В пул уходит параметром `pool_max_conns` строки
	// соединения ([Config.DSN]).
	DBMaxConns int `envconfig:"KACHO_NOTIFY_DB_MAX_CONNS" knob:"notify.db.maxConns"`

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

	// Workers — число исполнителей строк, в [WorkersMin..WorkersMax] (З21).
	Workers int `envconfig:"KACHO_NOTIFY_WORKERS" knob:"notify.workers"`

	// DeferFor — отсрочка DEFER по `grant_skew`, `platform_unavailable`,
	// `template_skew`, в [feed.MinDefer..feed.MaxDefer] (З23).
	DeferFor time.Duration `envconfig:"KACHO_NOTIFY_DEFER_FOR" knob:"notify.deferFor"`

	// KanameAddr — внутренний слушатель kaname `узел:порт` для `ResolveSend`
	// (ребро notify → kaname, §9 замысла).
	KanameAddr string `envconfig:"KACHO_NOTIFY_KANAME_ADDR" knob:"notify.kaname.addr"`

	// KanameSAN — точный URI SAN (SPIFFE ID) листа kaname: решение о письме
	// принимается только от него, а не от любого листа внутреннего УЦ.
	KanameSAN string `envconfig:"KACHO_NOTIFY_KANAME_SAN" knob:"notify.kaname.san"`

	// Origin — origin установки: абсолютный `https://` без пути, база ссылок
	// писем (NTF1-G06).
	Origin string `envconfig:"KACHO_NOTIFY_ORIGIN" knob:"notify.origin"`

	// Sources — перечень источников, выведенный чартом (З28): JSON-массив
	// записей `{module, feedAddr, san, classes, recipientForms, authorization}`
	// (NTF1-G01). Разбор — [Config.SourceRoster].
	Sources string `envconfig:"KACHO_NOTIFY_SOURCES" knob:"notify.sources"`

	// SMTPConnectionURI — адрес ретранслятора, ОДНА ручка узла почты без
	// умолчания (Д44, CX1-77). Разбор — закрытой таблицей ([parseRelayURI]):
	// схема → режим TLS, узел, порт, имя пользователя; результат — [Config.Relay].
	SMTPConnectionURI string `envconfig:"KACHO_NOTIFY_SMTP_CONNECTION_URI" knob:"notify.smtp.connectionURI"`

	// SMTPFromAddress — адрес отправителя установки (З20 «Отправитель»): форма
	// `notify/address` с непустым доменом. Его домен — домен `From` и домен
	// проверок DNS установки (§12а, Д101); результат — [Config.FromDomain].
	SMTPFromAddress string `envconfig:"KACHO_NOTIFY_SMTP_FROM_ADDRESS" knob:"notify.smtp.fromAddress"`

	// SMTPFromName — имя отправителя установки (заголовок From, З25): форма
	// `notify/form.HeaderText`; результат — [Config.FromName].
	SMTPFromName string `envconfig:"KACHO_NOTIFY_SMTP_FROM_NAME" knob:"notify.smtp.fromName"`

	// ── DNS установки (Р19, §12а) ───────────────────────────────────────────

	// DNSBootDeadline — срок стража DNS на старте, в [10s..10m] (NTF1-P13).
	DNSBootDeadline time.Duration `envconfig:"KACHO_NOTIFY_DNS_BOOT_DEADLINE" knob:"notify.dns.bootDeadline"`

	// DNSRecheckInterval — интервал перепроверки DNS, в [1m..24h] (NTF1-P13).
	DNSRecheckInterval time.Duration `envconfig:"KACHO_NOTIFY_DNS_RECHECK_INTERVAL" knob:"notify.dns.recheckInterval"`

	// DKIMKeyFile / DKIMSelectorFile — пути файлов ключа и селектора DKIM в
	// томе объекта, смонтированном без `subPath` (§12а «Ключ и селектор DKIM»).
	// Оба пути — в одном каталоге тома; пара читается из одного поколения.
	DKIMKeyFile      string `envconfig:"KACHO_NOTIFY_DKIM_KEY_FILE" knob:"notify.dkim.keyFile"`
	DKIMSelectorFile string `envconfig:"KACHO_NOTIFY_DKIM_SELECTOR_FILE" knob:"notify.dkim.selectorFile"`

	// StandDNS — зона DNS стенда (Д104): `off` либо хост приёмника стенда,
	// равный хосту адреса ретранслятора. Не адрес резолвера и не обход
	// проверки: страж DNS исполняется на резолвере пода при любом значении.
	StandDNS string `envconfig:"KACHO_NOTIFY_STAND_DNS" knob:"notify.standDNS"`

	// ── лимиты Р10 (З24, NTF1-H08) ──────────────────────────────────────────

	// LimitSecurityPerDay — сетка security на адресата за сутки UTC, в [1..1000].
	LimitSecurityPerDay int `envconfig:"KACHO_NOTIFY_LIMITS_RECIPIENT_SECURITY_PER_DAY" knob:"notify.limits.recipient.security.perDay"`

	// LimitNoticePerHour — сетка notice на адресата за час, в [1..10000].
	LimitNoticePerHour int `envconfig:"KACHO_NOTIFY_LIMITS_RECIPIENT_NOTICE_PER_HOUR" knob:"notify.limits.recipient.notice.perHour"`

	// LimitNoticePerDay — сетка notice на адресата за сутки UTC, в [1..10000].
	LimitNoticePerDay int `envconfig:"KACHO_NOTIFY_LIMITS_RECIPIENT_NOTICE_PER_DAY" knob:"notify.limits.recipient.notice.perDay"`

	// LimitGlobalPerDay — суточный потолок потока установки, в [1..10000000].
	LimitGlobalPerDay int `envconfig:"KACHO_NOTIFY_LIMITS_GLOBAL_PER_DAY" knob:"notify.limits.global.perDay"`

	// SourceLimits — ручки на источник: JSON-объект «модуль → {rate, burst,
	// paused}», параметризующий перечень [Config.Sources] (Р10). Разбор и
	// страж — validateSourceLimits.
	SourceLimits string `envconfig:"KACHO_NOTIFY_SOURCE_LIMITS" knob:"notify.sourceLimits"`

	// credential — удостоверение ретранслятора: три состояния, а не строка
	// (CX1-81 (а)). Читается [os.LookupEnv], а не загрузчиком с подстановкой
	// пустой строки, поэтому тега `envconfig` у поля нет; ручка — [credentialKnob].
	credential Credential

	// recipientKey — ключ сетки на адресата (З24): ключевой материал HMAC.
	// Читается [os.LookupEnv], а не загрузчиком: значение в перепись и в
	// строковый вид не попадает; ручка — [recipientKeyKnob].
	recipientKey RecipientKey

	// trustAnchor — путь якоря проверки листа ретранслятора; trustAnchorSet —
	// переменная есть в окружении. Читается [os.LookupEnv]: отсутствие —
	// законное значение (доверенный набор — корневое хранилище образа), поэтому
	// тега `envconfig` у поля нет; ручка — [trustAnchorKnob].
	trustAnchor    string
	trustAnchorSet bool

	// fromName — разобранное имя отправителя; заполняет [Config.Validate].
	fromName form.HeaderText

	// sources — разобранный перечень; заполняет [Config.Validate].
	sources []Source

	// relay — разобранный адрес ретранслятора; relayParsed — разбор удался.
	// Заполняет [Config.Validate].
	relay       RelayAddress
	relayParsed bool

	// fromDomain — домен адреса отправителя; заполняет [Config.Validate].
	fromDomain string

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

// trustAnchorKnob — якорь проверки листа ретранслятора. Переменную чарт
// рендерит только при объявленном якоре узла почты (`trustAnchorSecret`);
// без неё доверенный набор — корневое хранилище образа.
var trustAnchorKnob = Knob{
	Name: "notify.smtp.trustAnchorFile",
	Env:  "KACHO_NOTIFY_SMTP_TRUST_ANCHOR_FILE",
	Kind: reflect.String,
}

// recipientKeyKnob — ключ сетки на адресата (З24). Переменная — только ссылка
// `secretKeyRef` на объект `<полное имя>-recipient-key` чарта notify.
var recipientKeyKnob = Knob{
	Name: "notify.recipientKey",
	Env:  "KACHO_NOTIFY_RECIPIENT_KEY",
	Kind: reflect.String,
}

// RecipientKeyMinBytes — нижняя граница длины ключа сетки (Д89). Ключ —
// материал HMAC-SHA256 (строки сетки на адресата и отпечаток ограды ключа,
// З24): короче размера выхода хеша он ослабляет и то, и другое, а отпечаток
// ключа в аннотации пода становится оракулом перебора. Граница включена;
// число одно на страж и на сетку — [limits.KeyMinBytes].
const RecipientKeyMinBytes = limits.KeyMinBytes

// RecipientKey — ключ сетки на адресата. Значение в журнал не попадает:
// [RecipientKey.String] его не раскрывает.
type RecipientKey struct {
	value []byte
}

// Bytes — копия ключа для HMAC.
func (k RecipientKey) Bytes() []byte { return append([]byte(nil), k.value...) }

// String не раскрывает значения.
func (k RecipientKey) String() string {
	if len(k.value) == 0 {
		return "не задан"
	}
	return "задан"
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
		out = append(out, Knob{Name: f.Tag.Get("knob"), Env: env, Kind: f.Type.Kind()})
	}
	return append(out, credentialKnob, recipientKeyKnob, trustAnchorKnob)
}

func knobByEnv(env string) Knob {
	for _, k := range Knobs() {
		if k.Env == env {
			return k
		}
	}
	return Knob{Name: env, Env: env}
}

// KnobOfField — ручка поля [Config] по его тегу: имя для текста отказа там,
// где ручку судит не этот пакет (материал удостоверения пира — корень).
// Паника на неизвестном имени недостижима, пока каждый вызов передаёт имя
// существующего поля в форме, видимой разбору: это судит перепись
// TestKnobOfFieldIsCalledOnlyWithExistingFields (GS-I4).
func KnobOfField(field string) Knob {
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
	c.trustAnchor, c.trustAnchorSet = os.LookupEnv(trustAnchorKnob.Env)
	if key, ok := os.LookupEnv(recipientKeyKnob.Env); ok {
		c.recipientKey = RecipientKey{value: []byte(key)}
	}
	return c, nil
}

// RecipientKey — ключ сетки на адресата; годен только после успешного
// [Config.Validate].
func (c Config) RecipientKey() RecipientKey { return c.recipientKey }

// Credential — удостоверение ретранслятора (три состояния).
func (c Config) Credential() Credential { return c.credential }

// SourceRoster — перечень источников, разобранный стражем. До успешного
// [Config.Validate] пуст.
func (c Config) SourceRoster() []Source {
	out := make([]Source, len(c.sources))
	copy(out, c.sources)
	return out
}

// FromName — имя отправителя установки; годно только после успешного
// [Config.Validate].
func (c Config) FromName() form.HeaderText { return c.fromName }

// TrustAnchorFile — путь якоря проверки листа ретранслятора; ok=false —
// якоря нет, доверенный набор — корневое хранилище образа.
func (c Config) TrustAnchorFile() (string, bool) { return c.trustAnchor, c.trustAnchorSet }

// Mode — режим посадки для общего дескриптора.
func (c Config) Mode() (servicecontract.Mode, error) { return servicecontract.ParseMode(c.AuthMode) }

// DSN — строка соединения с собственной базой. Пароль и имя экранируются
// построителем адреса, а не склейкой строки.
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

// Validate — страж старта notify (ban #16, fail-closed): все находки разом.
//
// Каждая проверка судит свою ручку. На исход соседней опираются три — суммы
// сроков (судится только при годных слагаемых), пары «имя ⇔ удостоверение» и
// зоны DNS стенда (судятся только на разобранном адресе ретранслятора, поэтому
// [Config.validateRelay] стоит раньше них): иначе отказ одной ручки назывался
// бы дважды.
func (c *Config) Validate() error {
	var fs findings

	for _, k := range Knobs() {
		if k == credentialKnob || k == trustAnchorKnob {
			// Отсутствие этих двух — законное значение; их судят свои стражи.
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
			fs.add(KnobOfField("ResolveSendTimeout"), "%v", err)
		}
	}

	c.checkInt(&fs, "DBMaxConns", c.DBMaxConns, DBMaxConnsMin, DBMaxConnsMax)
	c.validateDelivery(&fs)
	c.validateOrigin(&fs)
	c.validateSources(&fs)
	c.validateGrid(&fs)
	c.validateSourceLimits(&fs)
	c.validateRelay(&fs)
	c.validateRelayCredential(&fs)
	c.validateFromAddress(&fs)
	c.validateRecipientKey(&fs)
	c.validateDNS(&fs)
	c.validateDKIM(&fs)
	c.validateStandDNS(&fs)

	return fs.err()
}

// checkDuration судит длительность по границе. Незаданная уже названа выше и
// второй раз не называется.
func (c *Config) checkDuration(fs *findings, field string, v, lo, hi time.Duration) bool {
	k := KnobOfField(field)
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
		sum, KnobOfField("ResolveSendTimeout"), resolveSend,
		KnobOfField("SMTPSessionTimeout"), session, AckMargin, lease)
}

func (c *Config) validatePosture(fs *findings) {
	k := KnobOfField("AuthMode")
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
		fk := KnobOfField(field)
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
		k := KnobOfField(field)
		if !c.unset[k.Env] && strings.TrimSpace(reflect.ValueOf(*c).FieldByName(field).String()) == "" {
			fs.add(k, "значение пусто")
		}
	}
	if k := KnobOfField("DBPort"); !c.unset[k.Env] {
		if _, err := parsePort(c.DBPort); err != nil {
			fs.add(k, "%v", err)
		}
	}
	if k := KnobOfField("DiagAddr"); !c.unset[k.Env] {
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
	k := KnobOfField("Origin")
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

// validateRecipientKey — страж ключа сетки (Д89, fail-closed): незаданный уже
// назван общим перебором; заданный короче [RecipientKeyMinBytes] — отказ.
// Ни значение, ни его длина в текст отказа не попадают.
func (c *Config) validateRecipientKey(fs *findings) {
	if c.unset[recipientKeyKnob.Env] {
		return
	}
	if len(c.recipientKey.value) < RecipientKeyMinBytes {
		fs.add(recipientKeyKnob, "ключ сетки короче %d байт — ключ HMAC-SHA256 обязан быть не короче "+
			"выхода хеша (З24, Д89)", RecipientKeyMinBytes)
	}
}

// validateRelayCredential — пара «имя пользователя в адресе ⇔ удостоверение»
// (Д45) в обе стороны и три состояния удостоверения (CX1-81 (а)). Ось G15
// «секрет почты не смонтирован» — половина этой пары (CX1-82 (б)): имя в адресе
// есть, переменной удостоверения нет. Адрес без имени и без удостоверения
// стартует — это приёмник стенда, сессия без `AUTH`. Пара судится только на
// разобранном адресе: неразобранный уже назван своей находкой.
func (c *Config) validateRelayCredential(fs *findings) {
	cred := c.credential
	if cred.present && cred.value == "" {
		fs.add(credentialKnob, "переменная удостоверения задана, но значение пусто; отсутствие "+
			"удостоверения выражается отсутствием переменной, а не пустой строкой")
		return
	}
	if !c.relayParsed {
		return
	}
	user := c.relay.Username
	switch {
	case user != "" && !cred.present:
		fs.add(credentialKnob, "секрет почты не смонтирован: в адресе ретранслятора (%s) есть имя "+
			"пользователя, а удостоверения нет (ось G15, пара Д45)", KnobOfField("SMTPConnectionURI"))
	case user == "" && cred.present:
		fs.add(credentialKnob, "удостоверение задано, а имени пользователя в адресе ретранслятора (%s) "+
			"нет: удостоверение без имени не применяется ни к какой сессии (пара Д45)",
			KnobOfField("SMTPConnectionURI"))
	}
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
