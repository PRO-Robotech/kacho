// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
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
// # Единица суждения — ПАКЕТ, а не файл (исправлено по #778)
//
// Прежняя редакция разбирала по одному файлу и требовала, чтобы имя
// производителя стояло в ТОМ ЖЕ файле. Это ловило форму, а не существо: в Go
// все `_test.go` каталога с одним именем пакета собираются в один бинарь и
// делят помощников. Проба, сеющая через помощника из соседнего файла, сеет
// через производителя ровно так же — а гейт объявлял её находкой.
//
// Цена этой ошибки названа: гейт был красен НА САМОМ СТВОЛЕ линии, `make
// test-unit` вместе с ним, и хук отправки отказывал всякой ветке. То есть
// проверка, заведённая против «зелёного ни на чём», сама производила красное
// ни на чём — и обесценивала локальный прогон для всех сразу.
//
// # Производитель узнаётся ИМЕНЕМ ПАКЕТА, а не одним лишь именем функции
//
// `UpsertTx` в дереве не одна: одноимённую экспортирует
// `services/iam/internal/repo/kacho/pg/target_members`. Предикат «встретилось
// слово UpsertTx» признал бы производителем вызов, который к рёбрам отношения
// не имеет, — то есть зеленел бы на фикстуре, ничего не посеявшей. Поэтому
// селектор обязан ссылаться на импорт ИМЕННО пакета-производителя.
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
//
// Способность упасть и смолчать доказана инъекцией в обе стороны — см.
// `TestInjection_Census*` ниже.

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

	// Пакет-производитель назван ДВАЖДЫ, и это не дубль: у двух величин разные
	// предметы, и они разошлись ровно там, где прежняя редакция считала их одним.
	//
	//   edgeProducerDir    — КАТАЛОГ в дереве, от корня монорепо;
	//   edgeProducerImport — ХВОСТ пути ИМПОРТА.
	//
	// Пока служба лежала в единственном модуле дерева, хвост импорта совпадал с
	// каталогом, и одной константы хватало. Служба получила свой модуль
	// (`github.com/PRO-Robotech/kacho-iam`), и сегмент `services/iam` из пути
	// импорта пропал: хвост перестал совпадать с каталогом. Прежний комментарий
	// обещал устойчивость к переименованию модуля — обещание было верно
	// наполовину, потому что величина несла оба смысла сразу.
	edgeProducerDir    = "services/iam/internal/repo/kacho/pg/resource_mirror"
	edgeProducerImport = "kacho-iam/internal/repo/kacho/pg/resource_mirror"
)

// censusFileFacts — то, что гейт узнаёт об одном файле.
type censusFileFacts struct {
	rel      string // путь от корня дерева — координата отказа
	census   bool   // содержит перепись покрытия (один стейтмент про обе таблицы)
	rawWrite bool   // сам кладёт рёбра прямой записью
	producer bool   // зовёт <пакет-производитель>.UpsertTx
}

// producerPackageName возвращает имя пакета-производителя и заодно проверяет
// ПРЕДПОСЫЛКУ гейта: что такой пакет есть и что функция с искомым именем в нём
// объявлена.
//
// Без этой проверки переименование производителя (или его переезд) не сделало
// бы гейт красным — он тихо перестал бы узнавать вызовы и объявил находкой
// каждую пробу-перепись сразу. Ложное красное на всём дереве неотличимо от
// настоящего класса, и разбирают его дольше, чем оно того стоит.
func producerPackageName(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(edgeProducerDir))
	files, err := treecorpus.UnderWithSuffix(dir, ".go")
	if err != nil {
		t.Fatalf("пакет-производитель %s не прочитан (%v): предпосылка гейта не выполнена, "+
			"и его молчание ничего не значило бы", edgeProducerDir, err)
	}
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if perr != nil {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if ok && fn.Recv == nil && fn.Name.Name == edgeProducerCall {
				return f.Name.Name
			}
		}
	}
	t.Fatalf("в пакете %s нет функции %s: производитель переименован или переехал. "+
		"Гейт обязан упасть здесь ГРОМКО — иначе он перестал бы узнавать вызовы и "+
		"объявил бы находкой каждую пробу-перепись сразу", edgeProducerDir, edgeProducerCall)
	return ""
}

// censusFactsOf разбирает один файл. Разбор, а не текст: комментарий,
// объясняющий эту же дисциплину, производителем не является.
func censusFactsOf(t *testing.T, rel string, body []byte, producerPkg string) censusFileFacts {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}
	facts := censusFileFacts{rel: rel}

	// Имена, под которыми в ЭТОМ файле виден пакет-производитель. Их может не
	// быть вовсе — тогда никакой селектор производителем не считается.
	producerNames := map[string]bool{}
	for _, im := range file.Imports {
		p, uerr := strconv.Unquote(im.Path.Value)
		if uerr != nil || !strings.HasSuffix(p, edgeProducerImport) {
			continue
		}
		// Неалиасированный импорт виден под ИМЕНЕМ ПАКЕТА, а не под именем
		// каталога. Здесь они совпадают, но выводить одно из другого — догадка;
		// имя берётся у самого пакета (см. producerPackageName).
		name := producerPkg
		if im.Name != nil {
			name = im.Name.Name
		}
		producerNames[name] = true
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			v, uerr := strconv.Unquote(node.Value)
			if uerr != nil {
				return true
			}
			if strings.Contains(v, censusMirrorTable) && strings.Contains(v, censusEdgeTable) {
				facts.census = true
			}
			if strings.Contains(v, rawEdgeWriteMarker) {
				facts.rawWrite = true
			}
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != edgeProducerCall {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if ok && producerNames[id.Name] {
				facts.producer = true
			}
		}
		return true
	})
	return facts
}

// judgeCensusFixtures выносит вердикт по КАТАЛОГАМ: ключ — каталог тестового
// пакета, значение — факты его файлов.
//
// Отделено от обхода дерева намеренно: инъекция кормит эту функцию
// синтетическими фактами и доказывает, что она умеет и краснеть, и молчать.
func judgeCensusFixtures(byDir map[string][]censusFileFacts) []string {
	var findings []string
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, d := range dirs {
		files := byDir[d]
		producerInPackage := false
		for _, f := range files {
			if f.producer {
				producerInPackage = true
				break
			}
		}
		for _, f := range files {
			if !f.census {
				// Законный близнец: проба читателя. Спрашивает об ОДНОМ объекте,
				// поэтому её фикстура вправе класть рёбра прямо.
				continue
			}
			if f.rawWrite {
				findings = append(findings, f.rel+
					" — проба пересчитывает все строки зеркала и САМА кладёт рёбра прямой записью: "+
					"она утверждает свойство своей фикстуры и останется зелёной, даже если "+
					"производитель перестанет писать цепь целиком")
				continue
			}
			if !producerInPackage {
				findings = append(findings, f.rel+
					" — проба пересчитывает все строки зеркала, но НИ ОДИН файл её тестового "+
					"пакета не зовёт производителя ("+edgeProducerDir+"."+edgeProducerCall+
					"): непонятно, чьё свойство она утверждает")
			}
		}
	}
	sort.Strings(findings)
	return findings
}

// TestCensusFixturesSeedThroughTheProducer — фикстура пробы-переписи идёт через
// производителя, а не пишет предмет переписи сама.
func TestCensusFixturesSeedThroughTheProducer(t *testing.T) {
	root := repoRoot(t)
	producerPkg := producerPackageName(t, root)

	base := filepath.Join(root, "services", "iam")
	files, err := treecorpus.UnderWithSuffix(base, "_test.go")
	if err != nil {
		t.Fatalf("состав тестового дерева под %s: %v", base, err)
	}

	scanned, censusFiles, rawWriters := 0, 0, 0
	byDir := map[string][]censusFileFacts{}
	for _, p := range files {
		body, rerr := os.ReadFile(p) // #nosec G304 -- путь получен из индекса СВОЕГО дерева
		if rerr != nil {
			t.Fatalf("чтение %s: %v", p, rerr)
		}
		scanned++
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)

		// Файл, вовсе не называющий таблицу рёбер, ни переписью, ни её соседом по
		// пакету быть не может — разбирать его незачем.
		if !strings.Contains(string(body), censusEdgeTable) {
			continue
		}
		facts := censusFactsOf(t, rel, body, producerPkg)
		if facts.rawWrite {
			rawWriters++
		}
		if facts.census {
			censusFiles++
		}
		dir := path.Dir(rel)
		byDir[dir] = append(byDir[dir], facts)
	}

	// Объём осмотренного — отдельное утверждение.
	t.Logf("осмотрено тестовых файлов iam: %d; каталогов с приметой: %d; "+
		"из них проб-переписей: %d; проб с прямой записью рёбер (включая законных "+
		"читателей): %d; пакет-производитель: %s",
		scanned, len(byDir), censusFiles, rawWriters, producerPkg)
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

	if findings := judgeCensusFixtures(byDir); len(findings) > 0 {
		t.Fatalf("перепись утверждает свойство собственной фикстуры:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
