// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
)

// serviceRoot — корень сервиса относительно этого пакета.
const serviceRoot = "../.."

// Позиции аргументов эмиссии `Outbox().Emit(ctx, вид, id, якорь, род, нагрузка)`.
const (
	argKind    = 1
	argAnchor  = 3
	argChange  = 4
	argMinimum = 6
)

// emission — одно место, где рождается строка журнала.
type emission struct {
	pos    string
	kind   string // пусто — вид задан не литералом
	change string // пусто — род задан не литералом
	anchor string // исходный текст аргумента якоря
}

// goEmissions — перепись эмиссий в КОДЕ.
//
// Обход идёт по всему дереву сервиса, а не по одному файлу: у vpc места эмиссии
// разнесены по use-case'ам (их полсотни), и перечень, выписанный руками,
// разошёлся бы с деревом на первом же новом ресурсе — молча.
func goEmissions(t *testing.T) []emission {
	t.Helper()
	var out []emission
	root, err := filepath.Abs(serviceRoot)
	if err != nil {
		t.Fatalf("корень сервиса не разрешился: %v", err)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Emit" {
				return true
			}
			// Ровно эмиссия РЕСУРСНОГО журнала: получатель — `…Outbox()`.
			inner, ok := sel.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			isel, ok := inner.Fun.(*ast.SelectorExpr)
			if !ok || isel.Sel.Name != "Outbox" {
				return true
			}
			e := emission{pos: fset.Position(call.Pos()).String()}
			if len(call.Args) < argMinimum {
				t.Errorf("%s: эмиссия с %d аргументами — позиции уехали, и разбор судит не то",
					e.pos, len(call.Args))
				return true
			}
			e.kind = literal(call.Args[argKind])
			e.change = literal(call.Args[argChange])
			e.anchor = exprText(call.Args[argAnchor])
			out = append(out, e)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева сервиса не удался: %v", err)
	}
	return out
}

func literal(e ast.Expr) string {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return s
}

func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Value
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	case *ast.CallExpr:
		return exprText(v.Fun) + "(…)"
	}
	return "?"
}

// sqlTriggerEmission — перепись эмиссий, которые рождает САМА БАЗА.
//
// Их существование — не деталь vpc, а причина, по которой перепись не может
// ограничиться кодом: триггер `subnets_outbox_emit_route_table_change` вставляет
// строку журнала при авто-привязке таблицы маршрутов, и его слова обязаны стоять
// в словаре наравне со словами кода. Читай перепись эта только Go, целый
// производитель остался бы вне наблюдения — не нарушением, а невидимостью.
var sqlInsert = regexp.MustCompile(
	`(?is)INSERT\s+INTO\s+\S*vpc_outbox\s*\([^)]*\)\s*VALUES\s*\(\s*'([^']+)'\s*,[^,]+,(?:[^,]+,)?\s*'([^']+)'`)

func sqlEmissions(t *testing.T) []emission {
	t.Helper()
	dir := filepath.Join(serviceRoot, "internal", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог миграций не прочитан: %v", err)
	}
	var out []emission
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("миграция %s не прочитана: %v", e.Name(), rerr)
		}
		for _, m := range sqlInsert.FindAllStringSubmatch(string(b), -1) {
			out = append(out, emission{pos: e.Name(), kind: m[1], change: m[2]})
		}
	}
	return out
}

// TestChangeDictionaryIsDerivedFromBothProducers — словарь родов изменения
// сверяется с ПРОИЗВОДИТЕЛЯМИ, а не со вторым рукописным перечнем.
//
// У журнала vpc нет ограничения базы на это поле, значит единственные
// производители слов — код и триггер, и сверять надо с ними обоими. Проба,
// выписывающая слова второй раз, закрепляет ОТВЕТ словаря, а не его согласие с
// деревом: слово, заменённое на горячем пути на необъявленное, такой пробой не
// ловится ничем — ни здесь, ни на настоящей базе, где строка просто перестаёт
// доставляться, тихо.
//
// Утверждаются ОБЕ стороны:
//
//	каждое слово производителя названо словарём — иначе строка недоставляема;
//	каждое слово словаря имеет производителя    — иначе запись переживёт свой
//	                                              предмет и будет читаться как
//	                                              способность журнала.
func TestChangeDictionaryIsDerivedFromBothProducers(t *testing.T) {
	all := append(goEmissions(t), sqlEmissions(t)...)
	if len(all) == 0 {
		t.Fatal("не найдено ни одной эмиссии — разбор сломан, и «расхождений нет» получено даром")
	}

	produced := map[string]int{}
	where := map[string][]string{}
	for _, e := range all {
		if e.change == "" {
			t.Errorf("%s: род изменения задан не строковым литералом — перепись его не увидит, "+
				"и слово окажется вне наблюдения", e.pos)
			continue
		}
		produced[e.change]++
		where[e.change] = append(where[e.change], e.pos)
	}
	if len(produced) == 0 {
		t.Fatalf("эмиссий %d, а слов ноль — разбор аргументов сломан", len(all))
	}

	declared := Journal().Mapping.Changes
	for word := range produced {
		if declared[word] == subscriptionv1.SubscriptionEvent_CHANGE_UNSPECIFIED {
			// Координаты названы ВСЕ: находка, называющая только слово, посылает
			// читателя искать его по полусотне мест эмиссии.
			t.Errorf("производитель пишет род %q, а словарь его НЕ называет: строка с ним "+
				"недоставляема, и потеря эта тихая — ни отказа, ни пропуска в нумерации.\n"+
				"  места (%d): %s", word, len(where[word]), strings.Join(where[word], "\n           "))
		}
	}
	for word := range declared {
		if produced[word] == 0 {
			t.Errorf("словарь называет род %q, которого не пишет НИ ОДИН производитель: "+
				"запись пережила свой предмет и читается как способность журнала", word)
		}
	}
	t.Logf("перепись: эмиссий %d (кода %d, базы %d); слов различных %d: %v; объявлено словарём %d",
		len(all), len(goEmissions(t)), len(sqlEmissions(t)), len(produced), sortedKeys(produced), len(declared))
}

// TestEveryDeliverableKindIsDeclaredAndEveryDeclaredKindIsProduced — словарь видов
// сверяется с деревом в обе стороны.
//
// Вид, который производится, но не объявлен, недоставляем: авторизовать его
// нечем. Вид, объявленный без производителя, — запись, пережившая свой предмет.
//
// Обе стороны, кроме НАЗВАННОГО исключения: `AddressPool` и
// `AddressPoolNetworkDefault` производятся и в словарь не входят НАМЕРЕННО —
// это админские предметы уровня кластера, у которых нет ни проектного измерения,
// ни типа объекта в модели прав, поэтому вопрос о видимости строки задать нечем.
// Исключение стоит здесь ПОИМЁННО, а не молчаливым пропуском: перечень, которому
// нечего исключать, обязан краснеть.
func TestEveryDeliverableKindIsDeclaredAndEveryDeclaredKindIsProduced(t *testing.T) {
	// Предметы без проектного измерения и без типа в модели прав.
	infraOnly := map[string]bool{
		"AddressPool":               true,
		"AddressPoolNetworkDefault": true,
	}

	all := append(goEmissions(t), sqlEmissions(t)...)
	produced := map[string]int{}
	kindWhere := map[string][]string{}
	for _, e := range all {
		if e.kind == "" {
			t.Errorf("%s: вид предмета задан не строковым литералом — перепись его не увидит", e.pos)
			continue
		}
		produced[e.kind]++
		kindWhere[e.kind] = append(kindWhere[e.kind], e.pos)
	}

	declared := Journal().Mapping.Kinds
	for kind := range produced {
		if infraOnly[kind] {
			continue
		}
		if _, ok := declared[kind]; !ok {
			t.Errorf("производится вид %q, а словарь его НЕ называет: строка недоставляема, "+
				"потому что авторизовать её нечем. Если вид админский — назови его в перечне "+
				"исключений этой пробы, а не оставляй молчаливым пропуском.\n"+
				"  места (%d): %s", kind, len(kindWhere[kind]),
				strings.Join(kindWhere[kind], "\n           "))
		}
	}
	for kind := range declared {
		if produced[kind] == 0 {
			t.Errorf("словарь называет вид %q, которого не производит НИ ОДИН производитель: "+
				"подписка на него открывается и молчит вечно", kind)
		}
	}
	for kind := range infraOnly {
		if produced[kind] == 0 {
			t.Errorf("исключению %q больше нечего исключать: вид не производится ни одним "+
				"производителем. Снимите запись — иначе следующая слепая зона унаследует её", kind)
		}
		if _, ok := declared[kind]; ok {
			t.Errorf("вид %q объявлен и словарём, и перечнем исключений: два решения об одном "+
				"предмете, из которых верно одно", kind)
		}
	}
	t.Logf("перепись: видов производится %d %v; объявлено словарём %d; исключено намеренно %d",
		len(produced), sortedKeys(produced), len(declared), len(infraOnly))
}

// TestEveryDeliverableEmissionCarriesAProjectAnchor — якорь проекта стоит на
// КАЖДОМ месте эмиссии доставляемого вида.
//
// Это и есть защита от того, ради чего якорь заведён. Пропустить аргумент нельзя
// — не соберётся; но можно передать пустую строку, и тогда событие тихо не
// покажется подписчику с осью проекта. Проба требует, чтобы у доставляемого вида
// якорь был ВЫРАЖЕНИЕМ, а не пустым литералом и не именованным «якоря нет».
//
// Обратная сторона утверждается тут же: у админских видов якоря быть НЕ должно, и
// стоять там обязано именно ИМЯ отсутствия, а не безымянный литерал — иначе
// пропуск и решение неразличимы на чтение.
func TestEveryDeliverableEmissionCarriesAProjectAnchor(t *testing.T) {
	const absent = "helpers.NoProjectAnchor"
	infraOnly := map[string]bool{"AddressPool": true, "AddressPoolNetworkDefault": true}

	emissions := goEmissions(t)
	if len(emissions) == 0 {
		t.Fatal("эмиссий не найдено — разбор сломан")
	}
	declared := Journal().Mapping.Kinds

	anchored, absent0 := 0, 0
	for _, e := range emissions {
		switch {
		case infraOnly[e.kind]:
			if e.anchor != absent {
				t.Errorf("%s: вид %q проектного измерения не имеет, но якорь задан как %s. "+
					"Отсутствие якоря обязано быть НАЗВАНО (%s), иначе решение неотличимо "+
					"от пропуска", e.pos, e.kind, e.anchor, absent)
			}
			absent0++
		default:
			if _, ok := declared[e.kind]; !ok {
				continue // вид вне словаря и вне исключений — предмет соседней пробы
			}
			if e.anchor == absent || e.anchor == `""` {
				t.Errorf("%s: вид %q доставляется подписчикам, а якорь проекта пуст (%s). "+
					"Событие не покажется подписчику с осью проекта — тихо, без отказа и "+
					"без пропуска в нумерации", e.pos, e.kind, e.anchor)
				continue
			}
			anchored++
		}
	}
	t.Logf("перепись: мест эмиссии %d; с якорем %d; отсутствие якоря названо %d",
		len(emissions), anchored, absent0)
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
