// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

// catalog_fact_symbols_exhaustive_test.go — РАСПОЗНАВАТЕЛЬ соседнего гейта
// обязан знать ВСЕ экспортированные функции, читающие литерал каталога
// (kacho#1816, приёмка
// `services/iam/docs/engineering/acceptance/catalog-readers-move-to-the-table.md`).
//
// # Предмет: невидимость, а не нарушение
//
// `TestIAMCT2_LiteralIsNotAReadSource` отбирает обращения по РУКОПИСНОМУ перечню
// имён (`catalogFactSymbols`). Перечень — второе место о предмете «что в пакете
// `authzmap` отвечает на каталожный вопрос», и держать его в согласии с самим
// пакетом не поручено ничему: функция, добавленная в `authzmap` и читающая тот же
// словарь, в перечень не попадает, и всё записанное через неё оказывается ВНЕ
// НАБЛЮДЕНИЯ. Это не красное и не зелёное — это молчание, и отличить его от
// чистого дерева нельзя ничем (`testing.md` §«Гейт на класс», п. 7).
//
// Замер, ради которого гейт написан: на ревизии заведения экспортированных
// функций пакета, транзитивно достающих до `objectTypes`/`typeVerbRelations`,
// было БОЛЬШЕ, чем перечислено обоими наборами соседнего гейта. Числа печатает
// перепись ниже — их не надо брать отсюда, они устаревают.
//
// # Что гейт требует и чего НЕ требует
//
// Требует: каждая экспортированная функция-читатель названа ровно одним из трёх
// наборов — запрещённый каталожный факт · переходник имени типа · читатель,
// осознанно оставленный вне предмета с ПРИЧИНОЙ. И: имя, названное набором,
// существует в пакете (иначе запись пережила свой предмет).
//
// НЕ требует, чтобы всякое имя из первых двух наборов было читателем: набор
// каталожного факта перечисляет то, что прод-коду СПРАШИВАТЬ У ЛИТЕРАЛА нельзя,
// и туда законно попадает функция, ставшая чистой. Третий набор — послабление, и
// у него требование обратное: запись без предмета есть находка.
//
// # Почему транзитивно
//
// `GrantedVerbs` литерала не касается сама — она зовёт `VerbsOfType`. Разбор,
// смотрящий только на тело, объявил бы её нечитателем, и перечень запрещённых
// разошёлся бы с разбором в первой же строке.
//
// # Почему производная переменная пакета читателем НЕ считается
//
// `expandableRelations` собирается из `typeVerbRelations` в инициализаторе
// переменной пакета. Приёмка (§0.2) называет её ОТДЕЛЬНЫМ литералом —
// поверхностью раскрытия, — и её читатели (`AcceptExpand`,
// `IsExpandableRelation`) предметом `#1816` не являются. Поэтому разбор считает
// чтением обращение к словарю ВНУТРИ ТЕЛА ФУНКЦИИ, а не в инициализаторе
// переменной: иначе гейт задним числом переклассифицировал бы решение, принятое
// приёмкой, и сделал бы это молча.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// catalogLiteralNames — словари-литералы каталога. Ровно те два, которые называет
// приёмка (§0.2): других объявлений формы «карта каталога» в пакете нет.
var catalogLiteralNames = map[string]bool{
	"objectTypes":       true,
	"typeVerbRelations": true,
}

// literalReadersOutOfScope — читатели литерала, ОСОЗНАННО оставленные вне
// предмета #1816, с причиной у каждого.
//
// Это послабление, и оно истекает само: запись, чья функция читателем быть
// перестала (или из пакета исчезла), — находка. Иначе слепая зона переживёт свой
// предмет и достанется следующему читателю, который положит в неё что угодно.
var literalReadersOutOfScope = map[string]string{
	"CatalogSeedResources": "ЛЕВАЯ СТОРОНА ПАРИТЕТА: значение, с которым страж старта и гейт " +
		"дерева сверяют живые строки. Читать литерал — её работа by construction, и запретить " +
		"это значило бы потребовать, чтобы паритет сверял строки сами с собой (§2.1, §6.4)",
	"CatalogSeedVerbs": "то же, глагольная половина посева каталога (§6.4)",
	"PermissionsCoveringType": "спрашивает ПОКРЫТИЕ образца разрешения по закрытой таблице " +
		"типов — ветвь с подстановочным знаком перебирает `objectTypes` внутри " +
		"`permissionCoversType`, — а не «какие глаголы объявлены». Классификация СПОРНА и " +
		"заведена задачей kacho#1874: приёмка §0.2 относит эту функцию к нечитателям, и это " +
		"утверждение неверно — его и обнажил настоящий гейт. Пока решение не принято, запись " +
		"есть послабление с названной причиной, а не вывод",
}

// readerCensus — объём осмотренного. Печатается всегда и независимо от исхода:
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
type readerCensus struct {
	Files      int
	Funcs      int
	Exported   int
	Readers    int
	Classified int
}

// authzmapLiteralReaders — экспортированные функции пакета, ТРАНЗИТИВНО достающие
// до словаря-литерала каталога, отсортированно.
//
// Состав приходит ПАРАМЕТРОМ по той же причине, что и у соседнего гейта: в живом
// дереве его даёт индекс git, а инъекция подаёт синтетический перечень —
// доказательство, требующее испортить живое дерево, в конвейере не исполняется
// никогда.
func authzmapLiteralReaders(files []string) (readers []string, exported map[string]bool, c readerCensus, err error) {
	type fn struct {
		reads   bool
		callees []string
	}
	funcs := map[string]*fn{}
	exported = map[string]bool{}

	fset := token.NewFileSet()
	for _, path := range files {
		name := filepath.Base(path)
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil, nil, c, fmt.Errorf("разобрать %s: %w", path, perr)
		}
		c.Files++
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			c.Funcs++
			f := &fn{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.Ident:
					if catalogLiteralNames[node.Name] {
						f.reads = true
					}
				case *ast.CallExpr:
					if id, isIdent := node.Fun.(*ast.Ident); isIdent {
						f.callees = append(f.callees, id.Name)
					}
				}
				return true
			})
			funcs[fd.Name.Name] = f
			if isExportedName(fd.Name.Name) {
				c.Exported++
				exported[fd.Name.Name] = true
			}
		}
	}

	// Неподвижная точка: чтение поднимается по вызовам ВНУТРИ пакета.
	for changed := true; changed; {
		changed = false
		for _, f := range funcs {
			if f.reads {
				continue
			}
			for _, callee := range f.callees {
				if target, ok := funcs[callee]; ok && target.reads {
					f.reads = true
					changed = true
					break
				}
			}
		}
	}

	for name, f := range funcs {
		if f.reads && isExportedName(name) {
			readers = append(readers, name)
		}
	}
	sort.Strings(readers)
	c.Readers = len(readers)
	return readers, exported, c, nil
}

// isExportedName — имя, видимое вне пакета.
func isExportedName(name string) bool {
	if name == "" {
		return false
	}
	return unicode.IsUpper([]rune(name)[0])
}

// TestIAMCT2_CatalogFactRecognizerKnowsEveryLiteralReader — распознаватель
// соседнего гейта полон относительно ПАКЕТА, а не относительно памяти автора.
func TestIAMCT2_CatalogFactRecognizerKnowsEveryLiteralReader(t *testing.T) {
	root := catalogRepoRoot(t)
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, literalPackageRel), ".go")
	if err != nil {
		t.Fatalf("состав пакета-литерала: %v", err)
	}
	readers, exported, c, err := authzmapLiteralReaders(files)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	classified := classifiedLiteralSymbols()
	c.Classified = len(classified)
	t.Logf("осмотрено файлов: %d; функций верхнего уровня: %d (экспортированных %d); "+
		"читателей литерала: %d; классифицировано имён: %d",
		c.Files, c.Funcs, c.Exported, c.Readers, c.Classified)

	if c.Files == 0 || c.Funcs == 0 {
		t.Fatalf("в %s прочитано файлов %d, функций %d — вердикт беспредметен",
			literalPackageRel, c.Files, c.Funcs)
	}
	if c.Readers == 0 {
		t.Fatalf("читателей литерала распознано НОЛЬ при %d экспортированных функциях — "+
			"это отказ РАЗБОРА, а не пакет без литерала: словари %v объявлены в нём и читаются",
			c.Exported, sortedKeys(catalogLiteralNames))
	}

	for _, r := range readers {
		if classified[r] {
			continue
		}
		t.Errorf("экспортированная функция authzmap.%s читает литерал каталога и НЕ НАЗВАНА "+
			"ни одним набором распознавателя.\n\n"+
			"Прод-файл, позвавший её, гейт TestIAMCT2_LiteralIsNotAReadSource не увидит — "+
			"это не нарушение и не чистота, а НЕВИДИМОСТЬ. Отнесите имя к одному из трёх:\n"+
			"  catalogFactSymbols        — каталожный факт, прод-коду спрашивать у литерала нельзя;\n"+
			"  typeDictionarySymbols     — переходник имени типа, остаётся на литерале (§2.2);\n"+
			"  literalReadersOutOfScope  — вне предмета #1816, с ПРИЧИНОЙ у записи.", r)
	}

	// Самоистечение: имя, названное набором, обязано существовать в пакете.
	for _, name := range sortedKeys(classified) {
		if !exported[name] {
			t.Errorf("набор распознавателя называет authzmap.%s, а такой экспортированной функции "+
				"в пакете нет — запись пережила свой предмет и молча сужает наблюдение", name)
		}
	}

	// Послаблению — обратное требование: запись без предмета есть находка.
	readerSet := map[string]bool{}
	for _, r := range readers {
		readerSet[r] = true
	}
	for _, name := range sortedKeys(literalReadersOutOfScope) {
		if !readerSet[name] {
			t.Errorf("послабление literalReadersOutOfScope[%q] больше нечего исключать: "+
				"функция литерал не читает. Снимите запись — послабление без предмета "+
				"становится слепой зоной, заведённой вперёд", name)
		}
	}
}

// classifiedLiteralSymbols — объединение трёх наборов распознавателя.
func classifiedLiteralSymbols() map[string]bool {
	out := map[string]bool{}
	for s := range catalogFactSymbols {
		out[s] = true
	}
	for _, s := range typeDictionarySymbols {
		out[s] = true
	}
	for s := range literalReadersOutOfScope {
		out[s] = true
	}
	return out
}

// sortedKeys — детерминированный порядок вывода находок.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestIAMCT2_CatalogFactRecognizerInjection — доказательство способности упасть,
// в ОБЕ стороны и по каждой оси отдельно.
//
// Инъекция подаётся синтетическим пакетом: портить живое дерево ради
// доказательства нельзя, а утверждение, чью способность падать не показали, от
// вакуумного неотличимо.
//
// Прогонов на ось ТРИ, а не два: контроль (всё цело — молчат оба свойства),
// инъекция НОВОГО свойства (краснеет только оно) и инъекция транзитивности
// (разбор, смотрящий только на тело, промолчал бы). Иначе красное могло бы
// прийти от соседнего утверждения, и вакуумность нового осталась бы непоказанной.
func TestIAMCT2_CatalogFactRecognizerInjection(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		classified  map[string]bool
		wantReaders []string
		wantUnknown []string
	}{
		{
			name: "контроль: единственный читатель назван набором",
			body: "package authzmap\n" +
				"var objectTypes = map[string]string{}\n" +
				"func ObjectType(k string) string { return objectTypes[k] }\n",
			classified:  map[string]bool{"ObjectType": true},
			wantReaders: []string{"ObjectType"},
		},
		{
			name: "инъекция: новый экспортированный читатель не назван",
			body: "package authzmap\n" +
				"var typeVerbRelations = map[string][]string{}\n" +
				"func VerbsOfType(k string) []string { return typeVerbRelations[k] }\n" +
				"func VerbRelationsOfType(k string) []string { return typeVerbRelations[k] }\n",
			classified:  map[string]bool{"VerbsOfType": true},
			wantReaders: []string{"VerbRelationsOfType", "VerbsOfType"},
			wantUnknown: []string{"VerbRelationsOfType"},
		},
		{
			name: "инъекция: читатель ТРАНЗИТИВНЫЙ — тело литерала не называет",
			body: "package authzmap\n" +
				"var typeVerbRelations = map[string][]string{}\n" +
				"func verbs(k string) []string { return typeVerbRelations[k] }\n" +
				"func GrantedVerbs(k string) []string { return verbs(k) }\n",
			classified:  map[string]bool{},
			wantReaders: []string{"GrantedVerbs"},
			wantUnknown: []string{"GrantedVerbs"},
		},
		{
			name: "контроль: производная переменная пакета читателем НЕ делает",
			body: "package authzmap\n" +
				"var typeVerbRelations = map[string][]string{}\n" +
				"var expandableRelations = func() map[string]bool {\n" +
				"  m := map[string]bool{}\n" +
				"  for _, s := range typeVerbRelations { _ = s }\n" +
				"  return m\n" +
				"}()\n" +
				"func IsExpandableRelation(r string) bool { return expandableRelations[r] }\n" +
				"func ObjectType(k string) string { _ = typeVerbRelations; return k }\n",
			classified:  map[string]bool{"ObjectType": true},
			wantReaders: []string{"ObjectType"},
		},
		{
			name: "контроль: неэкспортированный читатель находкой не является",
			body: "package authzmap\n" +
				"var objectTypes = map[string]string{}\n" +
				"func lookup(k string) string { return objectTypes[k] }\n" +
				"func ObjectType(k string) string { return lookup(k) }\n",
			classified:  map[string]bool{"ObjectType": true},
			wantReaders: []string{"ObjectType"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "fga_types.go")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatalf("записать синтетику: %v", err)
			}
			readers, exported, c, err := authzmapLiteralReaders([]string{path})
			if err != nil {
				t.Fatalf("разбор синтетики: %v", err)
			}
			if c.Files == 0 || c.Funcs == 0 {
				t.Fatalf("синтетика не прочитана (файлов %d, функций %d) — инъекция ничего "+
					"не доказывает", c.Files, c.Funcs)
			}
			if strings.Join(readers, ",") != strings.Join(tc.wantReaders, ",") {
				t.Fatalf("читатели: получено %v, ожидалось %v", readers, tc.wantReaders)
			}
			var unknown []string
			for _, r := range readers {
				if !tc.classified[r] {
					unknown = append(unknown, r)
				}
			}
			if strings.Join(unknown, ",") != strings.Join(tc.wantUnknown, ",") {
				t.Fatalf("нераспознанные: получено %v, ожидалось %v", unknown, tc.wantUnknown)
			}
			for name := range tc.classified {
				if !exported[name] {
					t.Fatalf("самоистечение: %q объявлено классифицированным, но экспортированной "+
						"функцией пакета не является", name)
				}
			}
		})
	}
}
