// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strconv"
	"strings"
	"testing"
)

// licensecompat_test.go — ГЕЙТ: ни одно ребро импорта в дереве не пересекает
// несовместимую лицензионную границу.
//
// Предмет, правило совместимости и разбор обеих его сторон — в шапке
// `licensecompat.go`; здесь они не пересказываются, чтобы два места об одном
// предмете не разошлись.
//
// Что делает именно этот файл: добывает РЕАЛЬНЫЕ рёбра дерева и предъявляет их
// разбору. Способность разбора падать и молчать доказывается отдельно —
// `licensecompat_injection_test.go`.
//
// # Вход берётся РАЗБОРОМ, и это несущее свойство, а не стиль
//
// Пути импорта службы встречаются в этом дереве десятками — в комментариях, в
// строковых константах гейтов и в фикстурах чужих инъекций (замер: `git grep -n
// 'PRO-Robotech/kaname' -- ':!services/iam'` даёт 23 строки, и НИ ОДНА из них не
// импорт). Гейт по образцу объявил бы находкой собственное объяснение. Поэтому
// импорты читаются узлом синтаксического дерева — `readTreePackages`, общая с
// гейтом границы фундамента.

// TestNoImportCrossesAnIncompatibleLicenseBoundary — сам гейт.
func TestNoImportCrossesAnIncompatibleLicenseBoundary(t *testing.T) {
	root := repoRoot(t)
	pkgs, files := readTreePackages(t, root)

	edges := make([]licenseEdge, 0, 4096)
	for _, p := range pkgs {
		collect := func(kind string, imports map[string]int) {
			for imp, n := range imports {
				// Второй результат не читается намеренно: путь вне обоих модулей
				// продукта приходит пустым и уезжает в графу «наружу» переписи.
				// Предмет чужих модулей — у dependencylicense.go.
				tree, _ := treePathOfImport(imp)
				edges = append(edges, licenseEdge{
					FromDir: p.Dir, ToTree: tree, Import: imp, Kind: kind, Files: n,
				})
			}
		}
		collect(licenseEdgeProd, p.Prod)
		collect(licenseEdgeTest, p.Test)
	}

	findings, census := scanLicenseCompat(edges, len(pkgs), files)
	t.Log(census.String())

	// Обход пуст — вердикт беспредметен. «Находок 0» здесь означало бы «не
	// прочитано ничего», и чинить надо было бы добычу входа, а не дерево.
	if census.Edges == 0 {
		t.Fatalf("рёбер внутри продукта не разобрано НИ ОДНОГО из %d осмотренных — "+
			"обход пуст, вердикт беспредметен\n%s", len(edges), census.String())
	}

	// Положительный контроль, и он же — страж собственной добычи входа.
	//
	// Служба под AGPL линкует фундамент под Apache-2.0: это ровно то ребро, ради
	// законности которого фундамент и перелицензирован, и оно ОБЯЗАНО быть
	// видимо гейту. Сломайся обход дерева или перевод пути импорта — пара
	// исчезла бы, находок по-прежнему было бы ноль, и гейт зеленел бы, не
	// осмотрев предмета. Ключ пары собирается вызовом, а не литералом: имена
	// уровней принадлежат licensemap.go и вправе меняться там.
	want := licensePairKey(licenseTierForDir("services/iam"), licenseTierForDir("pkg"))
	if census.Pairs[want] == 0 {
		t.Fatalf("положительный контроль не сработал: пары %q в переписи нет.\n"+
			"Служба под AGPL обязана линковать фундамент под Apache-2.0 — если этой\n"+
			"пары не видно, гейт не осмотрел предмет, а не дерево стало чистым.\n%s",
			want, census.String())
	}

	if len(findings) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("рёбер, пересекающих несовместимую лицензионную границу: ")
	b.WriteString(strconv.Itoa(len(findings)))
	b.WriteString("\n")
	for _, f := range findings {
		b.WriteString("  ")
		b.WriteString(f.String())
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(census.String())
	b.WriteString("\n\nЛинковка даёт производную работу, поэтому импортирующий принимает\n")
	b.WriteString("обязательства импортируемого. Ошибка НЕОБРАТИМА: у того, кто получил\n")
	b.WriteString("опубликованный код, права остаются, и отзыв публикации их не отменяет.\n")
	b.WriteString("Исходов три: снять ребро · перенести импортируемое на пермиссивный\n")
	b.WriteString("уровень · перевести импортирующего на ту же лицензию. Четвёртого —\n")
	b.WriteString("«прощённая запись» — здесь не заводится: она есть утверждение о праве\n")
	b.WriteString("получателя, которого мы делать не вправе.\n")
	b.WriteString("Отображение путь→лицензия, которым вынесен вердикт: licensemap.go\n")
	t.Fatal(b.String())
}
