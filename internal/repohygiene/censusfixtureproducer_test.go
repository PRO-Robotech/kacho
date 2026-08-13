// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// censusfixtureproducer_test.go — проба, которая пересчитывает ВСЕ строки
// таблицы, не вправе наполнять её сама в обход производителя.
//
// # Предмет
//
// Утверждение вида «у каждой строки зеркала есть цепь предков» — квантор по
// всему множеству. Если множество наполнила сама проба, прямой записью в
// таблицу, то утверждается свойство ФИКСТУРЫ: она положила ровно то, что потом
// пересчитала. Такая проба остаётся зелёной, даже если производитель перестал
// писать цепь ЦЕЛИКОМ, — то есть не может упасть на том единственном дефекте,
// ради которого заведена.
//
// Это измерено, а не предположено: производителю рёбер вырезали запись, и
// прежняя редакция пробы полноты прошла все свои кейсы зелёными. После перевода
// фикстуры на производителя та же инъекция даёт красный с именами объектов.
//
// # Что именно требуется
//
// Проба, содержащая перепись покрытия, обязана наполнять таблицу ТЕМ ЖЕ вызовом,
// которым это делает продукт. Прямая запись в таблицу рёбер из такой пробы —
// находка.
//
// # Чего НЕ требуется (законный близнец)
//
// Проба ЧИТАТЕЛЯ — та, что спрашивает о конкретном объекте, — вправе класть
// рёбра прямо: её предмет не «кто их написал», а «что по ним выводится». Гейт
// обязан молчать на ней, иначе он ловит форму, а не существо. На 2026-08-12
// таких проб в дереве шесть, и они проходят.
//
// # Разбор идёт по СТРОКОВЫМ ЛИТЕРАЛАМ, а не по тексту файла
//
// Обе приметы — SQL, то есть живут в литералах. Текстовый поиск нашёл бы их и в
// комментарии, объясняющем эту же дисциплину (в том числе в шапке самой пробы
// полноты), и гейт краснел бы на объяснении вместо исполнения.

const (
	// Приметы, по которым узнаются перепись покрытия и прямая запись. Разъедутся
	// со схемой — гейт упадёт на своей предпосылке (перепись найдёт ноль файлов),
	// а не промолчит.
	//
	// ПЕРЕПИСЬ ПОКРЫТИЯ — это ОДИН стейтмент, называющий ОБЕ таблицы: он
	// спрашивает про рёбра у строк зеркала, то есть квантифицирует по множеству.
	// Первая редакция приметы искала одно лишь чтение зеркала и пометила ШЕСТЬ
	// проб, читающих зеркало по конкретному объекту, — предикат ловил форму
	// (обращение к таблице), а не существо (квантор).
	censusMirrorTable  = "kacho_iam.resource_mirror"
	censusEdgeTable    = "kacho_iam.resource_parent_edge"
	rawEdgeWriteMarker = "INSERT INTO kacho_iam.resource_parent_edge"
	edgeProducerCall   = "UpsertTx"
)

// stringLiteralsOf возвращает значения всех строковых литералов файла.
func stringLiteralsOf(t *testing.T, name string, body []byte) (literals []string, calls map[string]bool) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, body, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("разбор %s: %v", name, err)
	}
	calls = map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				if v, err := strconv.Unquote(node.Value); err == nil {
					literals = append(literals, v)
				}
			}
		case *ast.CallExpr:
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
				calls[sel.Sel.Name] = true
			}
		}
		return true
	})
	return literals, calls
}

// TestCensusFixturesSeedThroughTheProducer — фикстура пробы-переписи идёт через
// производителя, а не пишет предмет переписи сама.
func TestCensusFixturesSeedThroughTheProducer(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, "services", "iam")
	files, err := treecorpus.UnderWithSuffix(base, "_test.go")
	if err != nil {
		t.Fatalf("состав тестового дерева под %s: %v", base, err)
	}

	scanned, censusFiles, rawWriters := 0, 0, 0
	var findings []string
	for _, path := range files {
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", path, rerr)
		}
		scanned++
		rel, _ := filepath.Rel(root, path)

		src := string(body)
		if !strings.Contains(src, censusEdgeTable) {
			continue
		}
		literals, calls := stringLiteralsOf(t, rel, body)
		census, rawWrite := false, false
		for _, lit := range literals {
			if strings.Contains(lit, censusMirrorTable) && strings.Contains(lit, censusEdgeTable) {
				census = true
			}
			if strings.Contains(lit, rawEdgeWriteMarker) {
				rawWrite = true
			}
		}
		if rawWrite {
			rawWriters++
		}
		if !census {
			// Законный близнец: проба читателя. Спрашивает об ОДНОМ объекте,
			// поэтому её фикстура вправе класть рёбра прямо.
			continue
		}
		censusFiles++
		if rawWrite {
			findings = append(findings, rel+
				" — проба пересчитывает все строки зеркала и САМА кладёт рёбра прямой записью: "+
				"она утверждает свойство своей фикстуры и останется зелёной, даже если "+
				"производитель перестанет писать цепь целиком")
			continue
		}
		if !calls[edgeProducerCall] {
			findings = append(findings, rel+
				" — проба пересчитывает все строки зеркала, но не зовёт производителя ("+
				edgeProducerCall+"): непонятно, чьё свойство она утверждает")
		}
	}

	// Объём осмотренного — отдельное утверждение.
	t.Logf("осмотрено тестовых файлов iam: %d; из них проб-переписей: %d; "+
		"проб с прямой записью рёбер (включая законных читателей): %d",
		scanned, censusFiles, rawWriters)
	if scanned == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	// Предпосылка гейта: предмет существует. Исчезнет перепись — требование
	// обязано исчезнуть ГРОМКО, а не превратиться в вечно-зелёную проверку.
	if censusFiles == 0 {
		t.Fatalf("в дереве нет ни одной пробы, пересчитывающей покрытие рёбрами "+
			"(один стейтмент, называющий %q и %q): предмет гейта отпал — снимите гейт "+
			"вместе с механизмом либо почините примету", censusMirrorTable, censusEdgeTable)
	}

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("перепись утверждает свойство собственной фикстуры:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
