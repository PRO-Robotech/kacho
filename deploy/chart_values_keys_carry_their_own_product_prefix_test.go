// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// chart_values_keys_carry_their_own_product_prefix_test.go — КЛЮЧИ ЗНАЧЕНИЙ И
// ПУТИ МОНТИРОВАНИЯ ЧАРТА ПРИНАДЛЕЖАТ ЕГО СОБСТВЕННОМУ ПРОДУКТУ, А ЧИТАТЕЛЬ
// КЛЮЧА И ЕГО ОБЪЯВЛЕНИЕ СУДЯТСЯ ПАРОЙ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Критерий витрины оператора: «увидит ли это тот, кто ставит службу в ЧУЖОМ
// облаке, не открывая наш исходный код». Здесь он выполняется по двум видам:
//
//	ключ значений      он пишет РУКАМИ в своём профиле;
//	путь монтирования  он читает `kubectl describe pod` — путь печатается дословно.
//
// Соседняя проверка (chart_env_names_carry_their_own_product_prefix_test.go)
// закрыла по задаче #2129 ось ИМЁН ОКРУЖЕНИЯ и прямо сказала, что ключей
// значений и путей монтирования не судит. Здесь закрыты оставшиеся две.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПАРА, А НЕ ДВЕ ПРОВЕРКИ ПО ОТДЕЛЬНОСТИ — ЭТО НЕСУЩЕЕ
//
// Переименование ОДНОЙ половины даёт чарт, который РЕНДЕРИТСЯ И НЕ РАБОТАЕТ:
// оператор задаёт новый ключ, шаблон читает старый и получает пустое значение.
// Helm незнакомый ключ верхнего уровня не отвергает, поэтому отказа нет ни у
// оператора, ни у рендера — величина просто не доезжает, и обнаруживается это
// в кластере, а не здесь.
//
// Отсюда правило A: первый сегмент КАЖДОГО пути `.Values.<сегмент>`, который
// шаблон ЧИТАЕТ, обязан быть ОБЪЯВЛЕН в `values.yaml` того же чарта. Оно ловит
// половинчатое переименование в обе стороны и ловит ключ, которого нет ни в
// одном файле значений: такой ключ оператор не может ни найти, ни прочесть — он
// виден только тому, кто открыл наш шаблон.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ПРАВИЛО A НЕ СУДИТ, И ПОЧЕМУ ЭТО НЕ ВЕДОМОСТЬ ИСКЛЮЧЕНИЙ
//
// Сегмент `global` пропускается. Это не послабление и не запись перечня: `global`
// — СОБСТВЕННОЕ зарезервированное слово helm, пространство значений ЗОНТА, а не
// подчарта. Объявляет его зонт, читают несколько чартов сразу, и требовать
// объявления у подчарта значило бы требовать, чтобы сосед объявил чужое. По той
// же причине его имя не судится правилом Б: приставка платформы в пространстве
// имён ПЛАТФОРМЫ — это имя владельца, а не чужое имя (тот же разделитель, что у
// соседней проверки: ручка соседа названа его приставкой законно).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРАВИЛА ПОЛОЖИТЕЛЬНЫЕ, А НЕ «НЕ УПОМИНАТЬ ЧУЖОЕ ИМЯ»
//
// Запрет по литералу чужого имени ловит ровно то имя, которое в него вписали, и
// молчит на следующем. Правила Б и В сформулированы от СВОЕГО имени продукта,
// поэтому предмет у них появляется вместе с записью в ведомости
// productnaming.RenamedServices() и исчезает вместе с ней. У части, чьё имя
// выводится приставкой платформы, правило вырождается в тождество — и предмета
// у него нет by construction, а не по исключению.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ДЕЛАЕТ
//
//   - не судит ЗНАЧЕНИЯ (адреса, домены, координаты образа) — их судят соседние
//     пробы каталога, и два места об одном предмете разошлись бы молча;
//   - не судит имя ПЛАТФОРМЕННОГО объекта, на который чарт ссылается: издатель
//     внутреннего удостоверяющего чеканится зонтом и назван восемью чартами
//     сразу. Ссылка на чужой объект его именем — не чужая приставка, а адрес;
//   - не судит прозу комментариев и README: чарт вправе назвать соседа,
//     объясняя, с чем он разговаривает. Различает не «код против комментария», а
//     то, что ключ ЧИТАЕТСЯ из значений либо ВЫВОДИТСЯ в манифест.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЕДИНИЦА СЧЁТА — КЛЮЧ (и путь), осмотренное печатается отдельно от найденного
//
// Обход, не давший ни одного читаемого ключа, ни одного объявленного корня либо
// ни одного пути, есть ОТКАЗ, а не чистый чарт: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ДОКАЗАНА СПОСОБНОСТЬ УПАСТЬ
//
// Разбор вынесен в чистую функцию auditChartValuesKeys, принимающую ОБЪЯВЛЕНИЯ.
// Инъекция — chart_values_keys_carry_their_own_product_prefix_injection_test.go:
// вход НАСТОЯЩИЙ, каждый случай меняет ровно один факт, контроль в обе стороны.

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

// platformValuesName — имя платформы так, как оно пишется в ключе значений и в
// пути монтирования. Выводится из источника имён у части, чьё имя приставкой и
// связано: своего литерала здесь не заводится, иначе он стал бы вторым
// объявлением об одном предмете.
var platformValuesName = productnaming.ProductName("\x00not-a-service")

// umbrellaGlobalRoot — зарезервированное слово helm: пространство значений ЗОНТА.
// Объявляет его зонт, а не подчарт (см. шапку).
const umbrellaGlobalRoot = "global"

// valuesReadPath — обращение шаблона к дереву значений. Берётся ПЕРВЫЙ сегмент:
// именно он есть корень, который оператор пишет и который чарт обязан объявить.
var valuesReadPath = regexp.MustCompile(`\.Values\.([A-Za-z_][A-Za-z0-9_]*)`)

// valuesDeclaredRoot — корень, ОБЪЯВЛЕННЫЙ файлом значений: ключ в нулевой
// колонке. Вложенные ключи корнями не являются и оператору как корень не видны.
var valuesDeclaredRoot = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*):`)

// mountPathLiteral — путь монтирования, записанный ЛИТЕРАЛОМ. Путь, собранный
// шаблоном из значения, судить нечем: его величина здесь неизвестна, и
// утверждать о ней значило бы выдавать догадку за замер.
var mountPathLiteral = regexp.MustCompile(`mountPath:\s*"?(/[A-Za-z0-9_./-]*)`)

// chartValuesDecl — одно наблюдение в чарте части продукта.
type chartValuesDecl struct {
	part string // каталог исходников части, чей это чарт
	file string // координата файла
	line int    // номер строки — находку надо где-то чинить
	kind string // "read" | "declared" | "mount"
	name string // корень значений либо путь монтирования
}

// chartValuesCensus — объём осмотренного.
type chartValuesCensus struct {
	charts     int // чартов частей со своим именем продукта
	files      int // файлов прочитано
	reads      int // обращений к дереву значений рассмотрено
	roots      int // из них РАЗНЫХ корней
	declared   int // объявленных корней рассмотрено
	mounts     int // путей монтирования рассмотрено
	pairOK     int // корней, у которых читатель и объявление сошлись
	ownCorrect int // корней и путей БЕЗ приставки платформы
}

// auditChartValuesKeys судит ОБЪЯВЛЕНИЯ и возвращает находки с переписью.
func auditChartValuesKeys(decls []chartValuesDecl) ([]string, chartValuesCensus) {
	var (
		findings []string
		census   chartValuesCensus
	)

	// Объявленные корни — по частям: чарт судится СВОИМ файлом значений.
	declaredBy := map[string]map[string]bool{}
	for _, d := range decls {
		if d.kind != "declared" {
			continue
		}
		census.declared++
		if declaredBy[d.part] == nil {
			declaredBy[d.part] = map[string]bool{}
		}
		declaredBy[d.part][d.name] = true
	}

	seenRead := map[string]bool{}
	seenFinding := map[string]bool{}

	for _, d := range decls {
		switch d.kind {
		case "read":
			census.reads++
			if d.name == umbrellaGlobalRoot {
				continue // пространство значений зонта — объявляет его зонт
			}
			key := d.part + "\x00" + d.name
			if !seenRead[key] {
				seenRead[key] = true
				census.roots++
				if declaredBy[d.part][d.name] {
					census.pairOK++
				}
			}
			// ── правило A: пара «читатель ↔ объявление» ──────────────────
			if !declaredBy[d.part][d.name] && !seenFinding["A"+key] {
				seenFinding["A"+key] = true
				findings = append(findings, fmt.Sprintf(
					"%s:%d: чарт части %q ЧИТАЕТ `.Values.%s`, но ни один его файл значений "+
						"этого корня НЕ ОБЪЯВЛЯЕТ — helm незнакомый ключ не отвергает, поэтому "+
						"величина молча не доедет, а оператор не может ключ ни найти, ни прочесть",
					d.file, d.line, d.part, d.name))
			}
			// ── правило Б: корень назван СВОИМ именем продукта ───────────
			findings = appendPlatformFinding(findings, seenFinding, "B"+key, d,
				fmt.Sprintf("чарт части %q адресует свой ключ значений `.Values.%s` именем "+
					"платформы — оператор чужого облака пишет этот ключ руками; у этой части "+
					"своё имя продукта %q", d.part, d.name, productnaming.ProductName(d.part)),
				d.name, &census)

		case "declared":
			key := d.part + "\x00" + d.name
			if d.name == umbrellaGlobalRoot {
				continue
			}
			findings = appendPlatformFinding(findings, seenFinding, "D"+key, d,
				fmt.Sprintf("чарт части %q ОБЪЯВЛЯЕТ корень значений %q именем платформы — "+
					"оператор чужого облака пишет этот ключ руками; у этой части своё имя "+
					"продукта %q", d.part, d.name, productnaming.ProductName(d.part)),
				d.name, &census)

		case "mount":
			census.mounts++
			key := d.part + "\x00" + d.name
			findings = appendPlatformFinding(findings, seenFinding, "M"+key, d,
				fmt.Sprintf("чарт части %q монтирует путь %q, названный именем платформы — "+
					"оператор чужого облака читает его спецификацией пода; у этой части своё "+
					"имя продукта %q", d.part, d.name, productnaming.ProductName(d.part)),
				lastPathSegmentsOf(d.name), &census)
		}
	}

	sort.Strings(findings)
	return findings, census
}

// appendPlatformFinding — общий разбор правил Б и В: имя платформы отдельным
// СЛОВОМ, а не подстрокой. Подстрока прощала бы `kachostan` и ловила бы
// `nakacho`; слово различает то, что различает и читатель.
func appendPlatformFinding(
	findings []string, seen map[string]bool, key string,
	d chartValuesDecl, why string, subject string, census *chartValuesCensus,
) []string {
	if !namesPlatform(subject) {
		census.ownCorrect++
		return findings
	}
	if seen[key] {
		return findings
	}
	seen[key] = true
	return append(findings, fmt.Sprintf("%s:%d: %s", d.file, d.line, why))
}

// namesPlatform — строка называет платформу ОТДЕЛЬНЫМ словом.
func namesPlatform(s string) bool {
	low := strings.ToLower(s)
	for i := 0; i+len(platformValuesName) <= len(low); i++ {
		if low[i:i+len(platformValuesName)] != platformValuesName {
			continue
		}
		if i > 0 && isWordByte(low[i-1]) {
			continue
		}
		if j := i + len(platformValuesName); j < len(low) && isWordByte(low[j]) {
			continue
		}
		return true
	}
	return false
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}

// lastPathSegmentsOf — путь как предмет правила В: судятся его СЕГМЕНТЫ, чтобы
// `/etc/kacho-identity` попадало, а `/etc/ssl` — нет.
func lastPathSegmentsOf(p string) string {
	return strings.ReplaceAll(strings.Trim(p, "/"), "/", " ")
}

// ─────────────────────────────────────────────────────────────────────────────
// Сбор из дерева.

// readChartValuesDecls читает шаблоны и файл значений чартов тех частей, у
// которых ЕСТЬ своё имя продукта.
func readChartValuesDecls(t *testing.T) []chartValuesDecl {
	t.Helper()

	var decls []chartValuesDecl

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
			// нельзя: это либо переезд чарта, либо запись без предмета.
			t.Fatalf("ведомость имён называет часть %q (чарт %q), но каталога %s нет: %v",
				part, productnaming.ChartName(part), dir, err)
		}

		valuesFile := filepath.Join(dir, "values.yaml")
		tmpl, err := filepath.Glob(filepath.Join(dir, "templates", "*"))
		if err != nil {
			t.Fatalf("обход шаблонов %s: %v", dir, err)
		}
		files := append([]string{valuesFile}, tmpl...)
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
			isValues := f == valuesFile
			for i, line := range strings.Split(string(raw), "\n") {
				at := i + 1
				for _, m := range valuesReadPath.FindAllStringSubmatch(line, -1) {
					decls = append(decls, chartValuesDecl{part, f, at, "read", m[1]})
				}
				for _, m := range mountPathLiteral.FindAllStringSubmatch(line, -1) {
					decls = append(decls, chartValuesDecl{part, f, at, "mount", m[1]})
				}
				if isValues {
					if m := valuesDeclaredRoot.FindStringSubmatch(line); m != nil {
						decls = append(decls, chartValuesDecl{part, f, at, "declared", m[1]})
					}
				}
			}
		}
	}
	return decls
}

// TestChartValuesKeysCarryTheirOwnProductPrefix — гейт класса.
func TestChartValuesKeysCarryTheirOwnProductPrefix(t *testing.T) {
	decls := readChartValuesDecls(t)

	charts := map[string]bool{}
	files := map[string]bool{}
	for _, d := range decls {
		charts[d.part] = true
		files[d.file] = true
	}

	findings, census := auditChartValuesKeys(decls)
	census.charts, census.files = len(charts), len(files)

	t.Logf("перепись: чартов с собственным именем продукта %d · файлов прочитано %d · "+
		"обращений к значениям %d · из них разных корней %d · корней объявлено %d · "+
		"пар «читатель↔объявление» сошлось %d · путей монтирования %d · "+
		"имён без приставки платформы %d · находок %d",
		census.charts, census.files, census.reads, census.roots, census.declared,
		census.pairOK, census.mounts, census.ownCorrect, len(findings))

	if census.charts == 0 || census.files == 0 || census.reads == 0 ||
		census.declared == 0 || census.mounts == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: чартов %d, файлов %d, обращений %d, "+
			"объявленных корней %d, путей монтирования %d",
			census.charts, census.files, census.reads, census.declared, census.mounts)
	}

	if len(findings) > 0 {
		t.Fatalf("ключи значений и пути монтирования чарта расходятся с деревом (%d):\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
}
