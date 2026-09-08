// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_chart_default_premise_test.go — умолчание стороннего чарта личности
// всё ещё безопасно.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ И ПОЧЕМУ ОН ОТДЕЛЁН ТЕГОМ СБОРКИ
//
// Соседний identity_dev_flag_declaration_test.go не требует от боевого профиля
// выписывать `false` у ручки режима разработки — потому что умолчание чарта уже
// `false`. Всё это утверждение держится ровно на одном факте о ЧУЖОМ дереве, и
// проверяется он здесь: обе координаты обязаны существовать в архиве своего
// чарта и обязаны иметь там безопасное умолчание. Перевернётся умолчание —
// красной станет эта проверка, а не тишина в соседнем файле.
//
// ЗДЕСЬ СТОЯЛ ТЕГ `helmcharts`, И ЕГО ПРЕДМЕТ ИСЧЕЗ — сказано полностью, потому
// что довод был записан подробно и пережил свой факт.
//
// Прежняя редакция: архивы чартов (`charts/*.tgz`) НЕ отслеживаются git, их
// подкачивает `helm dependency build`, значит условие этой проверки создаёт не
// всякое задание. Довод был верен и держал раскол.
//
// Задача #2256 вендорила ВНЕШНИЕ архивы точным пином — сеть лежала на пути к
// вердикту, — и `deploy/.gitignore` теперь их отслеживает поимённо. Условие
// проверки создаёт обычный клон: `postgresql-13.4.4.tgz` и его четыре соседа
// лежат в дереве до всякой материализации. Раскол потерял основание, и держать
// его значило бы объяснять исключение фактом, которого нет.
//
// Проверка вернулась в общий прогон. Строгость НЕ снята: отсутствие архива
// остаётся отказом (см. chartArchiveValues ниже), а не пропуском, — изменилось
// только то, что отказ наступает всюду, а не в одном задании.
//
// ─────────────────────────────────────────────────────────────────────────────
// ТЕГ ПРИ ЭТОМ ЖИВ — У НЕГО ОСТАЛСЯ ДРУГОЙ ПРЕДМЕТ, И ЕГО ЛЕГКО СПУТАТЬ С ЭТИМ
//
// Под `helmcharts` остались проверки, которые РЕНДЕРЯТ умбреллу
// (identity_file_keys_survive_the_environment_test.go и его инъекция). Их
// условие вендорингом НЕ создаётся: рендер требует ЛОКАЛЬНЫХ сабчартов (vpc,
// compute, kacho-nlb, api-gateway, uif, registry, storage), а они собираются из
// исходников этого же дерева и в git не вендорятся ОСОЗНАННО — вендоренная
// копия означала бы «правка есть в файле, а в стенд не попала».
//
// То есть под одним именем тега жили ДВЕ предпосылки, и умерла одна. Что у
// оставшейся есть предмет — утверждает TestUmbrellaRenderStillNeedsMaterializedDeps;
// что тег вообще зовётся — TestChartPremiseIsActuallyInvoked. Оба в
// identity_chart_premise_reachability_test.go, вне тега.
package deploy_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// chartArchiveValues достаёт values.yaml из архива чарта в charts/.
func chartArchiveValues(t *testing.T, archive string) map[string]any {
	t.Helper()
	p := filepath.Join(umbrellaDir, "charts", archive)
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("архив чарта %s не читается (%v) — предпосылка проверки исчезла, "+
			"а не дерево стало чистым. Архив внешнего чарта ВЕНДОРЕН (#2256) и обязан "+
			"лежать в дереве после обычного клона; если его нет — сломан вендоринг "+
			"(см. deploy/.gitignore и assert-vendored-external-charts.py), а не проверка", p, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("архив %s не распаковывается: %v", p, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("архив %s не читается: %v", p, err)
		}
		// values.yaml корня чарта — ровно один уровень вложенности.
		parts := strings.Split(filepath.ToSlash(h.Name), "/")
		if len(parts) != 2 || parts[1] != "values.yaml" {
			continue
		}
		raw, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("values.yaml из %s не читается: %v", p, err)
		}
		var tree map[string]any
		if err := yaml.Unmarshal(raw, &tree); err != nil {
			t.Fatalf("values.yaml из %s не разбирается: %v", p, err)
		}
		return tree
	}
	t.Fatalf("в архиве %s нет values.yaml — форма архива сменилась", p)
	return nil
}

// TestIdentityDevFlags_ChartDefaultsAreStillSecure — предпосылка того, что
// «ручка не объявлена» в боевом профиле не является находкой.
func TestIdentityDevFlags_ChartDefaultsAreStillSecure(t *testing.T) {
	knobs := identityDevKnobs()
	if len(knobs) == 0 {
		t.Fatal("осмотрено: ручек 0 — проверять нечего, и «зелено» здесь означало бы " +
			"«ничего не читал», а не безопасное умолчание")
	}
	for _, k := range knobs {
		vals := chartArchiveValues(t, k.archive)
		v, ok := lookup(vals, k.path...)
		if !ok {
			t.Errorf("%s: ручки %s нет в values.yaml чарта %s — координата переехала, "+
				"и проверка боевых стеков молча перестала её читать",
				k.coord(), strings.Join(k.path, "."), k.archive)
			continue
		}
		if v != false {
			t.Errorf("%s: умолчание чарта %s стало %v — «не объявлено» больше НЕ безопасно, "+
				"и каждый боевой профиль обязан объявить false сам", k.coord(), k.archive, v)
		}
		t.Logf("предпосылка: %s умолчание чарта %s = %v", k.coord(), k.archive, v)
	}
	t.Logf("осмотрено: ручек %d, архивов чартов %d", len(knobs), len(knobs))
}
