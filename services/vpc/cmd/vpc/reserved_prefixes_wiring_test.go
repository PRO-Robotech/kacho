// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// reserved_prefixes_wiring_test.go — гейт на КЛАСС: проверка, которой не передали
// того, относительно чего она судит.
//
// # Предмет
//
// Перечень адресных диапазонов, которые платформа держит за собой, приезжает в
// use-case'ы подсети ЯВНОЙ проводкой. Не переданный перечень — нулевое значение, а
// оно не пересекается ни с чем: проверка присутствует, исполняется на каждом
// создании подсети и НЕ ОТВЕРГАЕТ НИЧЕГО. Со стороны это неотличимо от работающей
// защиты: она не отказала ни разу за всю свою жизнь.
//
// Собственные пробы use-case'ов этого не замечают — они передают перечень сами.
// Страж старта тоже: он проверяет, что перечень ОБЪЯВЛЕН посадкой, а не что кто-то
// его прочитал. Между «объявлено» и «сверяется» лежит ровно эта строка проводки, и
// пропасть она может молча.
//
// # Что именно требуется
//
// Каждый глагол, которым диапазон подсети ОБЪЯВЛЯЕТСЯ, обязан получить перечень
// (`Create` и `:addCidrBlocks` — других таких глаголов у подсети нет: `Update`
// держит блоки неизменяемыми, `:removeCidrBlocks` только сужает). Плюс аргумент
// обязан быть ЧТЕНИЕМ НАСТРОЕК (`cfg.ReservedPrefixes()`), а не литералом в коде:
// перечень зависит от посадки, и вшитое значение описывало бы один стенд, оставаясь
// ложью про остальные.
//
// # Почему по синтаксическому дереву, а не по тексту
//
// Имя `WithReservedPrefixes` встречается в этом же файле в комментариях — и в
// комментарии рядом с самой проводкой. Текстовый поиск зеленел бы тем увереннее,
// чем лучше место задокументировано. Комментарии в дерево не входят.
//
// # Предпосылка проверяется
//
// Ноль найденных конструкторов — находка, а не «всё чисто»: значит разбор смотрит
// не туда (конструктор переименовали, проводку перенесли), и молчание гейта ничего
// не доказывает.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// subnetPkgSuffix — пакет use-case'ов подсети. Конструкторы опознаются по ПАКЕТУ,
// а не по имени метода: имя `NewAddCidrBlocksUseCase` носят ещё два соседа —
// супернет сети и диапазоны адресного пула, — и гейт, различающий их только по
// имени, требовал бы перечня от того, кому он не адресован. Именно это и произошло
// на первом прогоне: он нашёл двух чужих и назвал их непровязанными.
//
// Про сеть и пул решено отдельно и осознанно: супернет сети — адресный ПЛАН, из
// которого нарезаются подсети, и служебный диапазон внутри плана безвреден, пока
// подсеть поверх него отвергается; адресный пул — admin-only поверхность
// (Internal*, :9091), где объявляющий перечень и объявляющий пул — одно лицо.
// Появится потребность — это отдельное решение со своей приёмкой, а не расширение
// строки здесь.
const subnetPkgSuffix = "/services/vpc/internal/apps/kacho/api/subnet"

// subnetCidrDeclaringConstructors — конструкторы use-case'ов подсети, которым
// перечень обязателен, и зачем каждому.
//
// Перечень закрытый НАМЕРЕННО: он и есть ответ на вопрос «где диапазон подсети
// объявляется». Появится третий такой глагол — строка добавляется сюда, иначе
// «провязано» перестанет означать «провязано везде».
var subnetCidrDeclaringConstructors = map[string]string{
	"NewCreateSubnetUseCase": "создание подсети объявляет её диапазоны впервые",
	"NewAddCidrBlocksUseCase": "добавление диапазона — ВТОРОЕ и последнее место объявления; " +
		"без него обход занимает один дополнительный запрос (создать законным блоком, " +
		"добавить служебный)",
}

// wiringMethod — метод, которым перечень передаётся в use-case.
const wiringMethod = "WithReservedPrefixes"

// configReader — метод настроек, который единственно вправе быть источником
// значения (тот же, что читает страж старта).
const configReader = "ReservedPrefixes"

// constructorCall — вызов конструктора, найденный в композиционном корне.
type constructorCall struct {
	name  string
	where string
	wired bool
	// argProblem — непусто, если проводка есть, но аргумент не является чтением
	// настроек.
	argProblem string
}

// TestSubnetCidrUseCasesGetTheReservedPrefixesFromConfig — ядро гейта.
func TestSubnetCidrUseCasesGetTheReservedPrefixesFromConfig(t *testing.T) {
	fset := token.NewFileSet()
	files := compositionRootFiles(t, fset)

	// Псевдоним пакета подсети берётся ИЗ ИМПОРТОВ, а не выписывается: гейт
	// опознаёт конструктор по пути пакета, поэтому переименование псевдонима его не
	// обманывает, а исчезновение импорта — валит по предпосылке ниже.
	subnetAliases := importAliasesFor(files, subnetPkgSuffix)
	if len(subnetAliases) == 0 {
		t.Fatalf("композиционный корень не импортирует пакет %s — предпосылка гейта сломана "+
			"(пакет переехал или use-case'ы подсети больше не собираются здесь), его молчание "+
			"ничего не доказывает", subnetPkgSuffix)
	}

	// (1) Все вызовы интересующих конструкторов ИЗ ПАКЕТА ПОДСЕТИ.
	calls := map[*ast.CallExpr]*constructorCall{}
	// (2) Идентификаторы, которым присвоено чтение настроек: `x := cfg.ReservedPrefixes()`.
	configBackedIdents := map[string]bool{}

	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if pkg, name, ok := qualifiedCallName(call); ok && subnetAliases[pkg] {
					if _, interesting := subnetCidrDeclaringConstructors[name]; interesting {
						calls[call] = &constructorCall{
							name:  name,
							where: fset.Position(call.Pos()).String(),
						}
					}
				}
				return true
			}
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range assign.Rhs {
				call, ok := rhs.(*ast.CallExpr)
				if !ok {
					continue
				}
				if name, ok := selectorName(call); !ok || name != configReader {
					continue
				}
				if i < len(assign.Lhs) {
					if id, ok := assign.Lhs[i].(*ast.Ident); ok {
						configBackedIdents[id.Name] = true
					}
				}
			}
			return true
		})
	}

	// Предпосылка: конструкторы найдены. Ноль — сломанный разбор, а не чистота.
	if len(calls) == 0 {
		t.Fatalf("в композиционном корне не найдено ни одного вызова конструкторов %v — "+
			"предпосылка гейта сломана, его молчание ничего не доказывает",
			sortedKeys(subnetCidrDeclaringConstructors))
	}
	found := map[string]int{}
	for _, c := range calls {
		found[c.name]++
	}
	for name, why := range subnetCidrDeclaringConstructors {
		if found[name] == 0 {
			t.Fatalf("конструктор %s не вызывается композиционным корнем — либо его "+
				"переименовали (тогда чините гейт), либо use-case перестал собираться, и "+
				"проверять нечего (%s)", name, why)
		}
	}

	// (3) Проводка: разворачиваем цепочку `.WithReservedPrefixes(...)` до базового
	// вызова конструктора.
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := selectorName(call)
			if !ok || name != wiringMethod {
				return true
			}
			base := baseConstructorCall(call, calls)
			if base == nil {
				return true
			}
			base.wired = true
			base.argProblem = argProblem(call, configBackedIdents)
			return true
		})
	}

	var unwired, badArg []string
	for _, c := range calls {
		switch {
		case !c.wired:
			unwired = append(unwired, c.where+" ("+c.name+")")
		case c.argProblem != "":
			badArg = append(badArg, c.where+" ("+c.name+": "+c.argProblem+")")
		}
	}
	sort.Strings(unwired)
	sort.Strings(badArg)

	if len(unwired) > 0 {
		t.Fatalf("use-case(ы) %v собраны БЕЗ перечня служебных диапазонов: не переданный "+
			"перечень — нулевое значение, оно не пересекается ни с чем, и проверка на пути "+
			"запроса не отвергает ничего, оставаясь на вид работающей. Добавьте "+
			".%s(<чтение настроек>) в цепочку сборки", unwired, wiringMethod)
	}
	if len(badArg) > 0 {
		t.Fatalf("перечень служебных диапазонов передан НЕ из настроек: %v. Диапазоны зависят "+
			"от посадки, поэтому значение обязано приходить из cfg.%s() — вшитое в код "+
			"описывало бы один стенд и оставалось бы ложью про остальные", badArg, configReader)
	}

	t.Logf("перепись: прочитано файлов корня %d, вызовов конструкторов %d (%v), все провязаны "+
		"чтением настроек", len(files), len(calls), found)
}

// compositionRootFiles — разобранные прод-файлы композиционного корня.
func compositionRootFiles(t *testing.T, fset *token.FileSet) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("не прочитан каталог композиционного корня: %v", err)
	}
	var out []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, e.Name(), nil, 0)
		if perr != nil {
			// Неразобранный файл нельзя трактовать как «нарушений в нём нет».
			t.Fatalf("%s не разобран: %v", e.Name(), perr)
		}
		out = append(out, file)
	}
	if len(out) == 0 {
		t.Fatal("не прочитано ни одного файла композиционного корня")
	}
	return out
}

// selectorName — имя вызываемого метода/функции для `pkg.Name(...)` и
// `expr.Name(...)`.
func selectorName(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	return sel.Sel.Name, true
}

// qualifiedCallName — псевдоним пакета и имя функции для вызова вида `pkg.Name(...)`.
// Вызовы методов на выражении (`x.Foo().Bar()`) сюда не попадают: у них слева не
// идентификатор пакета.
func qualifiedCallName(call *ast.CallExpr) (pkg, name string, ok bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	return id.Name, sel.Sel.Name, true
}

// importAliasesFor — псевдонимы, под которыми файлы корня импортируют пакет с
// заданным суффиксом пути. Псевдоним читается из объявления импорта, а при его
// отсутствии — из последнего сегмента пути.
func importAliasesFor(files []*ast.File, pathSuffix string) map[string]bool {
	out := map[string]bool{}
	for _, file := range files {
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasSuffix(path, pathSuffix) {
				continue
			}
			if imp.Name != nil {
				out[imp.Name.Name] = true
				continue
			}
			segments := strings.Split(path, "/")
			out[segments[len(segments)-1]] = true
		}
	}
	return out
}

// baseConstructorCall разворачивает цепочку вызовов влево и возвращает вызов
// конструктора, к которому прицеплена проводка (nil — если это не наша цепочка).
func baseConstructorCall(call *ast.CallExpr, known map[*ast.CallExpr]*constructorCall) *constructorCall {
	expr := ast.Expr(call)
	for {
		switch e := expr.(type) {
		case *ast.CallExpr:
			if c, ok := known[e]; ok {
				return c
			}
			expr = e.Fun
		case *ast.SelectorExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		default:
			return nil
		}
	}
}

// argProblem — пусто, если аргумент проводки есть и он является чтением настроек.
func argProblem(call *ast.CallExpr, configBackedIdents map[string]bool) string {
	if len(call.Args) != 1 {
		return "ожидался ровно один аргумент"
	}
	switch arg := call.Args[0].(type) {
	case *ast.CallExpr:
		if name, ok := selectorName(arg); ok && name == configReader {
			return ""
		}
		return "аргумент — вызов, но не чтение настроек"
	case *ast.Ident:
		if configBackedIdents[arg.Name] {
			return ""
		}
		return "аргумент `" + arg.Name + "` не присвоен из чтения настроек в этом пакете"
	default:
		return "аргумент не является ни чтением настроек, ни переменной, ему присвоенной"
	}
}

// sortedKeys — детерминированный порядок имён для текста отказа.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
