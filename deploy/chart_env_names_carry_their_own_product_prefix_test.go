// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// chart_env_names_carry_their_own_product_prefix_test.go — ЧАРТ ЧАСТИ СО СВОИМ
// ИМЕНЕМ ПРОДУКТА НЕ ОБЪЯВЛЯЕТ СВОИХ ПЕРЕМЕННЫХ ПРИСТАВКОЙ ПЛАТФОРМЫ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Критерий витрины оператора: «увидит ли это тот, кто ставит службу в ЧУЖОМ
// облаке, не открывая наш исходный код». Имя переменной окружения он видит
// обычной командой — `kubectl describe` печатает и `env`, и `command`, то есть
// и объявленные переменные, и оболочечный шаг целиком.
//
// Приставка ручек самой службы уже своя и объявлена одним литералом
// (`productnaming.EnvPrefix`), а соседнюю полосу того же чарта — полосу
// поставщика личности — не тронули. Асимметрия и есть улика: это не решение о
// том, что полоса остаётся платформенной, — она поставляется ЭТИМ чартом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ОТЛИЧАЕТ НАРУШЕНИЕ ОТ ЗАКОННОГО УПОМИНАНИЯ — и это НЕ «код против
// комментария»
//
// Чарт вправе назвать ручку СОСЕДНЕЙ части: он объясняет, с чем разговаривает.
// `KACHO_REGISTRY_TOKEN_REALM` — ручка модуля реестра, и её имя в этом чарте
// законно. `KACHO_IDENTITY_SUBSTITUTED_VARS` — переменная САМОГО чарта, и её
// имя чужое.
//
// Соблазн различать их по «строка кода либо комментарий» велик и неверен:
// признак случайно совпал бы на сегодняшнем дереве и разошёлся бы на первой же
// ручке соседа, названной в исполняемой строке. Различает ПРИНАДЛЕЖНОСТЬ, и она
// спрашивается у того же источника имён:
//
//	имя вида EnvPrefix(<другая часть>) + "_…"   → ручка соседа, законно
//	любое другое имя с приставкой платформы     → своя переменная чужим именем
//
// Перечень частей ВЫВОДИТСЯ обходом дерева (каталоги под `services/` плюс имена
// чартов), а не выписывается: выписанный разошёлся бы с деревом молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРАВИЛО ВКЛЮЧАЕТСЯ ТОЛЬКО У ПЕРЕИМЕНОВАННЫХ ЧАСТЕЙ
//
// У части, чьё имя выводится приставкой платформы, приставка её переменных и
// ЕСТЬ `KACHO_` — правило для неё вырождается в тождество. Поэтому предмет у
// проверки появляется ровно вместе с записью в ведомости `RenamedServices()` и
// исчезает вместе с ней. Ведомость правится решением владельца о названии;
// проверка следует за ней, а не за памятью.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ДЕЛАЕТ
//
//   - она НЕ судит ключи значений (`.Values.kacho.*`), пути монтирования и имя
//     секрета внутреннего удостоверяющего: это соседние виды той же витрины, у
//     каждого своя цена перехода, и смешать их сюда значило бы сделать вердикт
//     непрослеживаемым. Предмет назван задачей #2129 целиком, здесь закрыта
//     одна его ось;
//   - она НЕ судит чарты частей, чьё имя выводится приставкой: у правила там нет
//     предмета by construction (см. выше). Число таких чартов печатается
//     переписью, а не подразумевается;
//   - она читает шаблоны и файл значений чарта — то, что оператор видит
//     спецификацией пода и пишет руками. Прозу README она не читает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ДОКАЗАНА СПОСОБНОСТЬ УПАСТЬ
//
// Разбор вынесен в чистую функцию auditChartEnvPrefixes, принимающую
// ОБЪЯВЛЕНИЯ. Инъекция — chart_env_names_carry_their_own_product_prefix_injection_test.go.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// platformEnvPrefix — приставка имён окружения платформы. Выводится из источника
// имён у части, чьё имя приставкой и связано: своего литерала здесь не
// заводится, иначе он стал бы вторым объявлением об одном предмете.
var platformEnvPrefix = productnaming.EnvPrefix("x")[:len("KACHO_")]

// envIdentifier — имя переменной окружения в тексте шаблона.
//
// Обход по ТЕКСТУ, а не по узлу разбора, — осознанно: файл чарта не является ни
// YAML, ни оболочкой, он шаблон, порождающий и то и другое, и разбирать его
// нечем. Цена названа: распознаватель видит и прозу. Именно поэтому нарушение
// от законного упоминания отличает ПРИНАДЛЕЖНОСТЬ имени, а не место строки, —
// иначе проверка краснела бы на собственном объяснении.
var envIdentifier = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{3,}\b`)

// chartEnvDecl — одно имя, найденное в файле чарта части продукта.
type chartEnvDecl struct {
	part string // каталог исходников части, чей это чарт
	file string // координата файла
	line int    // номер строки — находку надо где-то чинить
	name string // само имя
}

// chartEnvCensus — объём осмотренного.
type chartEnvCensus struct {
	charts     int // чартов частей продукта рассмотрено
	renamed    int // из них — со СВОИМ именем продукта (только у них есть предмет)
	files      int // файлов прочитано
	names      int // имён рассмотрено
	platform   int // из них с приставкой платформы
	peerKnobs  int // из них — законные ручки соседних частей
	ownCorrect int // имён со своей приставкой
}

// auditChartEnvPrefixes судит ОБЪЯВЛЕНИЯ и возвращает находки с переписью.
//
// peerPrefixes — приставки окружения ДРУГИХ частей продукта, выведенные из
// дерева вызывающим.
func auditChartEnvPrefixes(decls []chartEnvDecl, peerPrefixes map[string]string) ([]string, chartEnvCensus) {
	var (
		findings []string
		census   chartEnvCensus
		seen     = map[string]bool{}
	)

	for _, d := range decls {
		census.names++
		own := productnaming.EnvPrefix(d.part) + "_"

		if strings.HasPrefix(d.name, own) {
			census.ownCorrect++
			continue
		}
		if !strings.HasPrefix(d.name, platformEnvPrefix) {
			continue // имя не наше вовсе (`COURIER_…`, `SRC_…`) — предмета нет
		}
		census.platform++

		// Ручка СОСЕДНЕЙ части — законное упоминание зависимости.
		if peer, ok := longestPeerPrefixOwner(d.name, peerPrefixes); ok {
			census.peerKnobs++
			_ = peer
			continue
		}

		key := d.part + "\x00" + d.name
		if seen[key] {
			continue // одно имя — одна находка, сколько бы раз оно ни встретилось
		}
		seen[key] = true
		findings = append(findings, fmt.Sprintf(
			"%s:%d: чарт части %q объявляет СВОЮ переменную %q приставкой платформы — "+
				"оператор чужого облака видит её спецификацией пода; у этой части своё имя "+
				"продукта %q, и канон источника имён — %q",
			d.file, d.line, d.part, d.name, productnaming.ChartName(d.part), own))
	}

	sort.Strings(findings)
	return findings, census
}

// longestPeerPrefixOwner — имя принадлежит пространству имён другой части.
//
// Берётся САМАЯ ДЛИННАЯ подходящая приставка: `KACHO_API_GATEWAY_` и `KACHO_API_`
// (если бы часть `api` завелась) различаются только длиной, и короткая забрала
// бы чужие имена себе.
func longestPeerPrefixOwner(name string, peerPrefixes map[string]string) (string, bool) {
	best, bestLen := "", 0
	for prefix, part := range peerPrefixes {
		if len(prefix) > bestLen && strings.HasPrefix(name, prefix) {
			best, bestLen = part, len(prefix)
		}
	}
	return best, bestLen > 0
}

// ─────────────────────────────────────────────────────────────────────────────
// Сбор из дерева.

// productPartsInTree — части продукта, выведенные обходом дерева.
//
// Два источника, оба обходные: каталоги под `services/` (каталог исходников
// части) и имена чартов дерева (часть, чей каталог зовётся иначе, — например
// край: каталог `gateway`, чарт `api-gateway`). Выписанного перечня нет
// намеренно: он разошёлся бы с деревом молча.
func productPartsInTree(t *testing.T) map[string]bool {
	t.Helper()

	parts := map[string]bool{}

	entries, err := os.ReadDir(filepath.Join("..", "services"))
	if err != nil {
		t.Fatalf("обход каталога служб: %v — предпосылка проверки исчезла", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			parts[e.Name()] = true
		}
	}

	charts, err := filepath.Glob(filepath.Join("..", "*", "deploy", "Chart.yaml"))
	if err != nil {
		t.Fatalf("обход чартов: %v", err)
	}
	sub, err := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*", "Chart.yaml"))
	if err != nil {
		t.Fatalf("обход подчартов: %v", err)
	}
	for _, p := range append(charts, sub...) {
		name, _ := readYAML(t, p)["name"].(string)
		if name == "" {
			continue
		}
		if dir, ok := productnaming.ServiceDir(name); ok {
			parts[dir] = true
			continue
		}
		parts[name] = true
	}

	if len(parts) == 0 {
		t.Fatalf("частей продукта не найдено — обход пуст, а не дерево чисто")
	}
	return parts
}

// chartDirOfPart — каталог чарта части в умбрелле. Имя каталога здесь и есть
// имя чарта, а его даёт источник имён.
func chartDirOfPart(part string) string {
	return filepath.Join(umbrellaDir, "charts", productnaming.ChartName(part))
}

// readChartEnvDecls читает имена из шаблонов и файла значений чартов тех частей,
// у которых ЕСТЬ своё имя продукта.
func readChartEnvDecls(t *testing.T) ([]chartEnvDecl, chartEnvCensus) {
	t.Helper()

	var (
		decls  []chartEnvDecl
		census chartEnvCensus
	)

	renamed := productnaming.RenamedServices()
	parts := make([]string, 0, len(renamed))
	for part := range renamed {
		parts = append(parts, part)
	}
	sort.Strings(parts)

	for _, part := range parts {
		dir := chartDirOfPart(part)
		if _, err := os.Stat(dir); err != nil {
			// Ведомость называет часть, чьего чарта в умбрелле нет. Молчать
			// нельзя: это либо переезд чарта, либо запись, потерявшая предмет.
			t.Fatalf("ведомость имён называет часть %q (чарт %q), но каталога %s нет: %v",
				part, productnaming.ChartName(part), dir, err)
		}
		census.charts++
		census.renamed++

		var files []string
		for _, pattern := range []string{
			filepath.Join(dir, "templates", "*"),
			filepath.Join(dir, "values.yaml"),
		} {
			m, err := filepath.Glob(pattern)
			if err != nil {
				t.Fatalf("обход %s: %v", pattern, err)
			}
			files = append(files, m...)
		}
		sort.Strings(files)

		for _, f := range files {
			info, err := os.Stat(f)
			if err != nil || info.IsDir() {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			census.files++
			for i, line := range strings.Split(string(raw), "\n") {
				for _, name := range envIdentifier.FindAllString(line, -1) {
					decls = append(decls, chartEnvDecl{
						part: part, file: f, line: i + 1, name: name,
					})
				}
			}
		}
	}
	return decls, census
}

// TestChartEnvNamesCarryTheirOwnProductPrefix — гейт класса.
func TestChartEnvNamesCarryTheirOwnProductPrefix(t *testing.T) {
	decls, census := readChartEnvDecls(t)

	parts := productPartsInTree(t)
	peers := map[string]string{}
	for part := range parts {
		peers[productnaming.EnvPrefix(part)+"_"] = part
	}

	findings, walked := auditChartEnvPrefixes(decls, peers)
	walked.charts, walked.renamed, walked.files = census.charts, census.renamed, census.files

	t.Logf("перепись: частей продукта в дереве %d · чартов с собственным именем продукта %d · "+
		"файлов прочитано %d · имён рассмотрено %d · со своей приставкой %d · "+
		"с приставкой платформы %d · из них ручек соседей %d · находок %d",
		len(parts), walked.renamed, walked.files, walked.names, walked.ownCorrect,
		walked.platform, walked.peerKnobs, len(findings))

	if walked.renamed == 0 || walked.files == 0 || walked.names == 0 || walked.ownCorrect == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: чартов %d, файлов %d, имён %d, "+
			"из них со своей приставкой %d",
			walked.renamed, walked.files, walked.names, walked.ownCorrect)
	}

	if len(findings) > 0 {
		t.Fatalf("чарт объявляет свои переменные приставкой платформы (%d):\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
}
