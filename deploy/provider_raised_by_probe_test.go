// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// provider_raised_by_probe_test.go — ЧЕМ проба поднимает службу личности
// поставщика, если стенд её не поднимает (#2735).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Службу личности поставщика и её издателя на стендах таблицы не поднимает ни
// одна цепочка: выключение объявлено в базе зонта (helm/umbrella/values.yaml,
// раздел «ЧУЖОЙ СТЕК ЛИЧНОСТИ НЕ ПОДНИМАЕТСЯ»). Настройки их подов лежат в
// профилях до физического снятия подчартов (#1276), и пробы формы этих подов
// судят их, поднимая службу ВНУТРИ своего рендера одним фактом.
//
// Перечень этого факта живёт в ДВУХ читателях — shell-пробы берут
// IDENTITY_STORE_UP_ARGS из deploy/tests/helm/provider-up.sh, Go-пробы — этот
// срез. Два места об одном предмете расходятся молча: проба одного языка
// поднимала бы службу без наших карт её настроек и судила бы другой под.
// Поэтому их согласие — проверка, а не договорённость: срез ниже сверяется с
// разобранным текстом библиотеки.
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// providerRaisedByProbe — службу личности поставщика и наши карты её настроек
// поднимает ПРОБА: на стендах она выключена в базе зонта (#2735).
var providerRaisedByProbe = []string{
	"kratos.enabled=true",
	"kaname.kratos.config.enabled=true",
	"kaname.kratos.identitySchema.enabled=true",
}

// identityStoreUpArgs — объявление IDENTITY_STORE_UP_ARGS в библиотеке shell:
// массив от открывающей до закрывающей скобки, возможно в несколько строк.
var identityStoreUpArgs = regexp.MustCompile(`(?ms)^IDENTITY_STORE_UP_ARGS=\((.*?)\)`)

// shellSetValues — значения `--set` из тела массива shell, в порядке записи.
func shellSetValues(body string) []string {
	fields := strings.Fields(body)
	var out []string
	for i := 0; i < len(fields); i++ {
		if fields[i] == "--set" && i+1 < len(fields) {
			out = append(out, fields[i+1])
			i++
		}
	}
	return out
}

// TestProviderRaisedByProbeAgreesWithTheShellLibrary — Go-срез и массив shell
// поднимают службу ОДНИМ И ТЕМ ЖЕ набором ручек.
func TestProviderRaisedByProbeAgreesWithTheShellLibrary(t *testing.T) {
	lib := filepath.Join("tests", "helm", "provider-up.sh")
	raw, err := os.ReadFile(lib)
	if err != nil {
		t.Fatalf("библиотека %s не читается (%v) — сверять не с чем", lib, err)
	}
	m := identityStoreUpArgs.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("в %s нет объявления IDENTITY_STORE_UP_ARGS=( … ) — перечень переехал, "+
			"и согласие двух читателей не установлено", lib)
	}
	shell := shellSetValues(m[1])
	if len(shell) == 0 {
		t.Fatalf("массив IDENTITY_STORE_UP_ARGS разобран, но `--set` в нём ноль — разбор " +
			"перестал узнавать форму, и «совпало» здесь значило бы «сравнили пустоту»")
	}
	got := append([]string{}, providerRaisedByProbe...)
	want := append([]string{}, shell...)
	sort.Strings(got)
	sort.Strings(want)
	t.Logf("перепись: ручек у Go-среза %d, у массива shell %d", len(got), len(want))
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Go-срез providerRaisedByProbe %v и IDENTITY_STORE_UP_ARGS %v из %s разошлись — "+
			"пробы двух языков поднимали бы службу разным набором ручек", got, want, lib)
	}
}

// TestShellSetValuesReadsBothForms — разбор массива узнаёт запись в одну строку
// и в несколько, и не выдумывает значения без ключа `--set`.
func TestShellSetValuesReadsBothForms(t *testing.T) {
	cases := []struct {
		body string
		want []string
	}{
		{"--set a=1 --set b=2", []string{"a=1", "b=2"}},
		{"--set a=1\n                        --set b=2", []string{"a=1", "b=2"}},
		{"--set-string a=1", nil},
		{"--set", nil},
	}
	for _, c := range cases {
		got := shellSetValues(c.body)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("shellSetValues(%q) = %v, ждали %v", c.body, got, c.want)
		}
	}
}
