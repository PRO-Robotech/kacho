// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/authz"
)

// configEnv — годный набор ручек notify-api: значения в границах.
func configEnv() map[string]string {
	return map[string]string{
		"KACHO_NOTIFY_AUTH_MODE":                          "production",
		"KACHO_NOTIFY_PEER_TLS_CERT_FILE":                 "/etc/notify/peer/tls.crt",
		"KACHO_NOTIFY_PEER_TLS_KEY_FILE":                  "/etc/notify/peer/tls.key",
		"KACHO_NOTIFY_PEER_TLS_CA_FILE":                   "/etc/notify/peer/ca.crt",
		"KACHO_NOTIFY_DB_HOST":                            "pg",
		"KACHO_NOTIFY_DB_PORT":                            "5432",
		"KACHO_NOTIFY_DB_USER":                            "notify",
		"KACHO_NOTIFY_DB_PASSWORD":                        "secret",
		"KACHO_NOTIFY_DB_NAME":                            "kacho_notify",
		"KACHO_NOTIFY_DB_SSLMODE":                         "require",
		"KACHO_NOTIFY_DB_MAX_CONNS":                       "10",
		"KACHO_NOTIFY_DIAG_ADDR":                          ":9095",
		"KACHO_NOTIFY_AUTHZ_IAM_GRPC_ADDR":                "kaname:9091",
		"KACHO_NOTIFY_INTERNAL_PORT":                      "9091",
		"KACHO_NOTIFY_INTERNAL_SERVER_MTLS_CERTFILE":      "/etc/notify/server/tls.crt",
		"KACHO_NOTIFY_INTERNAL_SERVER_MTLS_KEYFILE":       "/etc/notify/server/tls.key",
		"KACHO_NOTIFY_INTERNAL_SERVER_MTLS_CLIENTCAFILES": "/etc/notify/server/ca.crt",
		"KACHO_NOTIFY_AUTHZ_TRUST_DOMAIN":                 "kacho.cloud",
		"KACHO_NOTIFY_AUTHZ_TRUSTED_FORWARDER_SANS":       "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway",
		"KACHO_NOTIFY_AUTHZ_TRUST_ANY_FORWARDER":          "false",
		"KACHO_NOTIFY_AUTHZ_CACHE_TTL":                    "5s",
		"KACHO_NOTIFY_AUTHZ_CHECK_TIMEOUT":                "2s",
		"KACHO_NOTIFY_AUTHZ_DENY_BUDGET_PER_SEC":          "100",
		"KACHO_NOTIFY_HANDLING_BUDGET":                    "30s",
		"KACHO_NOTIFY_NOTICE_REMINDER_LEAD":               "24h",
		"KACHO_NOTIFY_LIST_FILTER_CACHE_TTL":              "5s",
	}
}

// setConfigEnv выставляет окружение пробы и снимает прочие KACHO_NOTIFY_*.
func setConfigEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "KACHO_NOTIFY_") {
			if _, keep := env[k]; !keep {
				t.Setenv(k, "")
				if err := os.Unsetenv(k); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func loadConfig(t *testing.T, env map[string]string) (Config, error) {
	t.Helper()
	setConfigEnv(t, env)
	c, err := Load()
	if err != nil {
		return c, err
	}
	return c, c.Validate()
}

// refusedKnobs — ручки, названные отказом старта.
func refusedKnobs(err error) []string {
	var re *RefusalError
	if !errors.As(err, &re) {
		return nil
	}
	var out []string
	for _, f := range re.Findings {
		out = append(out, f.Knob.Env)
	}
	return out
}

// TestConfig_EveryKnobHasAReaderAndTheTwinStarts — годный набор принимается,
// и перепись ручек совпадает с набором пробы: ручки, которую никто не задаёт,
// нет, и задаваемой, которую загрузчик не читает, нет.
func TestConfig_EveryKnobHasAReaderAndTheTwinStarts(t *testing.T) {
	c, err := loadConfig(t, configEnv())
	if err != nil {
		t.Fatalf("годный набор отвергнут: %v", err)
	}
	knobs := Knobs()
	t.Logf("ручек notify-api: %d", len(knobs))
	if len(knobs) != len(configEnv()) {
		t.Fatalf("ручек в переписи %d, в наборе пробы %d", len(knobs), len(configEnv()))
	}
	for _, k := range knobs {
		if _, ok := configEnv()[k.Env]; !ok {
			t.Fatalf("ручка %s вне набора пробы", k)
		}
	}
	if c.ListFilter().CacheTTL != 5*time.Second || c.NoticeReminderLead != 24*time.Hour {
		t.Fatalf("величины не дошли: %+v", c.ListFilter())
	}
}

// TestConfig_EachRefusalNamesItsKnob — каждая инъекция меняет один факт
// годного набора и даёт отказ старта ровно с именем своей ручки.
func TestConfig_EachRefusalNamesItsKnob(t *testing.T) {
	ceiling := authz.RevocationPolicy.Ceiling
	cases := []struct {
		name, env, value string
		unset            bool
	}{
		{"не задано напоминание — умолчания у него нет", "KACHO_NOTIFY_NOTICE_REMINDER_LEAD", "", true},
		{"окно сужателя ноль", "KACHO_NOTIFY_LIST_FILTER_CACHE_TTL", "0s", false},
		{"окно сужателя выше потолка политики", "KACHO_NOTIFY_LIST_FILTER_CACHE_TTL", (ceiling + time.Second).String(), false},
		{"окно сужателя ниже секунды", "KACHO_NOTIFY_LIST_FILTER_CACHE_TTL", "500ms", false},
		{"напоминание короче часа", "KACHO_NOTIFY_NOTICE_REMINDER_LEAD", "30m", false},
		{"напоминание длиннее недели", "KACHO_NOTIFY_NOTICE_REMINDER_LEAD", "169h", false},
		{"не боевая посадка", "KACHO_NOTIFY_AUTH_MODE", "dev", false},
		{"граница обработки не больше срока вопроса", "KACHO_NOTIFY_HANDLING_BUDGET", "2s", false},
		{"порт слушателя равен диагностическому", "KACHO_NOTIFY_INTERNAL_PORT", "9095", false},
		{"пересылающий вне домена доверия", "KACHO_NOTIFY_AUTHZ_TRUSTED_FORWARDER_SANS", "spiffe://other.cloud/ns/kacho/sa/gw", false},
		{"окно звена прав выше потолка политики", "KACHO_NOTIFY_AUTHZ_CACHE_TTL", (ceiling + time.Second).String(), false},
		{"бюджет отказов ноль", "KACHO_NOTIFY_AUTHZ_DENY_BUDGET_PER_SEC", "0", false},
		{"относительный путь УЦ клиентов", "KACHO_NOTIFY_INTERNAL_SERVER_MTLS_CLIENTCAFILES", "ca.crt", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := configEnv()
			if c.unset {
				delete(env, c.env)
			} else {
				env[c.env] = c.value
			}
			_, err := loadConfig(t, env)
			got := refusedKnobs(err)
			if len(got) != 1 || got[0] != c.env {
				t.Fatalf("отказ %v (ручки %v), ожидался ровно по %s", err, got, c.env)
			}
		})
	}
}

// listFilterWindowKey — запись окна сужателя в политике окон отзыва платформы
// (corelib/authz.RevocationPolicy.Windows); ключ в форме переписи гейта
// «<процесс> <ручка>».
const listFilterWindowKey = "notify KACHO_NOTIFY_LIST_FILTER_CACHE_TTL"

// TestConfig_UnsetWindowKnobStartsWithThePolicyWindow — NTF5-53 (б): ручка окна
// сужателя не задана — старт с умолчанием загрузчика, и оно равно записи
// политики окон отзыва, а не литералу пробы. Близнец — та же ручка, заданная
// значением в границе, отличным от умолчания: доходит без подмены.
func TestConfig_UnsetWindowKnobStartsWithThePolicyWindow(t *testing.T) {
	want, ok := authz.RevocationPolicy.Windows[listFilterWindowKey]
	if !ok {
		t.Fatalf("записи %q в политике окон отзыва нет — сверять умолчание не с чем", listFilterWindowKey)
	}
	env := configEnv()
	delete(env, "KACHO_NOTIFY_LIST_FILTER_CACHE_TTL")
	c, err := loadConfig(t, env)
	if err != nil {
		t.Fatalf("ручка окна не задана — старт отвергнут: %v", err)
	}
	if got := c.ListFilter().CacheTTL; got != want {
		t.Fatalf("окно сужателя без ручки %s, политика объявляет %s", got, want)
	}

	twin := configEnv()
	twin["KACHO_NOTIFY_LIST_FILTER_CACHE_TTL"] = "2s"
	c, err = loadConfig(t, twin)
	if err != nil {
		t.Fatalf("близнец отвергнут: %v", err)
	}
	if got := c.ListFilter().CacheTTL; got != 2*time.Second {
		t.Fatalf("заданное окно 2s не дошло: %s", got)
	}
}

// authzWindowKnob — ручка окна звена прав слушателя (приёмка NTF-4 Р16).
const authzWindowKnob = "KACHO_NOTIFY_AUTHZ_CACHE_TTL"

// refusalWhy — причина отказа по ручке env ("" — по ней отказа нет).
func refusalWhy(err error, env string) string {
	var re *RefusalError
	if !errors.As(err, &re) {
		return ""
	}
	for _, f := range re.Findings {
		if f.Knob.Env == env {
			return f.Why
		}
	}
	return ""
}

// TestConfig_AuthzWindowCeilingIsThePolicyCeiling — NTF4-123/124: верх границы
// окна звена прав — потолок окна отзыва платформы, а не своё число notify. Окно
// на единицу разрешения (1 с) выше потолка — отказ с именем ручки и границей
// потолка; близнец ровно на потолке — старт. Значения отличаются одним фактом.
func TestConfig_AuthzWindowCeilingIsThePolicyCeiling(t *testing.T) {
	ceiling := authz.RevocationPolicy.Ceiling

	env := configEnv()
	env[authzWindowKnob] = (ceiling + time.Second).String()
	_, err := loadConfig(t, env)
	if got := refusedKnobs(err); len(got) != 1 || got[0] != authzWindowKnob {
		t.Fatalf("окно %s выше потолка %s: отказ %v (ручки %v), ожидался ровно по %s",
			env[authzWindowKnob], ceiling, err, got, authzWindowKnob)
	}
	if why := refusalWhy(err, authzWindowKnob); !strings.Contains(why, ".."+ceiling.String()+"]") {
		t.Fatalf("текст отказа %q не называет границу потолка %s", why, ceiling)
	}

	twin := configEnv()
	twin[authzWindowKnob] = ceiling.String()
	c, err := loadConfig(t, twin)
	if err != nil {
		t.Fatalf("окно ровно на потолке %s отвергнуто: %v", ceiling, err)
	}
	if c.AuthzCacheTTL != ceiling {
		t.Fatalf("окно %s не дошло: %s", ceiling, c.AuthzCacheTTL)
	}
}

// TestConfig_AuthzWindowCeilingFollowsAPolicySubstitution — подмена потолка
// (держатель Р16): при потолке 7 с значение 8 с — отказ с границей `7s`, 7 с —
// загрузка. При настоящем потолке 10 с граница из политики и литерал 10 с дают
// одинаковый исход; различает их только подмена. Подмена меняет общее
// состояние пакета corelib, поэтому проба не параллельна и возвращает политику.
func TestConfig_AuthzWindowCeilingFollowsAPolicySubstitution(t *testing.T) {
	const substituted = 7 * time.Second
	orig := authz.RevocationPolicy.Ceiling
	if orig == substituted {
		t.Fatalf("предпосылка пробы: потолок политики уже %s — подмена неотличима от исходного", orig)
	}
	authz.RevocationPolicy.Ceiling = substituted
	t.Cleanup(func() { authz.RevocationPolicy.Ceiling = orig })

	env := configEnv()
	env[authzWindowKnob] = "8s"
	_, err := loadConfig(t, env)
	if got := refusedKnobs(err); len(got) != 1 || got[0] != authzWindowKnob {
		t.Fatalf("при потолке %s окно 8s: отказ %v (ручки %v), ожидался ровно по %s",
			substituted, err, got, authzWindowKnob)
	}
	if why := refusalWhy(err, authzWindowKnob); !strings.Contains(why, "..7s]") {
		t.Fatalf("текст отказа %q не называет подменённую границу 7s", why)
	}

	twin := configEnv()
	twin[authzWindowKnob] = "7s"
	if _, err := loadConfig(t, twin); err != nil {
		t.Fatalf("при потолке %s окно 7s отвергнуто: %v", substituted, err)
	}
}
