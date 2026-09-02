// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// Гейт: имя пробы, названное приёмкой КООРДИНАТОЙ, резолвится функцией в дереве.
//
// # Предмет
//
// Приёмка ссылается на пробу, чтобы читатель мог её открыть и прогнать. Проба
// переживает не всякую правку: её переименовывают, сводят с соседней, снимают
// вместе с предметом — и делают это в СВОЁМ изменении, которое чужой приёмки не
// касается. Тогда координата остаётся стоять, а функции за ней нет.
//
// Класс — утверждение, пережившее свой предмет, и он опаснее обычной устаревшей
// строки: следующий идёт по названному адресу, не находит ничего и делает вывод
// О ДЕРЕВЕ, а не о документе.
//
// # Почему координатой считается ТОЛЬКО целый пролёт кода
//
// Имя пробы встречается в приёмке в трёх видах, и лишь один из них — координата:
//
//	`TestFoo`                            — КООРДИНАТА: пролёт целиком есть имя
//	`go test -run '^TestFoo$' -count=1`  — предикат: имя стоит внутри команды
//	«проба TestFoo снята вместе с предметом» — проза разбора
//
// Проверка по подстроке краснела бы на втором и третьем, то есть на СОБСТВЕННОМ
// объяснении документа — ровно тот класс, который корпус ловит в гейтах
// (`testing.md` §«Гейт на класс», п. 4). Поэтому документ читается РАЗОБРАННЫМ:
// огороженные блоки кода пропускаются целиком, а из строки берутся только
// пролёты `…`, чьё содержимое ЦЕЛИКОМ есть имя пробы.
//
// # Почему резолв по ПРЕФИКСУ, а не по равенству
//
// Корпус называет пробу двумя законными формами: полным именем и
// ИДЕНТИФИКАТОРОМ СЦЕНАРИЯ (`TestIAMCT112`), которым отбирают семейство —
// `-run '^TestIAMCT112'`. Вторая форма — не небрежность: у одного сценария бывает
// шесть проб, и называть их поимённо значило бы держать перечень, стареющий
// молча. Замер на ревизии заведения: идентификатором записаны ДЕВЯТЬ имён —
// требуй гейт равенства, он дал бы девять ложных находок и был бы снят первым
// же читателем. Общее число координат здесь не пишется: его печатает перепись
// на каждом прогоне, а второе место об одном предмете разошлось бы молча.
//
// Отсюда правило: координата резолвится, если ОБЪЯВЛЕННОЕ имя начинается с неё.
// Направление существенно. Переименование `TestMODMR10RolesSectionLoads‹хвост›` →
// `TestMODMR10RolesSectionLoads` этим правилом НЕ прощается: объявленное короче
// координаты и её префиксом не является.
//
// # Граница разборщика, названная замером, а не догадкой
//
// Огороженный блок ВНУТРИ цитаты (`> ```sh`) разборщик огораживанием не считает:
// строка начинается со знака цитаты, а не с забора. То есть содержимое такого
// блока читается как обычный текст. Расширять разбор на этот случай не за чем —
// перепись обеих форм совпала ДОСЛОВНО по всем трём величинам (координаты,
// резолвы, мёртвые имена), значит предмета у расширения нет, а расширение,
// ничего не меняющее в
// переписи, снимают, а не держат про запас (`testing.md` §«Гейт на класс», п. 7).
// Появится координата внутри цитируемого забора — она будет посчитана, и это
// ужесточение, а не пропуск.

// probeCoordinateShape — форма имени пробы Go. Три знака минимум после вида:
// голое `Test` резолвилось бы префиксом ко всему дереву.
var probeCoordinateShape = regexp.MustCompile(`^(Test|Fuzz|Benchmark|Example)[A-Za-z0-9_]{2,}$`)

var (
	probeCoordinateFence  = regexp.MustCompile("^\\s*(```|~~~)")
	probeCoordinateInline = regexp.MustCompile("`([^`\n]+)`")
	probeCoordinateDecl   = regexp.MustCompile(`(?m)^func ((?:Test|Fuzz|Benchmark|Example)[A-Za-z0-9_]*)\s*\(`)
)

// probeCoordinate — одно вхождение координаты: имя и место, где оно стоит.
type probeCoordinate struct {
	Name string
	Doc  string
	Line int
}

// deadProbeCoordinate — ПОСЛАБЛЕНИЕ: координата, о которой известно, что она не
// резолвится, и чей предмет принадлежит ДРУГОМУ кругу приёмки.
//
// Запись заводится ПО ФАКТУ, а не с запасом: имя и перечень документов —
// дословно те, что даёт обход на ревизии заведения записи. Потолка здесь нет
// намеренно — потолок не краснеет никогда и потому не истекает (`testing.md`
// §«Ведомость, записанная ШИРЕ предмета»).
//
// Послабление ИСТЕКАЕТ САМО, и оба конца — находка:
//   - имя стало резолвиться → исключать нечего;
//   - имя больше не стоит ни в одном документе → исключать нечего.
type deadProbeCoordinate struct {
	Name  string
	Docs  []string
	Issue int
	Note  string
}

// acceptanceProbeCoordinateExemptions — ВЕДОМОСТЬ ПУСТА, и это её цель, а не
// её поломка.
//
// Здесь стоял остаток ревизии заведения — девять имён, 20 вхождений в шести
// документах. Каждое разобрано задачей `#1892`: родство с живыми соседями
// установлено замером, а не догадкой, и по каждому имени назван исход прямо в
// документе. Исходов вышло три, и ни один не есть ослабление:
//
//   - имя ПЕРЕИМЕНОВАНО, а преемница утверждает ТО ЖЕ — координата переведена
//     на неё (проба дословности прозы блока: Doc → Note);
//   - имя снято ВМЕСТЕ с предметом либо преемница утверждает ДРУГОЕ — прежнее
//     имя написано ПРОЗОЙ. Живой адрес с ложным содержанием хуже мёртвого:
//     он не краснеет;
//   - имя РОДОВОЕ (форма имени пробы из документации Go) — записано подписью
//     func TestXxx(*testing.T), то есть перестало быть пролётом кода.
//
// Ведомость остаётся как МЕХАНИЗМ: следующий круг, чья координата переживёт
// свой предмет, заводит запись с номером и предикатом снятия, а не молчит.
// Способность гейта упасть и смолчать на обоих концах послабления доказывается
// синтетикой в `acceptanceprobecoordinate_injection_test.go`, а не наличием
// живой записи здесь, — иначе доказательство исчезало бы ровно тогда, когда
// ведомость правильно опустела.
var acceptanceProbeCoordinateExemptions []deadProbeCoordinate

// probeCoordinatesIn разбирает документ и возвращает координаты — только их.
func probeCoordinatesIn(doc, body string) []probeCoordinate {
	var found []probeCoordinate
	inFence := false
	for i, line := range strings.Split(body, "\n") {
		if probeCoordinateFence.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range probeCoordinateInline.FindAllStringSubmatch(line, -1) {
			// `TestFoo/подпроба` — координата семейства; судится основание.
			name := strings.TrimSpace(m[1])
			if idx := strings.IndexByte(name, '/'); idx >= 0 {
				name = name[:idx]
			}
			if !probeCoordinateShape.MatchString(name) {
				continue
			}
			found = append(found, probeCoordinate{Name: name, Doc: doc, Line: i + 1})
		}
	}
	return found
}

// probeCoordinateResolves — объявленное имя начинается с координаты.
// declared обязан быть отсортирован: имя с префиксом P сортируется не раньше P,
// поэтому кандидат ровно один и находится двоичным поиском.
func probeCoordinateResolves(name string, declared []string) bool {
	i := sort.SearchStrings(declared, name)
	return i < len(declared) && strings.HasPrefix(declared[i], name)
}

// probeCoordinateCensus — перепись обхода. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного», поэтому объём осмотренного — отдельное утверждение.
type probeCoordinateCensus struct {
	Docs        int
	Declared    int
	Coordinates int
	Resolved    int
	Exempted    int
	Findings    []string
}

// judgeProbeCoordinates — судящее ядро. Вход подаётся значениями, а не читается
// из дерева: инъекция обязана уметь дать ему свой вход, не трогая рабочую копию,
// из которой запущена (`multi-agent-flow.md` §13).
func judgeProbeCoordinates(docs map[string]string, declared []string, exemptions []deadProbeCoordinate) probeCoordinateCensus {
	sorted := append([]string(nil), declared...)
	sort.Strings(sorted)

	exempt := make(map[string]*deadProbeCoordinate, len(exemptions))
	for i := range exemptions {
		exempt[exemptions[i].Name] = &exemptions[i]
	}
	used := make(map[string]bool, len(exemptions))

	c := probeCoordinateCensus{Docs: len(docs), Declared: len(sorted)}

	paths := make([]string, 0, len(docs))
	for p := range docs {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		for _, co := range probeCoordinatesIn(p, docs[p]) {
			c.Coordinates++
			if probeCoordinateResolves(co.Name, sorted) {
				c.Resolved++
				continue
			}
			if e, ok := exempt[co.Name]; ok {
				c.Exempted++
				used[e.Name] = true
				continue
			}
			c.Findings = append(c.Findings, "МЁРТВАЯ КООРДИНАТА "+co.Doc+":"+strconv.Itoa(co.Line)+
				" — приёмка называет пробу `"+co.Name+"`, а функции, чьё имя с неё начинается, "+
				"в дереве НЕТ. Исходов три: назвать преемницу — но только прочитав ЕЁ ТЕЛО, "+
				"потому что живой адрес с ложным содержанием хуже мёртвого: он не краснеет; "+
				"снять координату, написав имя ПРОЗОЙ, — если проба снята вместе с предметом "+
				"либо преемница утверждает ДРУГОЕ (свидетельство круга остаётся, живого адреса "+
				"не заводится); либо — когда правка сделала бы вердикт круга непрослеживаемым — "+
				"завести запись послабления с номером задачи и предикатом снятия")
		}
	}

	// Второй конец самоистечения: послабление, которому нечего исключать.
	for _, e := range exemptions {
		if used[e.Name] {
			continue
		}
		why := "имя больше не стоит ни в одной приёмке"
		if probeCoordinateResolves(e.Name, sorted) {
			why = "имя снова резолвится функцией в дереве"
		}
		c.Findings = append(c.Findings, "ПОСЛАБЛЕНИЕ БЕЗ ПРЕДМЕТА `"+e.Name+"` — "+why+
			". Запись снимается тем же изменением: послабление, которому нечего исключать, "+
			"достаётся следующему читателю и прощает уже другое")
	}
	return c
}

// acceptanceDocsOfTree — приёмки, живущие рядом с кодом сервиса. Перечень
// ВЫВОДИТСЯ обходом, а не выписывается: сервис, заведший свой каталог приёмок
// завтра, попадает под гейт by construction.
func acceptanceDocsOfTree(t *testing.T, root string) map[string]string {
	t.Helper()
	all, err := treecorpus.Under(filepath.Join(root, "services"))
	if err != nil {
		t.Fatalf("состав дерева служб: %v", err)
	}
	docs := map[string]string{}
	for _, abs := range all {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			t.Fatalf("относительный путь для %s: %v", abs, err)
		}
		rel = filepath.ToSlash(rel)
		if !strings.Contains(rel, "/docs/engineering/acceptance/") || !strings.HasSuffix(rel, ".md") {
			continue
		}
		body, err := os.ReadFile(abs)
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		docs[rel] = string(body)
	}
	return docs
}

// declaredProbesOfTree — имена всех проб дерева, объявленных в отслеживаемых
// файлах проб.
func declaredProbesOfTree(t *testing.T, root string) []string {
	t.Helper()
	all, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	seen := map[string]bool{}
	for _, abs := range all {
		if !strings.HasSuffix(abs, "_test.go") {
			continue
		}
		body, err := os.ReadFile(abs)
		if err != nil {
			t.Fatalf("чтение %s: %v", abs, err)
		}
		for _, m := range probeCoordinateDecl.FindAllStringSubmatch(string(body), -1) {
			seen[m[1]] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// TestAcceptanceProbeCoordinateResolves — гейт класса «утверждение, пережившее
// свой предмет», в его самой проверяемой форме: адрес, названный документом,
// обязан существовать.
func TestAcceptanceProbeCoordinateResolves(t *testing.T) {
	root := repoRoot(t)

	docs := acceptanceDocsOfTree(t, root)
	declared := declaredProbesOfTree(t, root)
	c := judgeProbeCoordinates(docs, declared, acceptanceProbeCoordinateExemptions)

	t.Logf("осмотрено: приёмок %d · объявлений проб в дереве %d · координат %d · "+
		"резолвится %d · по ведомости %d · находок %d",
		c.Docs, c.Declared, c.Coordinates, c.Resolved, c.Exempted, len(c.Findings))

	// Пустой обход — ОТКАЗ, а не пустой успех: «ноль находок» на «ноль
	// прочитанного» неотличимо от чистого дерева.
	if c.Docs == 0 {
		t.Fatal("приёмок рядом с кодом служб не прочитано ни одной — вердикт беспредметен: " +
			"проверь, не переехал ли каталог docs/engineering/acceptance")
	}
	if c.Declared == 0 {
		t.Fatal("объявлений проб в дереве не найдено ни одного — резолвить координаты не с чем, " +
			"и всякая из них была бы объявлена мёртвой")
	}
	if c.Coordinates == 0 {
		t.Fatal("координат проб в приёмках не распознано ни одной при непустом корпусе — " +
			"разборщик не знает формы записи, а не документы её лишились")
	}
	for _, f := range c.Findings {
		t.Error(f)
	}
}
