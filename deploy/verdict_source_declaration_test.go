// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// verdict_source_declaration_test.go — РУЧКИ ИСТОЧНИКА ВЕРДИКТА объявлены в
// чарте, доезжают до процесса и не расходятся с умолчанием службы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПО ОБЪЯВЛЕНИЮ, А НЕ ПО РЕНДЕРУ
//
// Та же причина, что у соседних проверок этого каталога: рендер требует `helm`
// и скачанных зависимостей, а проверка, умеющая пропуститься, однажды
// пропустится ровно тогда, когда нужна. Имена секций и ключей здесь литералы —
// рендер их не меняет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО ДЕРЖИТСЯ
//
// Умолчание живёт в ДВУХ местах: в профиле чарта и в конфигурации службы. Это
// вынужденно (профиль обязан рендерить величину всегда, иначе `default` на
// булевом подменяет явное «выключено»), и потому расхождение двух мест держит
// проба, а не память. Разойдясь, они дали бы службу, поднявшуюся с посадкой,
// которой не объявлял никто.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	iamValues    = "helm/umbrella/charts/kacho-iam/values.yaml"
	iamConfigMap = "helm/umbrella/charts/kacho-iam/templates/configmap.yaml"
	iamDeploy    = "helm/umbrella/charts/kacho-iam/templates/deployment.yaml"
	// goDefaults — умолчания службы; сверяются с профилем.
	goDefaults = "../services/iam/internal/apps/kacho/config/defaults.go"
)

func readTree(t *testing.T, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(rel))
	require.NoErrorf(t, err, "координата %q не резолвится: проверка обязана падать на переезде файла, "+
		"а не тихо переставать что-либо читать", rel)
	require.NotEmpty(t, body, "файл пуст — читать нечего, и это находка, а не ноль находок")
	return string(body)
}

// Секция объявлена в профиле И рендерится в конфигурацию, которую служба
// разбирает. Ключ профиля без ключа конфигурации — ручка без читателя.
func TestVerdictSourceKnobsAreDeclaredAndRendered(t *testing.T) {
	values := readTree(t, iamValues)
	cm := readTree(t, iamConfigMap)

	require.Contains(t, values, "verdictFormTypes:", "профиль обязан объявлять перечень переключённых типов")
	require.Contains(t, values, "shadowCompare:", "профиль обязан объявлять выключатель сверки")

	require.Contains(t, cm, "verdict-form-types:", "перечень обязан доезжать до конфигурации службы")
	require.Contains(t, cm, "shadow-compare:", "выключатель обязан доезжать до конфигурации службы")
	require.Contains(t, cm, ".Values.config.authz.verdictFormTypes",
		"значение обязано браться из профиля, а не быть вшито в шаблон")
	require.Contains(t, cm, ".Values.config.authz.shadowCompare")
}

// `default` на булевом ключе ЗАПРЕЩЁН: он подменяет явное «выключено»
// умолчанием, то есть выключить сверку становится нельзя ни при каком профиле —
// ручка объявлена и неисполнима.
func TestShadowCompareIsNotRenderedThroughDefault(t *testing.T) {
	cm := readTree(t, iamConfigMap)

	line := ""
	for _, l := range strings.Split(cm, "\n") {
		if strings.Contains(l, "shadow-compare:") {
			line = l
			break
		}
	}
	require.NotEmpty(t, line, "строка ключа не найдена — предпосылка проверки исчезла")
	require.NotContains(t, line, "default",
		"`default` на булевом ключе делает значение `false` невыразимым: ручка есть, исполнить её нельзя")
}

// Умолчание профиля СОВПАДАЕТ с умолчанием службы.
//
// Разойдясь, они дали бы посадку, которой не объявлял никто: служба считает
// сверку включённой, профиль поднимает её выключенной, и рубильник роняет старт
// на ровном месте — либо, что хуже, не роняет.
func TestChartDefaultMatchesTheServiceDefault(t *testing.T) {
	values := readTree(t, iamValues)
	defaults := readTree(t, goDefaults)

	require.Regexp(t, regexp.MustCompile(`(?m)^\s*shadowCompare:\s*true\s*$`), values,
		"умолчание профиля — сверка ВКЛЮЧЕНА")
	require.Contains(t, defaults, `v.SetDefault("authz.shadow-compare", true)`,
		"умолчание службы — сверка ВКЛЮЧЕНА; два места об одном предмете обязаны совпадать")

	require.Regexp(t, regexp.MustCompile(`(?m)^\s*verdictFormTypes:\s*\[\]\s*$`), values,
		"умолчание профиля — не переключено ничего")
	require.Contains(t, defaults, `v.SetDefault("authz.verdict-form-types", []string{})`,
		"умолчание службы — не переключено ничего")
}

// ДОСТАВКА ПЕРЕКАТЫВАЕТ ПОД.
//
// Настройка, приезжающая картой, читается один раз при СТАРТЕ. Если правка
// карты не меняет шаблон пода, под не перекатывается, процесс живёт с прежним
// окружением — и рубильник «переключён» и не переключён одновременно, причём
// боевой страж молчит, потому что старта не было.
func TestConfigDeliveryRollsThePod(t *testing.T) {
	dep := readTree(t, iamDeploy)

	require.Contains(t, dep, `kacho.cloud/config-checksum:`,
		"шаблон пода обязан нести отпечаток конфигурационной карты — иначе её правка под не перекатывает")
	require.Contains(t, dep, `"/configmap.yaml"`,
		"отпечаток обязан считаться от ТОЙ карты, в которой едет рубильник")
	require.NotContains(t, dep, "envFrom:",
		"настройки через envFrom не покрыты отпечатком: их правка шаблон пода не меняет")
}
