// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// config_test.go — страж согласия флага доставки, кольца ключей и таблицы
// звена идентичности (NTF1-N08 на уровне разбора ручек).
//
// Базовое окружение одно (baseEnv); каждое отрицание меняет РОВНО ОДИН факт
// против положительного близнеца и утверждает имя своей ручки в тексте отказа.

import (
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/corelib/authz"
)

const (
	keyringPath = "/run/secrets/notifyprobe/keyring.json"
	notifySAN   = "spiffe://kacho.cloud/ns/kacho/sa/kacho-notify"
)

// keyringFile — файл кольца формы фундамента: активный ключ, 32 байта.
func keyringFile() []byte {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	return []byte(`{"active":{"id":1,"key":"` + key + `"}}`)
}

// baseEnv — окружение, с которым проба стартует с включённой доставкой.
func baseEnv() map[string]string {
	return map[string]string{
		"KACHO_NOTIFYPROBE_DB_PASSWORD": "pw",
		FlagKnob:                        "true",
		KeyringKnob:                     keyringPath,
		NotifySANKnob:                   notifySAN,
	}
}

// loadWith — load над поданным окружением: теги читает envconfig из окружения
// процесса, поэтому оно выставляется t.Setenv; флаг, кольцо и SAN читает
// та же функция поданным lookup — ровно как в Load. Значение политики окна —
// то же, что подаёт Load (policyWindow).
func loadWith(t *testing.T, env map[string]string) (Config, error) {
	t.Helper()
	return loadWithWindow(t, env, policyWindow())
}

// loadWithWindow — loadWith с поданным значением политики окна отзыва.
func loadWithWindow(t *testing.T, env map[string]string, window time.Duration) (Config, error) {
	t.Helper()
	for _, k := range []string{"KACHO_NOTIFYPROBE_DB_PASSWORD", FlagKnob, KeyringKnob, NotifySANKnob} {
		t.Setenv(k, "")
	}
	// Окно отзыва снимается с окружения прогона: незаданное — умолчание тега.
	t.Setenv(ttlKnob, "")
	os.Unsetenv(ttlKnob)
	for k, v := range env {
		t.Setenv(k, v)
	}
	lookup := func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
	readFile := func(p string) ([]byte, error) {
		if p == keyringPath {
			return keyringFile(), nil
		}
		return nil, fs.ErrNotExist
	}
	return load(lookup, readFile, window)
}

func without(env map[string]string, key string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		if k != key {
			out[k] = v
		}
	}
	return out
}

func with(env map[string]string, key, value string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	out[key] = value
	return out
}

func requireRefusalNames(t *testing.T, err error, knob string) {
	t.Helper()
	if err == nil {
		t.Fatalf("старт принят, а обязан быть отказом с именем %s", knob)
	}
	if !strings.Contains(err.Error(), knob) {
		t.Fatalf("отказ не называет ручку %s:\n%v", knob, err)
	}
}

// NTF1-N08 — переменная флага не задана: отказ с её именем.
func TestFlagUnsetRefusesNamingTheVariable(t *testing.T) {
	_, err := loadWith(t, without(baseEnv(), FlagKnob))
	requireRefusalNames(t, err, FlagKnob)
}

// Иное написание флага — тоже отказ: false из «0» или «TRUE» неотличим от
// выключенного модуля, и разбор принимает ровно два слова.
func TestFlagForeignSpellingRefusesNamingTheVariable(t *testing.T) {
	for _, v := range []string{"", "1", "TRUE", "yes", " true"} {
		t.Run(v, func(t *testing.T) {
			_, err := loadWith(t, with(baseEnv(), FlagKnob, v))
			requireRefusalNames(t, err, FlagKnob)
		})
	}
}

// Близнец NTF1-N08: «true» и «false» принимаются, и значение доезжает.
func TestFlagTrueAndFalseAreAccepted(t *testing.T) {
	on, err := loadWith(t, baseEnv())
	if err != nil {
		t.Fatalf("true отвергнут: %v", err)
	}
	if !on.Notifications.Set() || !on.Notifications.On() {
		t.Fatalf("true разобран как %+v", on.Notifications)
	}
	if on.Keyring == nil {
		t.Fatal("при включённой доставке кольцо не собрано")
	}

	// Выключенная доставка ни кольца, ни SAN не требует: их нет в окружении.
	off, err := loadWith(t, without(without(with(baseEnv(), FlagKnob, "false"), KeyringKnob), NotifySANKnob))
	if err != nil {
		t.Fatalf("false отвергнут: %v", err)
	}
	if !off.Notifications.Set() || off.Notifications.On() {
		t.Fatalf("false разобран как %+v", off.Notifications)
	}
	if off.Keyring != nil {
		t.Fatal("при выключенной доставке кольцо прочитано — ручка, которой нет, читается")
	}
}

// Включённая доставка без кольца — отказ с именем ручки кольца.
func TestEnabledWithoutKeyringRefusesNamingTheKnob(t *testing.T) {
	_, err := loadWith(t, without(baseEnv(), KeyringKnob))
	requireRefusalNames(t, err, KeyringKnob)
}

// Включённая доставка с нечитаемым кольцом — отказ с именем ручки, без
// содержимого файла.
func TestEnabledWithUnreadableKeyringRefusesNamingTheKnob(t *testing.T) {
	_, err := loadWith(t, with(baseEnv(), KeyringKnob, "/nowhere/keyring.json"))
	requireRefusalNames(t, err, KeyringKnob)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("причина отказа потеряна: %v", err)
	}
}

// Включённая доставка без SAN notify — отказ с именем ручки SAN.
func TestEnabledWithoutNotifySANRefusesNamingTheKnob(t *testing.T) {
	_, err := loadWith(t, without(baseEnv(), NotifySANKnob))
	requireRefusalNames(t, err, NotifySANKnob)
}

// ttlKnob — ручка окна отзыва пробы. Окно читают два места с разным смыслом
// нуля (дескриптор отвергает ≤0, сужатель потока подставляет своё умолчание),
// поэтому смысл сводится на загрузке: неположительное — отказ с именем ручки.
const ttlKnob = "KACHO_NOTIFYPROBE_AUTHZ_CACHE_TTL"

// GS-C1 — неположительное окно отзыва: отказ загрузки с именем ручки.
func TestAuthZCacheTTLNonPositiveRefusesNamingTheKnob(t *testing.T) {
	for _, v := range []string{"0s", "-1s"} {
		t.Run(v, func(t *testing.T) {
			_, err := loadWith(t, with(baseEnv(), ttlKnob, v))
			requireRefusalNames(t, err, ttlKnob)
		})
	}
}

// Текст отказа окна называет значение политики платформы из её записи для
// этой ручки, а не литералом: запись обязана существовать (иначе текст
// назвал бы 0s), и значение в тексте — ровно её значение.
func TestAuthZCacheTTLRefusalNamesThePolicyValue(t *testing.T) {
	want, ok := authz.RevocationPolicy.Windows[revocationWindowKey]
	if !ok || want <= 0 {
		t.Fatalf("в corelib/authz.RevocationPolicy.Windows нет записи %q (или она неположительна: %s) — "+
			"текст отказа назвал бы значение политики, которого нет", revocationWindowKey, want)
	}
	_, err := loadWith(t, with(baseEnv(), ttlKnob, "0s"))
	if err == nil || !strings.Contains(err.Error(), "значение политики платформы — "+want.String()) {
		t.Fatalf("отказ окна не называет значение политики %s: %v", want, err)
	}
}

// Текст отказа окна подставляет значение политики, а не литерал умолчания:
// поданное значение (7s) отлично от умолчания тега и от записи политики (5s),
// поэтому литерал «5s» в тексте отказа здесь краснеет (опыт B1 круга PR #2991).
// Близнец — то же значение политики при положительном окне: загрузка принята.
func TestAuthZCacheTTLRefusalSubstitutesTheGivenPolicyValue(t *testing.T) {
	const window = 7 * time.Second
	if window == policyWindow() {
		t.Fatalf("значение пробы %s совпало со значением политики — проба не отличит литерал от подстановки", window)
	}
	_, err := loadWithWindow(t, with(baseEnv(), ttlKnob, "0s"), window)
	if err == nil || !strings.Contains(err.Error(), "значение политики платформы — "+window.String()+")") {
		t.Fatalf("отказ окна не подставил поданное значение политики %s: %v", window, err)
	}
	if _, err := loadWithWindow(t, with(baseEnv(), ttlKnob, "5s"), window); err != nil {
		t.Fatalf("близнец: положительное окно при значении политики %s отвергнуто: %v", window, err)
	}
}

// Близнец GS-C1 — положительное окно (5s — значение RevocationPolicy) принято
// и доезжает до поля.
func TestAuthZCacheTTLPositiveIsAccepted(t *testing.T) {
	c, err := loadWith(t, with(baseEnv(), ttlKnob, "5s"))
	if err != nil {
		t.Fatalf("окно 5s отвергнуто: %v", err)
	}
	if c.AuthZCacheTTL != 5*time.Second {
		t.Fatalf("окно разобрано как %s, ожидалось 5s", c.AuthZCacheTTL)
	}
}

// GS-I1 — имя и пароль базы с символами адреса доезжают до драйвера как есть:
// разбор драйвера (pgconn.ParseConfig) читает из DSN ровно поданные значения.
// Близнец — пароль без особых символов.
func TestDSNCarriesCredentialsTheDriverReadsBack(t *testing.T) {
	for _, pw := range []string{"plain-password", "p@ss/w?rd#1%2", "a b:c"} {
		c := Config{DBHost: "db", DBPort: "5432", DBUser: "u@x", DBPassword: pw,
			DBName: "kacho_notifyprobe", DBSSLMode: "require", DBMaxConns: 4}
		for name, dsn := range map[string]string{"DSN": c.DSN(), "SingleConnDSN": c.SingleConnDSN()} {
			pc, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("%s с паролем %q не разобран драйвером: %v", name, pw, err)
			}
			if pc.User != "u@x" || pc.Password != pw || pc.Database != "kacho_notifyprobe" ||
				pc.Host != "db" || pc.Port != 5432 {
				t.Fatalf("%s с паролем %q разобран как user=%q password=%q db=%q host=%q port=%d",
					name, pw, pc.User, pc.Password, pc.Database, pc.Host, pc.Port)
			}
			if got := pc.RuntimeParams["options"]; got != "-c search_path=kacho_notifyprobe,public" {
				t.Fatalf("%s: options=%q, ожидалось «-c search_path=kacho_notifyprobe,public»", name, got)
			}
		}
	}
}

// withPolicyWindow подменяет запись политики платформы для окна этой ручки на
// время пробы: шов — сама политика (corelib/authz.RevocationPolicy.Windows),
// откуда Load берёт значение через policyWindow. Подменяется ссылка на
// перепись, а не её содержимое: общая карта фундамента не меняется, и по
// завершении пробы на место возвращается та же самая карта.
func withPolicyWindow(t *testing.T, window time.Duration) {
	t.Helper()
	orig := authz.RevocationPolicy.Windows
	windows := make(map[string]time.Duration, len(orig)+1)
	for k, v := range orig {
		windows[k] = v
	}
	windows[revocationWindowKey] = window
	authz.RevocationPolicy.Windows = windows
	t.Cleanup(func() { authz.RevocationPolicy.Windows = orig })
}

// processEnv — окружение процесса для Load: доставка выключена (Load читает
// флаг из окружения процесса, и выключенная доставка не требует кольца с
// диска), окно отзыва — поданное значение.
func processEnv(t *testing.T, ttl string) {
	t.Helper()
	t.Setenv("KACHO_NOTIFYPROBE_DB_PASSWORD", "pw")
	t.Setenv(FlagKnob, "false")
	t.Setenv(KeyringKnob, "")
	t.Setenv(NotifySANKnob, "")
	t.Setenv(ttlKnob, ttl)
}

// B1 по классу: значение политики в тексте отказа судится по ВСЕМУ пути
// Load → policyWindow → запись политики, а не по поданному параметру load.
// Запись политики подменена на 7s — значение, отличное и от умолчания тега, и
// от действующей записи (5s), — поэтому литерал 5s в любом звене источника
// (Load подаёт 5*time.Second; policyWindow возвращает 5*time.Second) здесь
// краснеет. Близнец — то же значение политики при положительном окне: Load
// принимает загрузку, и окно доезжает до поля.
func TestLoadRefusalNamesTheValueOfThePolicyRecord(t *testing.T) {
	const window = 7 * time.Second
	if live := authz.RevocationPolicy.Windows[revocationWindowKey]; live == window {
		t.Fatalf("значение пробы %s совпало с действующей записью политики — проба не отличит литерал от подстановки", window)
	}
	withPolicyWindow(t, window)

	processEnv(t, "0s")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "значение политики платформы — "+window.String()+")") {
		t.Fatalf("Load не подставил значение записи политики %s в текст отказа окна: %v", window, err)
	}

	processEnv(t, "3s")
	c, err := Load()
	if err != nil {
		t.Fatalf("близнец: положительное окно при записи политики %s отвергнуто: %v", window, err)
	}
	if c.AuthZCacheTTL != 3*time.Second {
		t.Fatalf("близнец: окно разобрано как %s, ожидалось 3s", c.AuthZCacheTTL)
	}
}
