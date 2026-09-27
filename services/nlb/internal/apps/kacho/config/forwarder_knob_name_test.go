// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// forwarder_knob_name_test.go — имя ручки, которое страж круга отправителей
// печатает оператору, ЧИТАЕТСЯ разбором конфигурации (kacho#2739).
//
// Текст отказа — часть контракта с оператором: он называет действие, которым
// отказ снимается. Здесь стояло имя с подчёркиваниями
// (`KACHO_NLB_AUTHZ__TRUSTED_FORWARDER_SANS`), а разбор читает форму с
// дефисами: viper заменяет на `__` только точку, дефис в ключе
// `trusted-forwarder-sans` доезжает до имени переменной как есть. Оператор,
// скопировавший имя из отказа, получал то же пустое значение, из-за которого
// страж и отказал.
//
// Проба берёт имя ИЗ ТЕКСТА ОТКАЗА, а не из своей памяти: переименование ручки
// или правка текста, разведённые между собой, краснят её, а вычитка — нет.
// Обе формы прогоняются: форма из текста обязана доставить величину и снять
// отказ, вторая — не доставить ничего и оставить тот же отказ (она не
// поддержана, и это здесь утверждается, а не подразумевается).

import (
	"regexp"
	"strings"
	"testing"
)

const probeForwarderSAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"

// forwarderProbeBase — посадка, на которой из всех отказов старта остаётся
// ровно отказ пустого круга: всё прочее, что страж требует, названо.
func forwarderProbeBase(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"KACHO_NLB_MODE":                       "dev",
		"KACHO_NLB_REPOSITORY__POSTGRES__URL":  "postgres://u:p@pg-nlb:5432/kacho_nlb?sslmode=require",
		"KACHO_NLB_EXTAPI__IAM__INTERNAL-ADDR": "kaname-internal:9091",
		"KACHO_NLB_EXTAPI__IAM__ADDR":          "kaname:9090",
		"KACHO_NLB_AUTHZ__TRUST_DOMAIN":        "kacho.cloud",
		"KACHO_NLB_QUOTA__AUTHORITY":           "not-deployed",
		// Опт-ин «круг не сужаем» снимается ЯВНО: TestMain пакета выставляет его
		// на весь прогон, и без этой строки страж пустого круга молчал бы —
		// предпосылка пробы не была бы создана.
		"KACHO_NLB_AUTHZ__TRUST_ANY_FORWARDER": "false",
	} {
		t.Setenv(k, v)
	}
}

// knobInRefusal — имя переменной, которое отказ называет у ключа key.
func knobInRefusal(t *testing.T, refusal, key string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(key) + ` \(env (KACHO_[A-Za-z0-9_-]+)\)`)
	m := re.FindStringSubmatch(refusal)
	if m == nil {
		t.Fatalf("отказ не называет ручку ключа %s формой «%s (env ИМЯ)» — оператору нечего выставлять:\n%s",
			key, key, refusal)
	}
	return m[1]
}

// otherForm — то же имя с обратной заменой разделителя внутри сегментов:
// дефис ↔ подчёркивание после приставки службы. Двойное подчёркивание —
// разделитель уровней ключа — не трогается.
func otherForm(name string) string {
	const prefix = "KACHO_NLB_"
	levels := strings.Split(strings.TrimPrefix(name, prefix), "__")
	for i, l := range levels {
		switch {
		case strings.Contains(l, "-"):
			levels[i] = strings.ReplaceAll(l, "-", "_")
		default:
			levels[i] = strings.ReplaceAll(l, "_", "-")
		}
	}
	return prefix + strings.Join(levels, "__")
}

func TestForwarderRefusalNamesAFormTheParserReads(t *testing.T) {
	const key = "authz.trusted-forwarder-sans"

	forwarderProbeBase(t)
	_, err := Load("")
	if err == nil || !strings.Contains(err.Error(), key) {
		t.Fatalf("предпосылка не создана: на пустом круге страж обязан отказать, называя %s; отказ: %v", key, err)
	}
	named := knobInRefusal(t, err.Error(), key)

	// Форма ИЗ ТЕКСТА: величина доезжает, и отказ пустого круга снят.
	t.Run("форма из текста отказа", func(t *testing.T) {
		forwarderProbeBase(t)
		t.Setenv(named, probeForwarderSAN)
		cfg, err := parse("")
		if err != nil {
			t.Fatalf("разбор отказал: %v", err)
		}
		if got := cfg.Authz.TrustedForwarderSANs; len(got) != 1 || got[0] != probeForwarderSAN {
			t.Fatalf("имя %s, названное отказом, НЕ ДОСТАВИЛО величину (круг = %q): оператор, "+
				"выставивший его по тексту, получит тот же отказ", named, got)
		}
		if _, err := Load(""); err != nil && strings.Contains(err.Error(), key) {
			t.Fatalf("по имени из текста отказа отказ пустого круга не снят:\n%v", err)
		}
	})

	// ВТОРАЯ форма не поддержана — и это утверждается: она не доставляет ничего и
	// оставляет тот же отказ. Иначе «какая форма читается» оставалось бы
	// неизвестным, и следующий текст мог бы назвать любую из двух.
	t.Run("вторая форма", func(t *testing.T) {
		other := otherForm(named)
		if other == named {
			t.Fatalf("вторая форма совпала с первой (%s) — сравнивать нечего", named)
		}
		forwarderProbeBase(t)
		t.Setenv(other, probeForwarderSAN)
		cfg, err := parse("")
		if err != nil {
			t.Fatalf("разбор отказал: %v", err)
		}
		if got := cfg.Authz.TrustedForwarderSANs; len(got) != 0 {
			t.Fatalf("вторая форма %s тоже доставляет величину (%q) — значит она поддержана, и это "+
				"обязано быть объявлено, а не выяснено пробой", other, got)
		}
		if _, err := Load(""); err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("на второй форме %s отказ пустого круга пропал: %v", other, err)
		}
	})
}
