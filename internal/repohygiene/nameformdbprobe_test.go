// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// nameformdbprobe_test.go — ограничение формы имени, поставленное миграцией
// сервиса, обязано быть ДОКАЗАНО вставкой в живую базу.
//
// # Предмет
//
// «Миграция применилась» и «ограничение отвергает негодную строку» — разные
// утверждения. Первое видно по дереву, второе — только прогоном. Задача #721
// пришла из состояния, где форму имени ставили ПЯТЬ сервисов, а её действие
// доказывал ОДИН: расхождение накопилось молча и заметить его было нечем, потому
// что оба перечня жили в разных местах и никто их не сверял.
//
// Ограничение базы — последний рубеж канона: код может смениться, вызывающий
// может пойти мимо слоя домена, но оператор базы отвергнет негодную строку
// всегда. Незадоказанное ограничение выглядит ровно так же, как действующее.
//
// # Что гейт требует
//
// Сервис, чья миграция объявляет канон формы, обязан нести ИСПОЛНЯЕМЫЙ вызов
// общего двигателя `pkg/nameformdb`. Гейт про НАЛИЧИЕ доказательства; его
// СОДЕРЖАНИЕ (перечень таблиц, отказ именно от формы, положительный контроль)
// держит сам двигатель, а его способность упасть — инъекция у geo.
//
// # Что значит «исполняемый» и почему это не поиск подстроки
//
// Доказательством считается вызов входного метода двигателя на значении типа
// `Probe`, стоящий в функции, достижимой от точки входа проб того же пакета.
// Прежняя редакция искала подстроку `nameformdb.Probe` в сыром тексте и
// зеленела на пробе, выпотрошенной до комментария `// Здесь когда-то звался
// nameformdb.Probe` (найдено рецензентом 2026-08-19). Комментарий вызовом не
// является; после разбора синтаксиса он в дерево не попадает вовсе.
//
// Перечень входных методов гейт ВЫВОДИТ из двигателя (экспортированные методы
// на `Probe`), а не выписывает: выписанный разошёлся бы с двигателем молча.
//
// # Почему гейт НЕ сужен до файла с «правильным» именем
//
// Засчитывается вызов из ЛЮБОГО файла проб сервиса, и это решение, а не
// недосмотр. Сужение по имени файла мерило бы соглашение об именовании, а не
// предмет: проба, переименованная или перенесённая в соседний пакет, доказывает
// ровно то же. Сужение сделано по свойству, которое действительно отличает
// доказательство от его вида, — по ИСПОЛНЯЕМОСТИ: вызов вне достижимости от
// точки входа прогона не доказывает ничего, будь он хоть в файле с самым верным
// именем.
//
// # Почему форма читается из дерева, а не выписана сюда
//
// Выписанная копия канона — ровно то, что запрещает соседний гейт
// TestResourceNameFormIsDeclaredOnce, и она разошлась бы с каноном первой.
// Поэтому форма приезжает параметром из единственного объявления
// (`pkg/validate/nameform`), а гейт отказывается судить, если прочитать её не
// удалось: «канона не нашли» обязано быть отказом, а не тихим «находок нет».
//
// # Перепись
//
// Печатается всегда: прочитано миграций, прочитано файлов проб, из них
// разобрано, сервисов под формой, сервисов с доказательством. Пустой обход —
// провал.
package repohygiene

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
)

// nameFormEnginePkgDir — дом двигателя. Гейт читает его, чтобы узнать входные
// методы; отсутствие каталога — отказ судить, а не тихий пропуск.
const nameFormEnginePkgDir = "pkg/nameformdb"

func TestNameFormConstraintIsProvenWhereItIsDeclared(t *testing.T) {
	tt := newTrackedTree(t, repoRoot(t))
	canonPattern := readCanonPattern(t, tt.root)
	entryMethods := readNameFormEngineEntries(t, tt.root)

	files := readNameFormDBGateCorpus(t, tt)
	cov := analyseNameFormDBCoverage(files, canonPattern, entryMethods)

	// Предпосылка. Гейт обоснован тем, что в дереве ЕСТЬ миграции и ЕСТЬ файлы
	// проб. Пустой обход обязан быть отказом: молчание на нём означало бы
	// «ничего не прочитано», а выглядело бы как «нарушений нет».
	if cov.MigrationsRead == 0 || cov.TestsRead == 0 {
		t.Fatalf("обход прочитал миграций %d, файлов проб %d — гейту нечего рассматривать; "+
			"молчаливый зелёный здесь означал бы «проверено»", cov.MigrationsRead, cov.TestsRead)
	}
	if len(cov.Constrained) == 0 {
		t.Fatalf("ни одна миграция дерева не объявляет форму имени (искали форму из %s) — "+
			"предпосылка гейта не выполняется, его молчание ничего не значит", canonNameFormPkgDir)
	}
	if len(cov.Unparsed) > 0 {
		t.Fatalf("не разобраны файлы проб: %s — гейт обязан отказаться судить дерево, "+
			"часть которого он не прочитал: «не разобрали» не может молча означать «вызова нет»",
			strings.Join(cov.Unparsed, ", "))
	}

	// Перепись печатается ДО находок: при отказе ниже она уже в логе, и «ноль
	// находок» остаётся отличимо от «ноль прочитанного».
	t.Logf("прочитано миграций %d, файлов проб %d (разобрано %d); входные методы двигателя: %v; "+
		"форму ставят сервисы: %v; исполняемый вызов несут: %v; файлов с вызовом %d "+
		"при %d упоминающих текстом",
		cov.MigrationsRead, cov.TestsRead, cov.TestsParsed, sortedEntryMethods(entryMethods),
		cov.Services(), probedServices(cov), cov.ProofFiles(), cov.MentionFiles())

	for _, svc := range cov.Unproven() {
		// Упоминание текстом называется отдельно: оно отличает выпотрошенную
		// пробу («вызов был, остался комментарий») от пробы, которой не было
		// никогда, — и посылает читателя восстанавливать, а не писать заново.
		mention := "имени двигателя нет ни в одном файле проб сервиса"
		if m := cov.Mentioned[svc]; len(m) > 0 {
			mention = fmt.Sprintf("имя двигателя ВСТРЕЧАЕТСЯ текстом в %s, но исполняемым вызовом "+
				"там не является (комментарий, строка, мёртвый помощник)", strings.Join(m, ", "))
		}
		t.Errorf("сервис %s ставит форму имени миграцией (%s), но действие ограничения "+
			"не доказано: ни один файл `services/%s/**/*_test.go` не несёт вызова %v на значении "+
			"`%s`, достижимого от точки входа прогона. %s. «Миграция применилась» и "+
			"«ограничение отвергает негодную строку» — разные утверждения, и проверяется только первое",
			svc, strings.Join(cov.Constrained[svc], ", "), svc,
			sortedEntryMethods(entryMethods), nameFormProbeMention, mention)
	}
}

func probedServices(cov nameFormDBCoverage) []string {
	out := make([]string, 0, len(cov.Probed))
	for svc := range cov.Probed {
		out = append(out, svc)
	}
	sort.Strings(out)
	return out
}

func sortedEntryMethods(entry map[string]bool) []string {
	out := make([]string, 0, len(entry))
	for m := range entry {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// readNameFormEngineEntries — входные методы двигателя, прочитанные У ДВИГАТЕЛЯ.
//
// Выписанный перечень («Run, Check») разошёлся бы с ним молча: переименовали бы
// метод — и гейт перестал бы видеть все пробы разом, объявив дерево сломанным
// там, где оно исправно. Пустой перечень — отказ судить: гейт, не знающий, что
// считать вызовом, не вправе молчать.
func readNameFormEngineEntries(t *testing.T, root string) map[string]bool {
	t.Helper()

	dir := filepath.Join(root, filepath.FromSlash(nameFormEnginePkgDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("чтение %s: %v — гейт судит по входным методам двигателя, а двигателя нет",
			nameFormEnginePkgDir, err)
	}

	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		af, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("%s/%s: разбор: %v", nameFormEnginePkgDir, e.Name(), perr)
		}
		for _, d := range af.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || !fn.Name.IsExported() {
				continue
			}
			if nameFormReceiverTypeName(fn.Recv.List[0].Type) == nameFormEngineType {
				out[fn.Name.Name] = true
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("у типа %s.%s не нашлось ни одного экспортированного метода — "+
			"гейту нечего считать вызовом, и его молчание ничего не значило бы",
			nameFormEnginePkgDir, nameFormEngineType)
	}
	return out
}

// nameFormReceiverTypeName — имя типа получателя без указателя и параметров типа.
func nameFormReceiverTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return nameFormReceiverTypeName(t.X)
	case *ast.IndexExpr:
		return nameFormReceiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// readNameFormDBGateCorpus читает то и только то, что гейт судит: миграции
// сервисов и файлы их проб.
func readNameFormDBGateCorpus(t *testing.T, tt *trackedTree) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, rel := range tt.SortedFiles() {
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "services/") {
			continue
		}
		isMigration := strings.HasSuffix(rel, ".sql") && strings.Contains(rel, "/internal/migrations/")
		if !isMigration && !strings.HasSuffix(rel, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tt.root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: чтение: %v — гейт обязан отказаться судить дерево, которое не смог "+
				"прочитать целиком", rel, err)
		}
		out[rel] = string(body)
	}
	return out
}
