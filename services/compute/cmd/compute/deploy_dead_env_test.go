// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kelseyhightower/envconfig"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

// Переменная окружения, которую чарт выставляет, а процесс не читает, — не
// безобидный мусор. Она документирует оператору настройку, которой нет: он
// заполняет её в values, видит её в поде, считает вопрос закрытым — и получает
// поведение по умолчанию. Хуже всего это на security-ручках: «включено» в values
// и «включено» в процессе перестают совпадать, а расхождение ничем не
// наблюдается.
//
// Гейт сверяет ДЕКЛАРАЦИИ, а не отрендеренный шаблон: читает исходник
// deployment.yaml и множество имён, которые реально консультирует загрузчик
// конфига. Поэтому он не может «не сработать» из-за отсутствия helm и не зависит
// от того, какой профиль values взят (эталон — gateway deploy/token_shape_test.go).

// envDeclRe — ОБЪЯВЛЕНИЕ переменной в шаблоне: элемент списка `env:` вида
// `- name: KACHO_COMPUTE_…`. Намеренно НЕ «любое вхождение имени в файле»:
// шаблон полон комментариев, объясняющих в том числе СНЯТЫЕ переменные, и гейт
// по сырому тексту объявил бы находкой ровно ту запись, которая сообщает, что
// находки больше нет. Читается исполняемая часть, комментарий — нет.
var envDeclRe = regexp.MustCompile(`(?m)^[^#\n]*-\s*name:\s*(KACHO_COMPUTE_[A-Z0-9_]+)`)

// configEnvNames — все имена, которые загрузчик конфига реально консультирует.
// Берётся из САМОГО загрузчика (envconfig по структуре Config с тем же
// префиксом, что и в Load), а не из списка в тесте: список разъехался бы с
// кодом ровно так же, как разъехался чарт.
//
// envconfig принимает для поля ДВА имени: выведенное из иерархии (Key) и
// абсолютное из тега (Alt) — учитываются оба, иначе гейт объявил бы мёртвыми
// живые переменные.
func configEnvNames(t *testing.T) map[string]struct{} {
	t.Helper()
	var buf bytes.Buffer
	err := envconfig.Usagef(config.EnvPrefix, &config.Config{}, &buf, "{{range .}}{{.Key}}\n{{.Alt}}\n{{end}}")
	require.NoError(t, err, "enumerating config env names")

	out := map[string]struct{}{}
	for _, line := range strings.Split(buf.String(), "\n") {
		name := strings.TrimSpace(line)
		if strings.HasPrefix(name, "KACHO_COMPUTE_") {
			out[name] = struct{}{}
		}
	}
	require.NotEmpty(t, out,
		"config exposes no KACHO_COMPUTE_* env name — the enumeration read nothing, so it would have asserted nothing")
	return out
}

// TestDeployChart_DeclaresNoEnvTheProcessNeverReads — гейт КЛАССА: ни одна
// переменная шаблона не может отсутствовать в конфиге. Новая мёртвая ручка валит
// сборку в тот же момент, когда её вписали, а не через несколько редизайнов,
// когда уже никто не помнит, читалась ли она когда-нибудь.
func TestDeployChart_DeclaresNoEnvTheProcessNeverReads(t *testing.T) {
	known := configEnvNames(t)

	declared := map[string]struct{}{}
	for _, m := range envDeclRe.FindAllStringSubmatch(readDeploymentTemplate(t), -1) {
		declared[m[1]] = struct{}{}
	}
	require.NotEmpty(t, declared,
		"the chart template declares no KACHO_COMPUTE_* env at all — the scan read nothing")

	var dead []string
	for name := range declared {
		if _, ok := known[name]; !ok {
			dead = append(dead, name)
		}
	}
	sort.Strings(dead)

	require.Emptyf(t, dead,
		"the chart renders %d env var(s) no config field reads: %v\n"+
			"Each one tells the operator a setting exists that the process ignores — remove it from "+
			"templates/deployment.yaml and from values.yaml, or give it a reader in internal/config.",
		len(dead), dead)
}

// TestDeployValues_DocumentNoKnobTheProcessNeverReads — вторая половина: values
// объясняет оператору, что настраивать. Осиротевший блок values переживает
// удаление переменной из шаблона и продолжает обещать настройку — поэтому
// проверяется отдельно, а не «заодно» (гейт выше видит только шаблон).
//
// Сверяется ОБЪЯВЛЕНИЕ ключа, а не упоминание имени: запись «resourceManagerAddr
// удалён» в комментарии — это документирование снятия, и падать на ней значило
// бы наказывать за честность.
func TestDeployValues_DocumentNoKnobTheProcessNeverReads(t *testing.T) {
	values := readValues(t)
	for _, orphan := range []string{"tupleWrite", "resourceManagerAddr"} {
		declared := regexp.MustCompile(`(?m)^[^#\n]*\b` + regexp.QuoteMeta(orphan) + `\s*:`)
		if declared.MatchString(values) {
			t.Errorf("values.yaml still declares %q, but no config field reads the env it feeds — "+
				"the operator fills it in, sees it in the pod, and gets the default", orphan)
		}
	}
}
