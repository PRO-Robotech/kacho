// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// required_knobs_test.go — держатель Р16 приёмки NTF-4 на стадии S1 (§8,
// строка «Р16»; DoD S1 п.5; З22): все 21 ручка NTF-4 стадии S1 объявлены в
// загрузчике `notify-sender` группой NTF-4, у каждой нет умолчания, у каждой —
// границы таблицы Р16, и окружение вне загрузчика не читается.
//
// Что судится и в каком порядке (порядок несущий: проба возможности
// испытуемого стоит ПОСЛЕ всей проверки фикстуры, иначе сломанная фикстура
// выдала бы себя за отсутствующую возможность):
//
//  1. Предпосылка — таблица пробы и фикстура. Таблица несёт 21 ручку без
//     повторов; эталон границ (s1Model — синтетический испытуемый, собранный
//     по таблице Р16) принимает фикстуру целиком и отвечает на каждую строку
//     таблицы так, как строка ждёт: значения на границе — старт, за
//     границей — отказ ровно по этой ручке. Фикстура NTF-1 стартует
//     настоящим загрузчиком. Провал здесь — «НЕ ВЫПОЛНИЛОСЬ», а не красный.
//  2. Группы. Каждая ручка загрузчика ([config.Knobs]) несёт группу — поле
//     `Group` ручки со значением `NTF-1` либо `NTF-4`; группа NTF-4 равна
//     перечню S1 Р16 в обе стороны (ручка стадии S3 в ней — находка); в группе
//     NTF-1 нет ни одной ручки перечня Р16; ручка не повторяется.
//  3. Каждая ручка: снята при заданных остальных — отказ ровно с её именем;
//     пустая — то же; на каждой границе (зависимые ручки подобраны внутри их
//     границ) — старт, и загруженное значение равно поданному; за каждой
//     границей — отказ ровно с её именем и названной границей.
//  4. Зависимая граница окна долей (Д26·6, Р12): срок журнала 7 сут и окно
//     30 сут — отказ, текст называет обе ручки; близнец — обе по 7 сут, старт.
//  5. Разбором исходников: ни один файл `services/notify` вне загрузчика не
//     читает окружение.
//
// Способность упасть доказана инъекциями на синтетике
// (required_knobs_injection_test.go): умолчание у одной ручки, ручка вне
// перечня Р16, чтение окружения в обход загрузчика, снятая граница у одной
// ручки, снятая зависимая граница `…_REPUTATION_WINDOW ≤ …_SENT_LOG_RETENTION`.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/treecorpus"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// Переменные ручек NTF-4 стадии S1 (Р16).
const (
	envMailboxAddr       = "KACHO_NOTIFY_FEEDBACK_MAILBOX_ADDR"
	envMailboxLocalPart  = "KACHO_NOTIFY_FEEDBACK_MAILBOX_LOCAL_PART"
	envReturnDomain      = "KACHO_NOTIFY_RETURN_DOMAIN"
	envPollInterval      = "KACHO_NOTIFY_FEEDBACK_POLL_INTERVAL"
	envPollStaleMax      = "KACHO_NOTIFY_FEEDBACK_POLL_STALE_MAX"
	envMaxMessageBytes   = "KACHO_NOTIFY_FEEDBACK_MAX_MESSAGE_BYTES"
	envMaxExpansionRatio = "KACHO_NOTIFY_FEEDBACK_MAX_EXPANSION_RATIO"
	envCanaryInterval    = "KACHO_NOTIFY_FEEDBACK_CANARY_INTERVAL"
	envCanaryDeadline    = "KACHO_NOTIFY_FEEDBACK_CANARY_DEADLINE"
	envLeaseTTL          = "KACHO_NOTIFY_FEEDBACK_LEASE_TTL"
	envSentLogRetention  = "KACHO_NOTIFY_SENT_LOG_RETENTION"
	envHardTTL           = "KACHO_NOTIFY_SUPPRESSION_HARD_TTL"
	envSoftThreshold     = "KACHO_NOTIFY_SUPPRESSION_SOFT_THRESHOLD"
	envSoftWindow        = "KACHO_NOTIFY_SUPPRESSION_SOFT_WINDOW"
	envSoftTTL           = "KACHO_NOTIFY_SUPPRESSION_SOFT_TTL"
	envSweepInterval     = "KACHO_NOTIFY_SUPPRESSION_SWEEP_INTERVAL"
	envSecretReload      = "KACHO_NOTIFY_SECRET_RELOAD_INTERVAL"
	envAddressKeyDir     = "KACHO_NOTIFY_ADDRESS_KEY_DIR"
	envHardBounceRateMax = "KACHO_NOTIFY_REPUTATION_HARD_BOUNCE_RATE_MAX"
	envComplaintRateMax  = "KACHO_NOTIFY_REPUTATION_COMPLAINT_RATE_MAX"
	envReputationWindow  = "KACHO_NOTIFY_REPUTATION_WINDOW"
)

// s1Envs — перечень ручек NTF-4 стадии S1 (Р16, «Какое развёртывание какую
// ручку читает»: `notify-sender` — 21 ручка стадии S1). Перечень закрыт.
var s1Envs = []string{
	envMailboxAddr, envMailboxLocalPart, envReturnDomain, envPollInterval, envPollStaleMax,
	envMaxMessageBytes, envMaxExpansionRatio, envCanaryInterval, envCanaryDeadline, envLeaseTTL,
	envSentLogRetention, envHardTTL, envSoftThreshold, envSoftWindow, envSoftTTL, envSweepInterval,
	envSecretReload, envAddressKeyDir,
	envHardBounceRateMax, envComplaintRateMax, envReputationWindow,
}

// s1Count — число ручек стадии S1 по Р16 (после Д96: ручки DNS ушли в NTF-1).
const s1Count = 21

// s3Envs — десять ручек NTF-4 стадии S3 (Р16, Р20). В группе NTF-4 загрузчика
// `notify-sender` их быть не должно (Р22): его читатель — `notify-api`.
var s3Envs = []string{
	"KACHO_NOTIFY_AUTHZ_TRUST_DOMAIN", "KACHO_NOTIFY_AUTHZ_TRUSTED_FORWARDER_SANS",
	"KACHO_NOTIFY_AUTHZ_CACHE_TTL", "KACHO_NOTIFY_AUTHZ_CHECK_TIMEOUT",
	"KACHO_NOTIFY_AUTHZ_DENY_BUDGET_PER_SEC", "KACHO_NOTIFY_INTERNAL_PORT",
	"KACHO_NOTIFY_INTERNAL_SERVER_MTLS_CERTFILE", "KACHO_NOTIFY_INTERNAL_SERVER_MTLS_KEYFILE",
	"KACHO_NOTIFY_INTERNAL_SERVER_MTLS_CLIENTCAFILES", "KACHO_NOTIFY_HANDLING_BUDGET",
}

// Группы ручек загрузчика (Р16 «Держатель»).
const (
	groupNTF1 = "NTF-1"
	groupNTF4 = "NTF-4"
)

// Ключ отпечатка в каталоге `KACHO_NOTIFY_ADDRESS_KEY_DIR` (Р15, Д23).
//
// Имя файла и форму ключа приёмка не называет (вопрос к приёмке в возврате
// полосы RED). Проба берёт форму ключа сетки NTF-1 — того же ключа, которым по
// Р15 ключуется сетка: материал HMAC-SHA256 записан 64 шестнадцатеричными
// символами (32 октета, [config.RecipientKeyMinBytes]), ключ объекта —
// `addressKey`. Форма названа в одном месте.
const addressKeyFileName = "addressKey"

func validAddressKey() []byte {
	return []byte(addressKeyOfOctets(config.AddressKeyMinBytes))
}

// addressKeyOfOctets — материал ключа отпечатка ровно n октетов в
// шестнадцатеричной записи. Октеты различимы (0x00, 0x01, …): запись ключа не
// совпадает ни с одной подстрокой текста отказа, кроме самой себя, — проба
// «текст отказа не несёт материала» судит именно материал.
func addressKeyOfOctets(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return hex.EncodeToString(b)
}

// addressKeyDir — каталог формы kubelet с годным файлом ключа отпечатка.
func addressKeyDir(t *testing.T) string {
	t.Helper()
	return kubeletDir(t, map[string][]byte{addressKeyFileName: validAddressKey()})
}

// addressKeyDirOf — выпуск каталога формы kubelet с файлом ключа body.
func addressKeyDirOf(body string) func(t *testing.T) string {
	return func(t *testing.T) string {
		t.Helper()
		return kubeletDir(t, map[string][]byte{addressKeyFileName: []byte(body)})
	}
}

// ── таблица границ ─────────────────────────────────────────────────────────

// probeValue — одно значение ручки с подобранными зависимыми ручками.
// gen, если задан, выпускает значение во время пробы (каталог ключа).
// want — загруженное значение на границе; пусто — равно v.
// bound — слова, из которых текст отказа обязан нести хотя бы одно (граница).
// form — нарушена форма значения, а не числовая граница: текст обязан назвать
// ручку, слова границы не требуются. Значение «за границей» без bound и без
// form — ошибка таблицы, её ловит предпосылка.
// secret — материал секрета, поданный пробой: текст отказа его не несёт
// (Р16: «значение секрета не печатает»).
type probeValue struct {
	what   string
	v      string
	gen    func(t *testing.T) string
	deps   map[string]string
	want   string
	bound  []string
	form   bool
	secret []string
}

// knobRow — строка таблицы Р16 для одной ручки: значения на каждой границе
// и за каждой.
type knobRow struct {
	env    string
	edges  []probeValue
	beyond []probeValue
}

func durBound(d time.Duration) []string {
	full := d.String()
	short := full
	if strings.HasSuffix(short, "m0s") {
		short = strings.TrimSuffix(short, "0s")
	}
	if strings.HasSuffix(short, "h0m") {
		short = strings.TrimSuffix(short, "0m")
	}
	return []string{full, short}
}

func dur(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic(err)
	}
	return d
}

func edge(v string, deps map[string]string) probeValue {
	return probeValue{what: "на границе " + v, v: v, deps: deps}
}

func over(v string, deps map[string]string, bound ...string) probeValue {
	return probeValue{what: "за границей " + v, v: v, deps: deps, bound: bound}
}

// formOver — значение не той формы: отказ обязан назвать ручку.
func formOver(v string) probeValue {
	return probeValue{what: "не той формы " + v, v: v, form: true}
}

func overDur(v string, b string, deps map[string]string) probeValue {
	return over(v, deps, durBound(dur(b))...)
}

func durRow(env, lo, hi string) knobRow {
	return knobRow{
		env:   env,
		edges: []probeValue{edge(lo, nil), edge(hi, nil)},
		beyond: []probeValue{
			overDur((dur(lo) - time.Second).String(), lo, nil),
			overDur((dur(hi) + time.Second).String(), hi, nil),
		},
	}
}

func intRow(env string, lo, hi int64) knobRow {
	l, h := strconv.FormatInt(lo, 10), strconv.FormatInt(hi, 10)
	return knobRow{
		env:   env,
		edges: []probeValue{edge(l, nil), edge(h, nil)},
		beyond: []probeValue{
			over(strconv.FormatInt(lo-1, 10), nil, l),
			over(strconv.FormatInt(hi+1, 10), nil, h),
		},
	}
}

func floatRow(env, lo, below, hi, above string) knobRow {
	return knobRow{
		env:    env,
		edges:  []probeValue{edge(lo, nil), edge(hi, nil)},
		beyond: []probeValue{over(below, nil, lo), over(above, nil, hi)},
	}
}

// s1Rows — таблица Р16 стадии S1. У строгой верхней границы «на границе» —
// на 1 с ниже неё, «за границей» — ровно она (Р16 «Держатель»).
func s1Rows() []knobRow {
	pollStaleLow := map[string]string{envLeaseTTL: "2m"} // POLL_INTERVAL=1m: аренда ∈ [2m, STALE−1m)
	return []knobRow{
		{
			env: envMailboxAddr,
			edges: []probeValue{
				edge("pop3.example.invalid:1", nil),
				edge("pop3.example.invalid:65535", nil),
			},
			beyond: []probeValue{
				over("pop3.example.invalid:0", nil, "65535"),
				over("pop3.example.invalid:65536", nil, "65535"),
				formOver("pop3.example.invalid"),
			},
		},
		{
			env:   envMailboxLocalPart,
			edges: []probeValue{edge("b", nil), edge(strings.Repeat("b", 15), nil)},
			beyond: []probeValue{
				over(strings.Repeat("b", 16), nil, "15", "64"),
				formOver("b+x"),
				formOver("b..x"),
			},
		},
		{
			env:    envReturnDomain,
			edges:  []probeValue{edge("a.b", nil)},
			beyond: []probeValue{formOver("localhost"), formOver("-a.example.invalid"), formOver("a_b.example.invalid")},
		},
		{
			env: envPollInterval,
			edges: []probeValue{
				edge("5s", nil),
				edge("10m", map[string]string{envPollStaleMax: "40m", envLeaseTTL: "20m"}),
			},
			beyond: []probeValue{
				overDur("4s", "5s", nil),
				overDur("10m1s", "10m", map[string]string{envPollStaleMax: "24h", envLeaseTTL: "21m"}),
			},
		},
		{
			env: envPollStaleMax,
			edges: []probeValue{
				edge("4m", pollStaleLow), // 4 × POLL_INTERVAL
				edge("65s", map[string]string{envPollInterval: "5s", envLeaseTTL: "30s"}), // POLL_INTERVAL + 60s
				edge("24h", nil),
			},
			beyond: []probeValue{
				overDur("3m59s", "4m", pollStaleLow),
				overDur("64s", "65s", map[string]string{envPollInterval: "5s", envLeaseTTL: "30s"}),
				overDur("24h0m1s", "24h", nil),
			},
		},
		intRow(envMaxMessageBytes, 64<<10, 25<<20),
		intRow(envMaxExpansionRatio, 1, 100),
		{
			env: envCanaryInterval,
			edges: []probeValue{
				edge("1m", map[string]string{envCanaryDeadline: "30s"}),
				edge("24h", nil),
			},
			beyond: []probeValue{
				overDur("59s", "1m", map[string]string{envCanaryDeadline: "30s"}),
				overDur("24h0m1s", "24h", nil),
			},
		},
		{
			env:   envCanaryDeadline,
			edges: []probeValue{edge("30s", nil), edge("59m59s", nil)}, // строго < CANARY_INTERVAL=1h
			beyond: []probeValue{
				overDur("29s", "30s", nil),
				over("1h", nil, append(durBound(time.Hour), envCanaryInterval)...),
			},
		},
		{
			env: envLeaseTTL,
			edges: []probeValue{
				edge("2m", nil),     // 2 × POLL_INTERVAL при 1m
				edge("28m59s", nil), // строго < POLL_STALE_MAX − POLL_INTERVAL = 29m
				edge("30s", map[string]string{envPollInterval: "5s"}),
			},
			beyond: []probeValue{
				overDur("1m59s", "2m", nil),
				over("29m", nil, append(durBound(29*time.Minute), envPollStaleMax)...),
				overDur("29s", "30s", map[string]string{envPollInterval: "5s"}),
			},
		},
		{
			env:   envSentLogRetention,
			edges: []probeValue{edge("168h", nil), edge("2160h", nil)},
			beyond: []probeValue{
				overDur("167h59m59s", "168h", nil),
				overDur("2160h0m1s", "2160h", nil),
			},
		},
		durRow(envHardTTL, "1h", "72h"),
		intRow(envSoftThreshold, 2, 20),
		durRow(envSoftWindow, "1h", "168h"),
		durRow(envSoftTTL, "1h", "168h"),
		durRow(envSweepInterval, "1m", "24h"),
		durRow(envSecretReload, "5s", "10m"),
		{
			env: envAddressKeyDir,
			edges: []probeValue{{
				what: fmt.Sprintf("на границе: ключ ровно %d октетов", config.AddressKeyMinBytes),
				gen:  addressKeyDirOf(addressKeyOfOctets(config.AddressKeyMinBytes)),
			}},
			beyond: []probeValue{
				{
					what:   fmt.Sprintf("за границей: ключ на один октет короче (%d)", config.AddressKeyMinBytes-1),
					gen:    addressKeyDirOf(addressKeyOfOctets(config.AddressKeyMinBytes - 1)),
					bound:  []string{strconv.Itoa(config.AddressKeyMinBytes)},
					secret: []string{addressKeyOfOctets(config.AddressKeyMinBytes - 1)},
				},
				{what: "за границей: относительный путь", v: "keys/address", form: true},
				{form: true, what: "за границей: каталога нет", gen: func(t *testing.T) string {
					return filepath.Join(t.TempDir(), "absent")
				}},
				{form: true, what: "за границей: в каталоге нет файла ключа", gen: func(t *testing.T) string {
					return kubeletDir(t, map[string][]byte{"other": validAddressKey()})
				}},
				{
					form:   true,
					what:   "за границей: ключ не разбирается",
					gen:    addressKeyDirOf("not-a-key"),
					secret: []string{"not-a-key"},
				},
			},
		},
		floatRow(envHardBounceRateMax, "0.001", "0.0009", "0.2", "0.2001"),
		floatRow(envComplaintRateMax, "0.0001", "0.00009", "0.05", "0.0501"),
		{
			env: envReputationWindow,
			edges: []probeValue{
				edge("1h", nil),
				edge("720h", nil), // SENT_LOG_RETENTION фикстуры = 720h
			},
			beyond: []probeValue{
				overDur("59m59s", "1h", nil),
				overDur("720h0m1s", "720h", map[string]string{envSentLogRetention: "2160h"}),
			},
		},
	}
}

// ── испытуемый: старт и его исход ──────────────────────────────────────────

// bootFinding — одна находка отказа старта: переменная ручки и строка текста.
type bootFinding struct {
	env, line string
}

// bootResult — исход старта. refused == nil и other == nil — старт принят.
type bootResult struct {
	refused []bootFinding
	other   error
	loaded  map[string]reflect.Value
	missing []string // ручки S1 без поля в перечне Config (только при старте)
}

func (r bootResult) started() bool { return r.refused == nil && r.other == nil }

func (r bootResult) text() string {
	if r.other != nil {
		return r.other.Error()
	}
	lines := make([]string, 0, len(r.refused))
	for _, f := range r.refused {
		lines = append(lines, f.line)
	}
	return strings.Join(lines, "\n")
}

// starter — старт с правками фикстуры: значение заменяет строку, nil снимает.
type starter func(t *testing.T, edits map[string]*string) bootResult

// realStart — настоящий загрузчик и страж: ровно то, что корень делает до
// подъёма чего бы то ни было.
func realStart(t *testing.T, edits map[string]*string) bootResult {
	t.Helper()
	useFixture(t, edits)
	cfg, err := config.Load()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		var r *config.RefusalError
		if !errors.As(err, &r) {
			return bootResult{other: err}
		}
		res := bootResult{refused: []bootFinding{}}
		for _, f := range r.Findings {
			res.refused = append(res.refused, bootFinding{env: f.Knob.Env, line: f.Knob.String() + ": " + f.Why})
		}
		return res
	}
	res := bootResult{loaded: map[string]reflect.Value{}}
	v := reflect.ValueOf(cfg)
	for _, env := range s1Envs {
		found := false
		for i := range v.Type().NumField() {
			if v.Type().Field(i).Tag.Get("envconfig") == env {
				res.loaded[env] = v.Field(i)
				found = true
				break
			}
		}
		if !found {
			res.missing = append(res.missing, env)
		}
	}
	return res
}

// ── судьи: чистые функции над исходом, общие для дерева и инъекций ──────────

// sameLoaded — загруженное значение равно поданному в виде поля.
func sameLoaded(got reflect.Value, want string) error {
	switch {
	case got.Type() == reflect.TypeFor[time.Duration]():
		d, err := time.ParseDuration(want)
		if err != nil {
			return err
		}
		if time.Duration(got.Int()) != d {
			return fmt.Errorf("загружено %v, подано %v", time.Duration(got.Int()), d)
		}
	case got.CanInt():
		n, err := strconv.ParseInt(want, 10, 64)
		if err != nil {
			return err
		}
		if got.Int() != n {
			return fmt.Errorf("загружено %d, подано %d", got.Int(), n)
		}
	case got.CanUint():
		n, err := strconv.ParseUint(want, 10, 64)
		if err != nil {
			return err
		}
		if got.Uint() != n {
			return fmt.Errorf("загружено %d, подано %d", got.Uint(), n)
		}
	case got.CanFloat():
		f, err := strconv.ParseFloat(want, 64)
		if err != nil {
			return err
		}
		if got.Float() != f {
			return fmt.Errorf("загружено %v, подано %v", got.Float(), f)
		}
	case got.Kind() == reflect.String:
		if got.String() != want {
			return fmt.Errorf("загружено %q, подано %q", got.String(), want)
		}
	default:
		return fmt.Errorf("поле вида %s пробой не сравнивается", got.Type())
	}
	return nil
}

// judgeRefusal — отказ ровно одной находкой по env, текст называет env и,
// кроме отказа по форме (form), хотя бы одно слово границы bound.
func judgeRefusal(res bootResult, env string, bound []string, form bool) string {
	if !form && len(bound) == 0 {
		return "ошибка таблицы пробы: у отказа по границе не названа граница"
	}
	switch {
	case res.started():
		return "старт принят, ожидался отказ с именем ручки " + env
	case res.other != nil:
		return "отказ не в форме RefusalError: " + res.other.Error()
	case len(res.refused) != 1:
		return fmt.Sprintf("ожидалась ровно одна находка по %s, получено %d:\n%s", env, len(res.refused), res.text())
	case res.refused[0].env != env:
		return fmt.Sprintf("находка называет ручку %s, ожидалась %s: %s", res.refused[0].env, env, res.text())
	case !strings.Contains(res.text(), env):
		return "текст отказа не называет " + env + ": " + res.text()
	}
	if !form && !containsAny(res.text(), bound...) {
		return fmt.Sprintf("текст отказа не называет границу (одно из %q): %s", bound, res.text())
	}
	return ""
}

// judgeKnob — все пробы одной ручки; находки — строки красного.
func judgeKnob(t *testing.T, row knobRow, run starter) []string {
	t.Helper()
	var out []string
	add := func(what, msg string) {
		if msg != "" {
			out = append(out, row.env+" "+what+": "+msg)
		}
	}
	add("снята", judgeRefusal(run(t, map[string]*string{row.env: nil}), row.env, nil, true))
	add("пуста", judgeRefusal(run(t, map[string]*string{row.env: str("")}), row.env, nil, true))
	for _, pv := range row.edges {
		v := pv.v
		if pv.gen != nil {
			v = pv.gen(t)
		}
		edits := map[string]*string{row.env: str(v)}
		for k, dv := range pv.deps {
			edits[k] = str(dv)
		}
		res := run(t, edits)
		if !res.started() {
			add(pv.what, "старт отвергнут: "+res.text())
			continue
		}
		if slices.Contains(res.missing, row.env) {
			add(pv.what, "старт принят, но загрузчик ручку не читает: в перечне Config нет поля с тегом envconfig:\""+row.env+"\"")
			continue
		}
		want := pv.want
		if want == "" {
			want = v
		}
		if err := sameLoaded(res.loaded[row.env], want); err != nil {
			add(pv.what, err.Error())
		}
	}
	for _, pv := range row.beyond {
		v := pv.v
		if pv.gen != nil {
			v = pv.gen(t)
		}
		edits := map[string]*string{row.env: str(v)}
		for k, dv := range pv.deps {
			edits[k] = str(dv)
		}
		res := run(t, edits)
		add(pv.what, judgeRefusal(res, row.env, pv.bound, pv.form))
		add(pv.what, judgeNoSecret(res, pv.secret))
	}
	return out
}

// judgeNoSecret — текст отказа не несёт поданного материала секрета.
func judgeNoSecret(res bootResult, secret []string) string {
	for _, s := range secret {
		if s != "" && strings.Contains(res.text(), s) {
			return "текст отказа несёт материал секрета: " + res.text()
		}
	}
	return ""
}

// judgeWindowWithinRetention — зависимая граница окна долей (Р12, Д26·6).
func judgeWindowWithinRetention(t *testing.T, run starter) []string {
	t.Helper()
	var out []string
	res := run(t, map[string]*string{envSentLogRetention: str("168h"), envReputationWindow: str("720h")})
	if msg := judgeRefusal(res, envReputationWindow, []string{envSentLogRetention}, false); msg != "" {
		out = append(out, "окно 30 сут при сроке журнала 7 сут: "+msg)
	}
	twin := run(t, map[string]*string{envSentLogRetention: str("168h"), envReputationWindow: str("168h")})
	if !twin.started() {
		out = append(out, "близнец (окно = срок журнала = 7 сут) отвергнут: "+twin.text())
	}
	return out
}

// censusKnob — ручка загрузчика с группой.
type censusKnob struct {
	env, group string
}

// loaderCensus — ручки загрузчика с группами: поле `Group` ручки [config.Knob].
// Нет поля — находка: загрузчик групп не объявляет.
func loaderCensus() ([]censusKnob, string) {
	knobs := config.Knobs()
	out := make([]censusKnob, 0, len(knobs))
	for _, k := range knobs {
		g := reflect.ValueOf(k).FieldByName("Group")
		if !g.IsValid() {
			return nil, "у ручки загрузчика (config.Knob) нет поля Group: загрузчик не объявляет ручки " +
				"группами NTF-1 / NTF-4 (Р16 «Держатель»)"
		}
		out = append(out, censusKnob{env: k.Env, group: fmt.Sprint(g.Interface())})
	}
	return out, ""
}

// judgeGroups — группа NTF-4 равна перечню S1 в обе стороны; NTF-1 не несёт
// ручек Р16; группы две; ручка не повторяется.
func judgeGroups(census []censusKnob, want []string) []string {
	if len(census) == 0 {
		return []string{"перепись ручек загрузчика пуста: о группах на пустом обходе утверждать нечего"}
	}
	var out []string
	seen := map[string]int{}
	ntf4 := map[string]bool{}
	for _, k := range census {
		seen[k.env]++
		switch k.group {
		case groupNTF4:
			ntf4[k.env] = true
		case groupNTF1:
			if slices.Contains(want, k.env) || slices.Contains(s3Envs, k.env) {
				out = append(out, "ручка Р16 "+k.env+" в группе NTF-1; её группа — NTF-4")
			}
		default:
			out = append(out, fmt.Sprintf("ручка %s в группе %q; групп две — NTF-1 и NTF-4", k.env, k.group))
		}
	}
	for env, n := range seen {
		if n > 1 {
			out = append(out, fmt.Sprintf("ручка %s объявлена %d раз: группы не пересекаются", env, n))
		}
	}
	for _, env := range want {
		if !ntf4[env] {
			out = append(out, "ручки Р16 стадии S1 "+env+" нет в группе NTF-4 загрузчика notify-sender")
		}
	}
	for env := range ntf4 {
		switch {
		case slices.Contains(s3Envs, env):
			out = append(out, "ручка стадии S3 "+env+" в группе NTF-4 загрузчика notify-sender; её читатель — notify-api (Р22)")
		case !slices.Contains(want, env):
			out = append(out, "ручка "+env+" в группе NTF-4 вне перечня Р16 стадии S1")
		}
	}
	slices.Sort(out)
	return out
}

// anyMember — любой член пакета читает окружение (пакет-загрузчик целиком).
const anyMember = "*"

// envReaders — обращения к окружению процесса. Ссылка без вызова
// (`load(os.LookupEnv)`) — тоже чтение.
var envReaders = map[string][]string{
	"os":                                     {"Getenv", "LookupEnv", "Environ", "ExpandEnv"},
	"syscall":                                {"Getenv", "Environ"},
	"github.com/kelseyhightower/envconfig":   {anyMember},
	"github.com/PRO-Robotech/corelib/config": {anyMember},
}

// loaderDirs — загрузчики процессов `services/notify`: каталог относительно
// корня службы. Исключение истекает само: загрузчик, не читающий окружение,
// — находка.
var loaderDirs = []string{"internal/config", "cmd/notify-probe/internal/config", "cmd/notify-api/internal/config"}

// envReadFindings — чтения окружения вне загрузчиков в исходниках srcs
// (путь относительно корня службы → текст) и число осмотренных файлов.
func envReadFindings(srcs map[string]string) ([]string, error) {
	var out []string
	readersIn := map[string]int{}
	fset := token.NewFileSet()
	for name, src := range srcs {
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("%s не разобран: %w", name, err)
		}
		alias := map[string]string{} // имя в файле → путь импорта
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if _, ok := envReaders[p]; !ok {
				continue
			}
			local := filepath.Base(p)
			if im.Name != nil {
				local = im.Name.Name
			}
			alias[local] = p
		}
		dir := filepath.ToSlash(filepath.Dir(name))
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			p, ok := alias[id.Name]
			if !ok {
				return true
			}
			members := envReaders[p]
			if !slices.Contains(members, anyMember) && !slices.Contains(members, sel.Sel.Name) {
				return true
			}
			if slices.Contains(loaderDirs, dir) {
				readersIn[dir]++
				return true
			}
			out = append(out, fmt.Sprintf("%s: %s.%s читает окружение вне загрузчика", fset.Position(sel.Pos()), p, sel.Sel.Name))
			return true
		})
	}
	for _, d := range loaderDirs {
		if readersIn[d] == 0 {
			out = append(out, "исключение «загрузчик "+d+"» без предмета: чтения окружения в нём нет")
		}
	}
	slices.Sort(out)
	return out, nil
}

// notifySources — непробные исходники Go службы notify (состав — у индекса git).
func notifySources(t *testing.T) map[string]string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: корень службы: %v", err)
	}
	files, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: состав services/notify не взят у индекса: %v", err)
	}
	srcs := map[string]string{}
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не прочитан: %v", p, err)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s вне корня службы: %v", p, err)
		}
		srcs[rel] = string(b)
	}
	if len(srcs) == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: непробных исходников notify ноль — о чтении окружения утверждать нечего")
	}
	return srcs
}

// ── эталон границ: синтетический испытуемый, собранный по таблице Р16 ───────
//
// Нужен двум местам: предпосылке (фикстура и строки таблицы согласны с Р16) и
// инъекциям (держатель краснеет на испорченном эталоне). Настоящий загрузчик
// он не заменяет и в судьи дерева не входит.

type modelDefect int

const (
	defectNone                   modelDefect = iota
	defectDefaultHardTTL                     // у ручки срока HARD_BOUNCE умолчание 72h
	defectNoUpperHardTTL                     // верхняя граница срока HARD_BOUNCE снята
	defectNoWindowRetention                  // снята граница окно ≤ срок журнала
	defectNoAddressKeyLowerBound             // нижняя граница длины ключа отпечатка снята до 1 октета
	defectAddressKeyLeaked                   // текст отказа ключа отпечатка несёт содержимое файла
)

var (
	localPartRe = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*/=?^_` + "`" + `{|}~-]+(\.[A-Za-z0-9!#$%&'*/=?^_` + "`" + `{|}~-]+)*$`)
	labelRe     = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
)

// s1Model — старт эталона на фикстуре с правками.
func s1Model(defect modelDefect) starter {
	return func(t *testing.T, edits map[string]*string) bootResult {
		t.Helper()
		env := map[string]string{}
		for _, p := range readFixture(t) {
			env[p[0]] = p[1]
		}
		if _, ok := edits[envAddressKeyDir]; !ok {
			env[envAddressKeyDir] = addressKeyDir(t)
		}
		for k, v := range edits {
			if v == nil {
				delete(env, k)
			} else {
				env[k] = *v
			}
		}
		if defect == defectDefaultHardTTL {
			if _, ok := env[envHardTTL]; !ok {
				env[envHardTTL] = "72h"
			}
		}
		res := bootResult{refused: []bootFinding{}}
		refuse := func(e, why string) { res.refused = append(res.refused, bootFinding{env: e, line: e + ": " + why}) }
		d := map[string]time.Duration{}
		ok := map[string]bool{}
		durOf := func(e string, lo, hi time.Duration) {
			v, err := time.ParseDuration(env[e])
			if err != nil {
				refuse(e, "не разбирается")
				return
			}
			if e == envHardTTL && defect == defectNoUpperHardTTL {
				hi = time.Duration(1 << 62)
			}
			if v < lo || v > hi {
				refuse(e, fmt.Sprintf("значение %s вне границы [%s..%s]", v, lo, hi))
				return
			}
			d[e], ok[e] = v, true
		}
		intOf := func(e string, lo, hi int64) {
			v, err := strconv.ParseInt(env[e], 10, 64)
			if err != nil || v < lo || v > hi {
				refuse(e, fmt.Sprintf("вне границы [%d..%d]", lo, hi))
			}
		}
		floatOf := func(e string, lo, hi string) {
			v, err := strconv.ParseFloat(env[e], 64)
			l, _ := strconv.ParseFloat(lo, 64)
			h, _ := strconv.ParseFloat(hi, 64)
			if err != nil || v < l || v > h {
				refuse(e, "вне границы ["+lo+".."+hi+"]")
			}
		}
		for _, e := range s1Envs {
			v, set := env[e]
			switch {
			case !set:
				refuse(e, "ручка не задана; умолчания у неё нет")
				continue
			case v == "":
				refuse(e, "значение пусто")
				continue
			}
			switch e {
			case envMailboxAddr:
				i := strings.LastIndex(v, ":")
				if i <= 0 {
					refuse(e, "не в форме узел:порт")
					continue
				}
				if p, err := strconv.Atoi(v[i+1:]); err != nil || p < 1 || p > 65535 {
					refuse(e, "порт вне границы [1..65535]")
				}
			case envMailboxLocalPart:
				if len(v) > 15 || !localPartRe.MatchString(v) || strings.Contains(v, "+") {
					refuse(e, "не dot-atom без + длиной 1..15")
				}
			case envReturnDomain:
				labels := strings.Split(v, ".")
				good := len(labels) >= 2
				for _, l := range labels {
					good = good && labelRe.MatchString(l)
				}
				if !good {
					refuse(e, "не имя DNS из двух меток")
				}
			case envMaxMessageBytes:
				intOf(e, 64<<10, 25<<20)
			case envMaxExpansionRatio:
				intOf(e, 1, 100)
			case envSoftThreshold:
				intOf(e, 2, 20)
			case envHardBounceRateMax:
				floatOf(e, "0.001", "0.2")
			case envComplaintRateMax:
				floatOf(e, "0.0001", "0.05")
			case envAddressKeyDir:
				if !filepath.IsAbs(v) {
					refuse(e, "путь не абсолютный")
					continue
				}
				b, err := os.ReadFile(filepath.Join(v, addressKeyFileName))
				if err != nil {
					refuse(e, "файл ключа не читается")
					continue
				}
				minBytes := config.AddressKeyMinBytes
				if defect == defectNoAddressKeyLowerBound {
					minBytes = 1
				}
				if k, err := hex.DecodeString(strings.TrimSpace(string(b))); err != nil || len(k) < minBytes {
					why := fmt.Sprintf("ключ не разбирается: не короче %d октетов", config.AddressKeyMinBytes)
					if defect == defectAddressKeyLeaked {
						why += fmt.Sprintf(" (прочитано %q)", b)
					}
					refuse(e, why)
				}
			case envPollInterval:
				durOf(e, 5*time.Second, 10*time.Minute)
			case envCanaryInterval, envSweepInterval:
				durOf(e, time.Minute, 24*time.Hour)
			case envSentLogRetention:
				durOf(e, 168*time.Hour, 2160*time.Hour)
			case envHardTTL:
				durOf(e, time.Hour, 72*time.Hour)
			case envSoftWindow, envSoftTTL:
				durOf(e, time.Hour, 168*time.Hour)
			case envSecretReload:
				durOf(e, 5*time.Second, 10*time.Minute)
			case envReputationWindow:
				durOf(e, time.Hour, 720*time.Hour)
			}
		}
		// Зависимые границы судятся только на годных слагаемых.
		if v, err := time.ParseDuration(env[envPollStaleMax]); err == nil && env[envPollStaleMax] != "" {
			if ok[envPollInterval] {
				lo := max(4*d[envPollInterval], d[envPollInterval]+time.Minute)
				if v < lo || v > 24*time.Hour {
					refuse(envPollStaleMax, fmt.Sprintf("значение %s вне границы [%s..24h0m0s]", v, lo))
				} else {
					d[envPollStaleMax], ok[envPollStaleMax] = v, true
				}
			}
		} else if _, set := env[envPollStaleMax]; set && env[envPollStaleMax] != "" {
			refuse(envPollStaleMax, "не разбирается")
		}
		if v, err := time.ParseDuration(env[envCanaryDeadline]); err == nil && env[envCanaryDeadline] != "" {
			if ok[envCanaryInterval] && (v < 30*time.Second || v >= d[envCanaryInterval]) {
				refuse(envCanaryDeadline, fmt.Sprintf("значение %s вне границы [30s..%s) (%s)", v, d[envCanaryInterval], envCanaryInterval))
			}
		}
		if v, err := time.ParseDuration(env[envLeaseTTL]); err == nil && env[envLeaseTTL] != "" {
			if ok[envPollInterval] && ok[envPollStaleMax] {
				lo := max(30*time.Second, 2*d[envPollInterval])
				hi := d[envPollStaleMax] - d[envPollInterval]
				if v < lo || v >= hi {
					refuse(envLeaseTTL, fmt.Sprintf("значение %s вне границы [%s..%s) (%s − %s)", v, lo, hi, envPollStaleMax, envPollInterval))
				}
			}
		}
		if defect != defectNoWindowRetention && ok[envReputationWindow] && ok[envSentLogRetention] &&
			d[envReputationWindow] > d[envSentLogRetention] {
			refuse(envReputationWindow, fmt.Sprintf("окно %s больше срока журнала %s %s: граница %s ≤ %s",
				d[envReputationWindow], envSentLogRetention, d[envSentLogRetention], envReputationWindow, envSentLogRetention))
		}
		if len(res.refused) > 0 {
			return res
		}
		res.refused = nil
		res.loaded = map[string]reflect.Value{}
		for _, e := range s1Envs {
			if dv, isDur := d[e]; isDur {
				res.loaded[e] = reflect.ValueOf(dv)
			} else {
				res.loaded[e] = reflect.ValueOf(env[e])
			}
		}
		// Строковое значение сравнивается как строка: эталон не типизирует числа.
		for e, v := range res.loaded {
			if v.Kind() == reflect.String {
				res.loaded[e] = reflect.ValueOf(env[e])
			}
		}
		return res
	}
}

// ── предпосылка ────────────────────────────────────────────────────────────

// requireS1Premise — таблица пробы и фикстура годны. Провал — «НЕ ВЫПОЛНИЛОСЬ»:
// предмет пробы здесь ещё не спрошен.
func requireS1Premise(t *testing.T) []knobRow {
	t.Helper()
	rows := s1Rows()
	if len(s1Envs) != s1Count || len(rows) != s1Count {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: перечень пробы %d, строк таблицы %d, а ручек S1 по Р16 %d", len(s1Envs), len(rows), s1Count)
	}
	seen := map[string]bool{}
	for i, r := range rows {
		if r.env != s1Envs[i] || seen[r.env] {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: строка %d таблицы (%s) расходится с перечнем или повторяется", i, r.env)
		}
		seen[r.env] = true
		if len(r.edges) == 0 || len(r.beyond) == 0 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: у строки %s нет значения на границе либо за ней", r.env)
		}
	}
	fixture := map[string]bool{}
	for _, p := range readFixture(t) {
		fixture[p[0]] = true
	}
	for _, env := range s1Envs {
		if env != envAddressKeyDir && !fixture[env] {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: фикстура %s не несёт ручку %s", fixturePath, env)
		}
	}
	// Эталон принимает фикстуру и отвечает на каждую строку так, как она ждёт:
	// иначе значение строки само противоречит Р16, и красное было бы её.
	model := s1Model(defectNone)
	var bad []string
	t.Run("предпосылка/эталон", func(t *testing.T) {
		if res := model(t, nil); !res.started() {
			bad = append(bad, "фикстура вне границ Р16: "+res.text())
		}
		for _, r := range rows {
			bad = append(bad, judgeKnob(t, r, model)...)
		}
		bad = append(bad, judgeWindowWithinRetention(t, model)...)
	})
	if len(bad) > 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: таблица пробы противоречит Р16:\n  %s", strings.Join(bad, "\n  "))
	}
	t.Run("предпосылка/фикстура NTF-1 стартует", func(t *testing.T) {
		useFixture(t, nil)
		if err := start(t); err != nil {
			bad = append(bad, err.Error())
		}
	})
	if len(bad) > 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: фикстура проб старта не проходит настоящего стража: %s", bad[0])
	}
	return rows
}

// ── держатель ──────────────────────────────────────────────────────────────

// TestRequiredKnobsS1 — держатель Р16 стадии S1 (DoD S1 п.5).
func TestRequiredKnobsS1(t *testing.T) {
	rows := requireS1Premise(t)

	t.Run("группы", func(t *testing.T) {
		census, missing := loaderCensus()
		if missing != "" {
			t.Fatal(missing)
		}
		t.Logf("перепись ручек загрузчика: %d", len(census))
		for _, f := range judgeGroups(census, s1Envs) {
			t.Error(f)
		}
	})

	for _, r := range rows {
		t.Run("ручка/"+r.env, func(t *testing.T) {
			for _, f := range judgeKnob(t, r, realStart) {
				t.Error(f)
			}
		})
	}

	t.Run("окно долей не больше срока журнала", func(t *testing.T) {
		for _, f := range judgeWindowWithinRetention(t, realStart) {
			t.Error(f)
		}
	})

	t.Run("окружение читает только загрузчик", func(t *testing.T) {
		srcs := notifySources(t)
		found, err := envReadFindings(srcs)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %v", err)
		}
		t.Logf("перепись: непробных исходников notify %d, загрузчиков %d", len(srcs), len(loaderDirs))
		for _, f := range found {
			t.Error(f)
		}
	})
}

// NTF4-87 — срок HARD_BOUNCE за верхней границей: отказ старта до подъёма
// чего бы то ни было; текст называет ручку и границу 72h. Близнец — NTF4-88.
func TestNTF4_87_HardTTLBeyondUpperBoundRefusesStart(t *testing.T) {
	requireS1Premise(t)
	res := realStart(t, map[string]*string{envHardTTL: str("73h")})
	if msg := judgeRefusal(res, envHardTTL, durBound(72*time.Hour), false); msg != "" {
		t.Fatal(msg)
	}
}

// NTF4-88 — срок HARD_BOUNCE ровно на верхней границе: старт, загружено 72h.
// Половина сценария о записи `HARD_BOUNCE` с `expiresAt` = событие + 72 ч —
// integration-проба пути NTF4-01 на П6 + П10, не этой полосы конфигурации.
func TestNTF4_88_HardTTLOnUpperBoundStarts(t *testing.T) {
	requireS1Premise(t)
	res := realStart(t, map[string]*string{envHardTTL: str("72h")})
	if !res.started() {
		t.Fatalf("старт отвергнут: %s", res.text())
	}
	if slices.Contains(res.missing, envHardTTL) {
		t.Fatalf("старт принят, но загрузчик не читает %s: в перечне Config нет поля с тегом envconfig", envHardTTL)
	}
	if err := sameLoaded(res.loaded[envHardTTL], "72h"); err != nil {
		t.Fatal(err)
	}
}
