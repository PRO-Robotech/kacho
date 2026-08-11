// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

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
// Архивы чартов (`charts/*.tgz`) НЕ отслеживаются git (`deploy/.gitignore`): их
// подкачивает `helm dependency build`. Значит условие этой проверки создаёт не
// всякое задание, а только то, что чарты материализует. Задание unit-прогона
// (`make test-unit` → `go test ./...`) их не материализует — и первый же прогон
// с этой проверкой внутри общего файла дал красный конвейер на исправном
// дереве: «архив не читается» есть честный отказ от предпосылки, но отказ
// БЕЗУСЛОВНЫЙ — там она не могла позеленеть никогда.
//
// Исход выбран тот, что предписан правилами: «здесь его не запустить» — факт
// расписания, а не вердикт, и такой проверке полагается СВОЁ задание,
// создающее условие, а не маска и не пропуск. Тег `helmcharts` и есть это
// разделение: файл компилируется только там, где его спрашивают.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО УДЕРЖИВАЕТ САМ РАСКОЛ (иначе он тихо переживёт свой предмет)
//
// У тега два способа стать ложью, и оба закрыты в НЕТЕГИРОВАННОЙ части, потому
// что проверка, живущая под собственным тегом, о собственном невызове молчит:
//
//   - тег объявлен, но никто его не зовёт → проверка исчезает из конвейера
//     незаметно. Ловит TestChartPremiseIsActuallyInvoked: он читает конвейер и
//     требует шага, зовущего `-tags helmcharts` на этом пакете;
//   - архивы стали отслеживаться git → раскола больше нечем оправдывать, и он
//     превращается в необъяснимое исключение. Ловит
//     TestChartArchivesAreStillUntracked.
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
			"а не дерево стало чистым. Этот файл собирается под тегом `helmcharts`, то есть "+
			"его зовут ТОЛЬКО после `helm dependency build`; если архива нет и там — сломана "+
			"материализация чартов, а не проверка", p, err)
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
