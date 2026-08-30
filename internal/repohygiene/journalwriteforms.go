// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journalwriteforms.go — ПЕРЕПИСЬ ТОЧЕК ЗАПИСИ ЖУРНАЛА ПОДПИСКИ ПО ФОРМАМ.
//
// # Предмет
//
// Строку журнала подписки в этом дереве пишут НЕСКОЛЬКИМИ формами, и разбор,
// знающий одну из них, о прочих не говорит ни «есть», ни «нет» — он МОЛЧИТ.
// Записанное неизвестной формой не «разрешено», а НЕ ОСМОТРЕНО (`testing.md`
// §«Гейт на класс», п. 7).
//
// Класс сработал на самом ЗАМЕРЕ, которым задача обосновывалась (#1573):
// предикат по литеральному имени таблицы дал у машин ноль писателей журнала при
// живом писателе, потому что имя таблицы там приходит аргументом. Перепись,
// печатающая ОДНО число, читается как «столько точек у журнала», а означает
// «столько точек ОДНОЙ формы».
//
// # Формы ВЫВЕДЕНЫ ОБХОДОМ, а не выписаны
//
// Перечень собран классификацией КАЖДОЙ вставки дерева по форме выражения имени
// таблицы плюс двумя формами вызова, где имя не называется вовсе. Он не
// перечисляется здесь константами «на память»: у каждой формы есть свой
// распознаватель, и перепись печатает, сколько экземпляров ФОРМЫ он нашёл в
// дереве ВООБЩЕ — отдельно от того, сколько из них пишут журнал. Ноль журнальных
// точек при непустом распознавателе означает «искали и не нашли»; ноль у самого
// распознавателя означает, что он умер, и это отказ.
//
// # Что считается ТОЧКОЙ, а что ПЕРЕНОСОМ
//
// Точка, не называющая НИ ОДНОГО слова словами (ни вида, ни рода — всё пришло
// параметрами), решения не принимает: она исполняет чужое. Такая точка идёт в
// перепись ПЕРЕНОСОМ, и решение за неё принимает вызывающий, учтённый своей
// формой. Различение идёт ПО СУЩЕСТВУ (какие литералы стоят в самой точке), а не
// по месту: исключение «вот в этом файле можно» пережило бы свой предмет молча.
//
// # Границы названы, а не умолчаны
//
//  1. перепись считает ТОЧКИ ЗАПИСИ, а не точки решения. Обёртка сервиса,
//     зовущая общую библиотеку, — ОДНА точка, сколько бы вызывающих у неё ни
//     было; её вызывающие имени журнала не называют и потому этой переписи не
//     видны;
//  2. свой ПЕРЕНОС сервиса (функция, принимающая имя таблицы аргументом и
//     вставляющая в него) по вызывающим не разбирается — разбор вызывающих
//     сделан ровно для общей библиотеки `pkg/outbox`, чьи вызовы форма 2 и
//     считает. Величина переносов вне `pkg/` печатается ОТДЕЛЬНО, чтобы этот
//     край был виден числом;
//  3. форму триггера судить по ТЕКСТУ миграций нельзя решить: живое тело функции
//     — последнее из череды переопределений, а прежние лежат в ПРИМЕНЁННЫХ
//     миграциях, править которые нельзя (ban #5). Перепись поэтому называет
//     ОБЪЯВЛЕНИЯ по всем ревизиям — верхнюю границу, а не живой набор, — и
//     говорит это словами. Живое тело знает только база.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// JournalWriteForm — форма, которой в дереве записывается строка журнала.
type JournalWriteForm string

const (
	// JournalFormPortCall — вызов порта журнала (`…Outbox().Emit(ctx, …)`).
	// Имени таблицы не называет вовсе: владелец читается по каталогу сервиса.
	JournalFormPortCall JournalWriteForm = "вызовом порта"
	// JournalFormLibraryCall — вызов общей библиотеки
	// (`outbox.Emit`/`outbox.EmitAnchored`): имя таблицы стоит аргументом.
	JournalFormLibraryCall JournalWriteForm = "вызовом общей библиотеки"
	// JournalFormLiteralStatement — оператор SQL, имя таблицы литералом.
	JournalFormLiteralStatement JournalWriteForm = "оператором, имя таблицы литералом"
	// JournalFormSchemaFormatted — оператор SQL, СХЕМА подставляется
	// форматированием, имя таблицы литералом (`INSERT INTO %s.<имя>`).
	JournalFormSchemaFormatted JournalWriteForm = "оператором, схема форматируется"
	// JournalFormNameFormatted — оператор SQL, имя таблицы подставляется
	// форматированием ЦЕЛИКОМ (`INSERT INTO %s`).
	JournalFormNameFormatted JournalWriteForm = "оператором, имя таблицы форматируется целиком"
	// JournalFormTrigger — вставка в теле функции, объявленной миграцией.
	JournalFormTrigger JournalWriteForm = "триггером базы"
)

// JournalWriteForms — порядок печати переписи. Перечень существует ради
// ПОРЯДКА и ради того, чтобы форма без единой точки всё равно печаталась своей
// строкой: молчание о форме и есть тот дефект, который эта перепись закрывает.
var JournalWriteForms = []JournalWriteForm{
	JournalFormPortCall,
	JournalFormLibraryCall,
	JournalFormLiteralStatement,
	JournalFormSchemaFormatted,
	JournalFormNameFormatted,
	JournalFormTrigger,
}

// JournalOwner — владелец журнала подписки, выведенный из дерева.
type JournalOwner struct {
	Service string // имя каталога под `services/`
	Table   string // имя таблицы так, как оно объявлено
	Bare    string // то же без схемы
	Decl    string // координата объявления
}

// JournalWritePoint — одна точка записи строки журнала.
type JournalWritePoint struct {
	Pos      string
	Form     JournalWriteForm
	Service  string   // владелец, которому точка сопоставлена
	Literals []string // слова, названные в самой точке (вид, род, …)
}

// Transport — точка не назвала ни одного слова: решение принято вызывающим.
func (p JournalWritePoint) Transport() bool { return len(p.Literals) == 0 }

// JournalFormattedInsert — оператор вставки, чьё имя таблицы подставляется
// форматированием ЦЕЛИКОМ. Отдельный вид записи, потому что у него есть третий
// исход помимо «журнал» и «не журнал»: предмет может остаться НЕУСТАНОВЛЕННЫМ.
type JournalFormattedInsert struct {
	Pos string
	// Resolved — имя таблицы, до которого разбор дошёл; пусто, если не дошёл.
	Resolved string
	// Transport — имя таблицы пришло параметром или приёмником объемлющей
	// функции. Предмет такой вставки решает ВЫЗЫВАЮЩИЙ, и разбирается он своей
	// формой, а не здесь.
	Transport bool
	// InPkg — вставка лежит в общей библиотеке (`pkg/`). Переносы вне `pkg/`
	// печатаются отдельной величиной: их вызывающие этой переписи не видны.
	InPkg bool
	// svc, literals — то, что понадобится, когда имя таблицы разрешится: чей это
	// каталог и какие слова точка назвала сама. Хранится здесь, потому что
	// разрешение имени бывает ОТЛОЖЕННЫМ (значение константы дочитывает второй
	// проход), а точка обязана попасть в перепись владельца ровно так же, как
	// точка с литеральным именем.
	svc      string
	literals []string
}

// JournalWriteFormCensus — перепись целиком.
type JournalWriteFormCensus struct {
	Root      string
	GoFiles   int
	SQLFiles  int
	Owners    []JournalOwner
	Points    []JournalWritePoint
	Formatted []JournalFormattedInsert
	// Recognizer — сколько экземпляров формы распознаватель нашёл в дереве
	// ВООБЩЕ, включая вставки в НЕ-журнальные таблицы. Ноль здесь означает, что
	// распознаватель умер, и тогда его молчание о журнале ничего не утверждает.
	Recognizer map[JournalWriteForm]int
}

// PointsOf — точки владельца по форме.
func (c JournalWriteFormCensus) PointsOf(service string, form JournalWriteForm) []JournalWritePoint {
	var out []JournalWritePoint
	for _, p := range c.Points {
		if p.Service == service && p.Form == form {
			out = append(out, p)
		}
	}
	return out
}

// Unresolved — вставки с форматируемым именем, чей предмет не установлен.
func (c JournalWriteFormCensus) Unresolved() []JournalFormattedInsert {
	var out []JournalFormattedInsert
	for _, f := range c.Formatted {
		if f.Resolved == "" && !f.Transport {
			out = append(out, f)
		}
	}
	return out
}

// TransportsOutsideSharedLibrary — переносы, лежащие вне общей библиотеки. Их
// вызывающие переписью не разбираются; величина печатается, чтобы край был
// виден числом, а не подразумевался.
func (c JournalWriteFormCensus) TransportsOutsideSharedLibrary() []JournalFormattedInsert {
	var out []JournalFormattedInsert
	for _, f := range c.Formatted {
		if f.Transport && !f.InPkg {
			out = append(out, f)
		}
	}
	return out
}

// journalCorpusRoots — каталоги дерева, которые перепись читает. `pkg/` входит
// намеренно: терминальная вставка общей библиотеки лежит там, и без неё форма
// переноса не имела бы в дереве ни одного экземпляра — то есть её распознаватель
// был бы неотличим от мёртвого.
var journalCorpusRoots = []string{"services/", "pkg/", "gateway/"}

// CensusJournalWriteForms собирает перепись по СОСТАВУ дерева (`files` — пути от
// `root`, как их отдаёт индекс git либо обход синтетического дерева).
func CensusJournalWriteForms(root string, files map[string]bool) (JournalWriteFormCensus, error) {
	res := JournalWriteFormCensus{Root: root, Recognizer: map[JournalWriteForm]int{}}

	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	owners, err := discoverJournalOwners(root, rels)
	if err != nil {
		return res, err
	}
	res.Owners = owners

	fset := token.NewFileSet()
	// needConst — (каталог пакета → имена), чьё значение понадобилось разрешить.
	needConst := needSet{}
	type pendingFormatted struct {
		idx   int
		dir   string
		ident string
	}
	var pending []pendingFormatted

	for _, rel := range rels {
		switch {
		case strings.HasSuffix(rel, ".sql"):
			if !isMigrationPath(rel) {
				continue
			}
			body, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) // #nosec G304 -- путь из индекса git этого дерева
			if readErr != nil {
				return res, fmt.Errorf("%s: %w", rel, readErr)
			}
			res.SQLFiles++
			svc := serviceOf(rel)
			for _, ins := range insertTargets(string(body)) {
				res.Recognizer[JournalFormTrigger]++
				owner := ownerByTable(owners, svc, ins.table)
				if owner == "" {
					continue
				}
				res.Points = append(res.Points, JournalWritePoint{
					Pos:      fmt.Sprintf("%s:%d", rel, ins.line),
					Form:     JournalFormTrigger,
					Service:  owner,
					Literals: ins.literals,
				})
			}
		case strings.HasSuffix(rel, ".go"):
			if strings.HasSuffix(rel, "_test.go") || !underAnyRoot(rel, journalCorpusRoots) {
				continue
			}
			f, parseErr := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil,
				parser.SkipObjectResolution)
			if parseErr != nil {
				return res, fmt.Errorf("%s не разобран: %w — гейт судит по узлам, и "+
					"неосмотренный файл его молчания не оправдывает", rel, parseErr)
			}
			res.GoFiles++
			svc := serviceOf(rel)
			dir := path.Dir(rel)
			imports := fileImportNames(f)

			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				pos := func() string { return posOf(fset, rel, call.Pos()) }
				switch form := classifyJournalCall(call); form {
				case JournalFormPortCall:
					res.Recognizer[JournalFormPortCall]++
					if svc != "" && ownerService(owners, svc) {
						res.Points = append(res.Points, JournalWritePoint{
							Pos: pos(), Form: form, Service: svc,
							Literals: journalCallWords(journalEnclosingFunc(f, call.Pos()), imports, call.Args, -1),
						})
					}
				case JournalFormLibraryCall:
					res.Recognizer[JournalFormLibraryCall]++
					if len(call.Args) < 3 {
						return true
					}
					table, resolvable := resolveTableIdent(call.Args[2], f)
					if !resolvable {
						// Имя пришло не литералом и не константой пакета —
						// разрешать нечего: это вызов внутри самой библиотеки
						// либо переброс чужого аргумента.
						return true
					}
					if table == "" {
						if id, isIdent := call.Args[2].(*ast.Ident); isIdent {
							needConst.setNeed(dir, id.Name)
							pending = append(pending, pendingFormatted{
								idx: len(res.Points), dir: dir, ident: id.Name,
							})
							res.Points = append(res.Points, JournalWritePoint{
								Pos: pos(), Form: form, Service: "",
								Literals: journalCallWords(journalEnclosingFunc(f, call.Pos()), imports, call.Args, 2),
							})
						}
						return true
					}
					if owner := ownerByTable(owners, svc, table); owner != "" {
						res.Points = append(res.Points, JournalWritePoint{
							Pos: pos(), Form: form, Service: owner,
							Literals: journalCallWords(journalEnclosingFunc(f, call.Pos()), imports, call.Args, 2),
						})
					}
				}
				return true
			})

			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, uErr := strconv.Unquote(lit.Value)
				if uErr != nil {
					return true
				}
				for _, ins := range insertTargets(text) {
					pos := posOf(fset, rel, lit.Pos())
					if strings.HasPrefix(lit.Value, "`") && ins.line > 1 {
						// Сырой литерал переносы строк сохраняет, поэтому
						// смещение внутри него читается как строка файла.
						// Экранированный (`"…\n…"`) — нет, и там координатой
						// остаётся начало литерала.
						pos = fmt.Sprintf("%s:%d", rel, fset.Position(lit.Pos()).Line+ins.line-1)
					}
					switch ins.form {
					case JournalFormNameFormatted:
						res.Recognizer[JournalFormNameFormatted]++
						rec := JournalFormattedInsert{
							Pos:      pos,
							InPkg:    strings.HasPrefix(rel, "pkg/"),
							svc:      svc,
							literals: ins.literals,
						}
						expr := formatArgFor(f, lit, ins.verbIndex)
						switch {
						case expr == nil:
							// Разбор не нашёл вызова форматирования вокруг
							// литерала: предмет не установлен.
						case mentionsParamOrReceiver(f, lit, expr):
							rec.Transport = true
						default:
							name, resolvable := resolveTableIdent(expr, f)
							switch {
							case !resolvable:
							case name != "":
								rec.Resolved = name
							default:
								if id, isIdent := expr.(*ast.Ident); isIdent {
									needConst.setNeed(dir, id.Name)
									pending = append(pending, pendingFormatted{
										idx: -len(res.Formatted) - 1, dir: dir, ident: id.Name,
									})
								}
							}
						}
						res.Formatted = append(res.Formatted, rec)
					case JournalFormSchemaFormatted, JournalFormLiteralStatement:
						res.Recognizer[ins.form]++
						if owner := ownerByTable(owners, svc, ins.table); owner != "" {
							res.Points = append(res.Points, JournalWritePoint{
								Pos: pos, Form: ins.form, Service: owner, Literals: ins.literals,
							})
						}
					}
				}
				return true
			})
		}
	}

	if len(pending) > 0 {
		consts, cErr := resolvePackageConsts(root, rels, needConst)
		if cErr != nil {
			return res, cErr
		}
		for _, p := range pending {
			value := consts[p.dir][p.ident]
			if value == "" {
				continue
			}
			if p.idx >= 0 {
				if owner := ownerByTable(res.Owners, serviceOf(res.Points[p.idx].Pos), value); owner != "" {
					res.Points[p.idx].Service = owner
				}
				continue
			}
			res.Formatted[-p.idx-1].Resolved = value
		}
	}
	// Разрешённое имя таблицы делает оператор ТОЧКОЙ ЗАПИСИ ровно так же, как
	// литеральное. Шаг отдельный, потому что разрешение бывает отложенным:
	// значение константы дочитывает второй проход, и раньше него владелец
	// неизвестен.
	for _, f := range res.Formatted {
		if f.Resolved == "" {
			continue
		}
		owner := ownerByTable(res.Owners, f.svc, f.Resolved)
		if owner == "" {
			continue
		}
		res.Points = append(res.Points, JournalWritePoint{
			Pos: f.Pos, Form: JournalFormNameFormatted, Service: owner, Literals: f.literals,
		})
	}

	// Точки библиотечного вызова, чьё имя так и не разрешилось, владельцу не
	// сопоставлены — они остаются в переписи без владельца и потому не
	// зачитываются никому.
	kept := res.Points[:0]
	for _, p := range res.Points {
		if p.Service != "" {
			kept = append(kept, p)
		}
	}
	res.Points = kept
	return res, nil
}

type needSet map[string]map[string]bool

func (n needSet) setNeed(dir, name string) {
	if n[dir] == nil {
		n[dir] = map[string]bool{}
	}
	n[dir][name] = true
}

// resolvePackageConsts дочитывает значения названных строковых констант/переменных
// пакета. Второй проход намеренно УЗКИЙ: он открывает только те каталоги, где
// разбор упёрся в имя, — сплошной второй обход дерева стоил бы вдвое дороже
// ради нескольких десятков имён.
//
// Раннего выхода на пустом `need` здесь НЕТ намеренно, и это не забывчивость:
// такая охрана — ровно та форма, которую ловит соседний гейт
// (`TestCheckNeverAcceptsBecauseItsConstraintIsEmpty`), и он её на этом файле и
// нашёл. Сужение делает сам цикл, пропуская каталоги без искомых имён; выигрыш
// охраны был бы в одном пустом проходе по перечню путей, а цена — форма,
// неотличимая от «принимаем вход, потому что ограничение пусто».
func resolvePackageConsts(root string, rels []string, need needSet) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	fset := token.NewFileSet()
	for _, rel := range rels {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		dir := path.Dir(rel)
		wanted := need[dir]
		if len(wanted) == 0 {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil,
			parser.SkipObjectResolution)
		if err != nil {
			return out, fmt.Errorf("%s не разобран: %w", rel, err)
		}
		for name, value := range topLevelStringDecls(f) {
			if !wanted[name] {
				continue
			}
			if out[dir] == nil {
				out[dir] = map[string]string{}
			}
			out[dir][name] = value
		}
	}
	return out, nil
}

// topLevelStringDecls — строковые константы и переменные верхнего уровня файла.
func topLevelStringDecls(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, d := range f.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
			continue
		}
		for _, spec := range gen.Specs {
			vs, isValue := spec.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, isLit := vs.Values[i].(*ast.BasicLit); isLit && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						out[name.Name] = s
					}
				}
			}
		}
	}
	return out
}

// discoverJournalOwners выводит владельцев журнала ИЗ ДЕРЕВА: каталог
// `services/<svc>/internal/subscriptionjournal/` с объявленной строковой
// константой `Table`. Перечень владельцев не выписывается — он растёт вместе с
// эпиком, и выписанный разошёлся бы с деревом молча.
func discoverJournalOwners(root string, rels []string) ([]JournalOwner, error) {
	const marker = "/internal/subscriptionjournal/"
	fset := token.NewFileSet()
	var out []JournalOwner
	for _, rel := range rels {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") ||
			!strings.Contains(rel, marker) || !strings.HasPrefix(rel, "services/") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil,
			parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("%s не разобран: %w", rel, err)
		}
		table, ok := topLevelStringDecls(f)["Table"]
		if !ok || table == "" {
			continue
		}
		out = append(out, JournalOwner{
			Service: serviceOf(rel),
			Table:   table,
			Bare:    bareTable(table),
			Decl:    rel,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out, nil
}

// classifyJournalCall различает две формы ВЫЗОВА.
//
// Обе судятся по УЗЛУ, а не по тексту: комментарий, называющий вызов, в дерево
// разбора отдельной ветвью не входит, поэтому объяснения этого файла — включая
// приведённые в них вызовы — перепись не искажают.
func classifyJournalCall(call *ast.CallExpr) JournalWriteForm {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	switch sel.Sel.Name {
	case "Emit":
		// `…Outbox().Emit(…)` — порт журнала: приёмник сам есть вызов `Outbox()`.
		if inner, isCall := sel.X.(*ast.CallExpr); isCall {
			if innerSel, isSel := inner.Fun.(*ast.SelectorExpr); isSel && innerSel.Sel.Name == "Outbox" {
				return JournalFormPortCall
			}
		}
		fallthrough
	case "EmitAnchored":
		if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "outbox" {
			return JournalFormLibraryCall
		}
	}
	return ""
}

// journalCallWords — слова, которые точка вызова назвала САМА: строковые
// литералы и ССЫЛКИ НА КОНСТАНТЫ. Аргумент с номером skip (имя таблицы)
// пропускается.
//
// # Почему одних литералов НЕДОСТАТОЧНО
//
// Первая редакция считала словом только строковый литерал — и объявила
// ПЕРЕНОСАМИ все восемнадцать точек одного владельца, который называет вид
// КОНСТАНТОЙ словаря (`subscriptionjournal.KindLoadBalancer`). То есть
// дисциплина «слово журнала объявлено один раз», за которую этот владелец
// платил отдельной работой, читалась переписью как отсутствие слова. Ровно тот
// класс, ради которого гейт заведён, воспроизведённый на нём самом: узкий
// распознаватель не даёт ни красного, ни зелёного — он молчит.
//
// # Что считается ссылкой на константу
//
//	ДА   `subscriptionjournal.KindLoadBalancer` — уточнённое имя: слева стоит
//	     ИМЯ ПАКЕТА из импортов файла, значит справа символ уровня пакета;
//	ДА   голое имя, НЕ связанное параметром или приёмником объемлющей функции, —
//	     константа или переменная своего пакета;
//	НЕТ  `lb.ID` — слева локальная переменная, а не пакет: это данные строки, а
//	     не слово словаря;
//	НЕТ  всё, связанное параметром объемлющей функции: решение принял вызывающий,
//	     и точка исполняет чужое.
func journalCallWords(fn *ast.FuncDecl, imports map[string]bool, args []ast.Expr, skip int) []string {
	bound := boundNames(fn)
	var out []string
	for i, a := range args {
		if i == skip {
			continue
		}
		switch e := a.(type) {
		case *ast.BasicLit:
			if e.Kind != token.STRING {
				continue
			}
			if s, err := strconv.Unquote(e.Value); err == nil && s != "" {
				out = append(out, s)
			}
		case *ast.Ident:
			if !bound[e.Name] && e.Name != "nil" {
				out = append(out, e.Name)
			}
		case *ast.SelectorExpr:
			pkg, ok := e.X.(*ast.Ident)
			if ok && imports[pkg.Name] && !bound[pkg.Name] {
				out = append(out, pkg.Name+"."+e.Sel.Name)
			}
		}
	}
	return out
}

// boundNames — имена, связанные параметрами и приёмником функции.
func boundNames(fn *ast.FuncDecl) map[string]bool {
	bound := map[string]bool{}
	if fn == nil {
		return bound
	}
	collect := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, field := range fl.List {
			for _, name := range field.Names {
				bound[name.Name] = true
			}
		}
	}
	collect(fn.Recv)
	if fn.Type != nil {
		collect(fn.Type.Params)
	}
	return bound
}

// fileImportNames — имена, под которыми файл видит импортированные пакеты.
func fileImportNames(f *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, imp := range f.Imports {
		if imp.Name != nil {
			if imp.Name.Name != "_" && imp.Name.Name != "." {
				out[imp.Name.Name] = true
			}
			continue
		}
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		out[path.Base(p)] = true
	}
	return out
}

// resolveTableIdent пытается прочесть имя таблицы из выражения.
//
// Возврат: (имя, разрешимо). `разрешимо=false` означает форму, о которой разбор
// не берётся судить вовсе (вызов, поле, индексация); `имя=""` при
// `разрешимо=true` — имя стоит идентификатором, и его значение дочитывает второй
// проход по каталогу пакета.
func resolveTableIdent(expr ast.Expr, f *ast.File) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.Ident:
		if v, ok := topLevelStringDecls(f)[e.Name]; ok {
			return v, true
		}
		return "", true
	default:
		return "", false
	}
}

// formatArgFor находит аргумент вызова форматирования, стоящий против verbIndex-го
// глагола формата, у которого литерал lit служит строкой формата.
func formatArgFor(f *ast.File, lit *ast.BasicLit, verbIndex int) ast.Expr {
	var found ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 || call.Args[0] != ast.Expr(lit) {
			return true
		}
		if verbIndex+1 < len(call.Args) {
			found = call.Args[verbIndex+1]
		}
		return false
	})
	return found
}

// mentionsParamOrReceiver — выражение имени таблицы упоминает параметр либо
// приёмник объемлющей функции. Тогда предмет вставки решает ВЫЗЫВАЮЩИЙ, и это
// ПЕРЕНОС, а не неустановленный предмет.
//
// Различение структурное, а не по имени файла: послабление «вот здесь можно»
// пережило бы свой предмет молча, а связанность с параметром читается из того же
// узла и истекает вместе с ним.
func mentionsParamOrReceiver(f *ast.File, lit *ast.BasicLit, expr ast.Expr) bool {
	fn := journalEnclosingFunc(f, lit.Pos())
	if fn == nil {
		return false
	}
	bound := boundNames(fn)
	mentions := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && bound[id.Name] {
			mentions = true
		}
		return !mentions
	})
	return mentions
}

func journalEnclosingFunc(f *ast.File, pos token.Pos) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Pos() <= pos && pos <= fn.End() {
			return fn
		}
	}
	return nil
}

// sqlInsertTarget — одна вставка, найденная в тексте оператора.
type sqlInsertTarget struct {
	form      JournalWriteForm
	table     string // имя без схемы; пусто для формы с целиком форматируемым именем
	verbIndex int    // номер глагола формата, стоящего на месте имени таблицы
	line      int    // строка внутри текста (для миграций — строка файла)
	literals  []string
}

// insertTargets классифицирует каждую вставку текста по форме выражения имени
// таблицы. Формы не выписаны заранее: они и есть исчерпывающий разбор того, что
// может стоять после `INSERT INTO`.
func insertTargets(text string) []sqlInsertTarget {
	const marker = "insert into "
	low := strings.ToLower(text)
	var out []sqlInsertTarget
	for at := 0; ; {
		i := strings.Index(low[at:], marker)
		if i < 0 {
			return out
		}
		start := at + i
		pos := start + len(marker)
		at = pos
		rest := strings.TrimLeft(text[pos:], " \t\r\n")
		skipped := len(text[pos:]) - len(rest)
		target := sqlInsertTarget{
			line:      strings.Count(text[:start], "\n") + 1,
			verbIndex: countFormatVerbs(text[:pos+skipped]),
			literals:  insertValueLiterals(text[pos:]),
		}
		verb, verbLen, explicit := leadingFormatVerb(rest)
		var tail string
		switch {
		case verb && explicit >= 0:
			target.verbIndex = explicit
			fallthrough
		case verb:
			after := rest[verbLen:]
			if strings.HasPrefix(after, ".") {
				target.form = JournalFormSchemaFormatted
				name := readIdentifier(after[1:])
				target.table = bareTable(name)
				if target.table == "" {
					continue
				}
				tail = after[1+len(name):]
			} else {
				target.form = JournalFormNameFormatted
				tail = after
			}
		default:
			name := readIdentifier(rest)
			if name == "" {
				continue
			}
			target.form = JournalFormLiteralStatement
			target.table = bareTable(name)
			tail = rest[len(name):]
		}
		if !looksLikeInsertStatement(tail) {
			// Не оператор, а ПРОЗА: сообщение об ошибке, комментарий, строка
			// журнала. Без этого различения `"insert into %s: %w"` из текста
			// отказа считалось бы точкой записи, и перепись росла бы от
			// объяснений, а не от кода.
			continue
		}
		out = append(out, target)
	}
}

// looksLikeInsertStatement — за именем таблицы стоит то, что стоит в НАСТОЯЩЕМ
// операторе вставки: перечень колонок либо ключевое слово. Проза вида
// `"insert into %s: %w"` этому не отвечает.
func looksLikeInsertStatement(tail string) bool {
	t := strings.TrimLeft(tail, " \t\r\n")
	if strings.HasPrefix(t, "(") {
		return true
	}
	word := strings.ToLower(readIdentifier(t))
	switch word {
	case "values", "select", "default", "overriding", "as", "table":
		return true
	}
	return false
}

// leadingFormatVerb — текст начинается глаголом формата (`%s`, `%q`, `%[1]s`).
// Возвращает длину глагола и ЯВНЫЙ номер аргумента (или -1, если номер не задан).
func leadingFormatVerb(s string) (bool, int, int) {
	if !strings.HasPrefix(s, "%") {
		return false, 0, -1
	}
	i, explicit := 1, -1
	if strings.HasPrefix(s[i:], "[") {
		end := strings.IndexByte(s[i:], ']')
		if end < 0 {
			return false, 0, -1
		}
		n, err := strconv.Atoi(s[i+1 : i+end])
		if err != nil {
			return false, 0, -1
		}
		explicit = n - 1
		i += end + 1
	}
	if i < len(s) && (s[i] == 's' || s[i] == 'q' || s[i] == 'v') {
		return true, i + 1, explicit
	}
	return false, 0, -1
}

// countFormatVerbs — сколько глаголов формата стоит в тексте до этого места.
func countFormatVerbs(s string) int {
	n := 0
	for i := 0; i < len(s)-1; i++ {
		if s[i] != '%' {
			continue
		}
		if s[i+1] == '%' {
			i++
			continue
		}
		n++
	}
	return n
}

// readIdentifier читает имя таблицы (возможно схема-квалифицированное).
func readIdentifier(s string) string {
	end := 0
	for end < len(s) {
		c := s[end]
		if c == '_' || c == '.' || c == '"' || (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			end++
			continue
		}
		break
	}
	return strings.Trim(s[:end], `".`)
}

// insertValueLiterals — строковые литералы SQL, стоящие в этой вставке. Ими
// читается, назвала ли точка хоть одно слово сама.
func insertValueLiterals(s string) []string {
	// Границей служит конец ОПЕРАТОРА: без неё в перечень слов уезжали бы
	// литералы соседних операторов той же миграции, и перенос стал бы
	// неотличим от точки, назвавшей слово.
	if end := strings.IndexByte(s, ';'); end >= 0 {
		s = s[:end]
	}
	if end := strings.Index(strings.ToLower(s), "insert into "); end >= 0 {
		s = s[:end]
	}
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '\'' {
			continue
		}
		j := strings.IndexByte(s[i+1:], '\'')
		if j < 0 {
			break
		}
		if v := strings.TrimSpace(s[i+1 : i+1+j]); v != "" {
			out = append(out, v)
		}
		i += j + 1
	}
	return out
}

func bareTable(t string) string {
	t = strings.Trim(t, `"`)
	if i := strings.LastIndexByte(t, '.'); i >= 0 {
		t = t[i+1:]
	}
	return strings.Trim(t, `"`)
}

// ownerByTable сопоставляет имя таблицы владельцу. Сравнение идёт по имени БЕЗ
// схемы: у одного владельца журнал объявлен схема-квалифицированным, у другого —
// нет, и сравнение полных имён молча промахивалось бы ровно по второму. Каталог
// сервиса служит уточнением, а не условием: журнал соседа из чужого каталога
// писать нельзя (DB-per-service), и такая точка обязана быть видна.
func ownerByTable(owners []JournalOwner, svc, table string) string {
	bare := bareTable(table)
	if bare == "" {
		return ""
	}
	for _, o := range owners {
		if o.Bare != bare {
			continue
		}
		if svc == "" || svc == o.Service {
			return o.Service
		}
		return o.Service
	}
	return ""
}

func ownerService(owners []JournalOwner, svc string) bool {
	for _, o := range owners {
		if o.Service == svc {
			return true
		}
	}
	return false
}

func serviceOf(rel string) string {
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, "services/") {
		return ""
	}
	rest := rel[len("services/"):]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return ""
}

func isMigrationPath(rel string) bool {
	return strings.HasPrefix(rel, "services/") && strings.Contains(rel, "/internal/migrations/")
}

func underAnyRoot(rel string, roots []string) bool {
	for _, r := range roots {
		if strings.HasPrefix(rel, r) {
			return true
		}
	}
	return false
}

func posOf(fset *token.FileSet, rel string, p token.Pos) string {
	return fmt.Sprintf("%s:%d", rel, fset.Position(p).Line)
}

// ── ВЕРДИКТ, ОТДЕЛЁННЫЙ ОТ ПЕЧАТИ ────────────────────────────────────────────
//
// Находки возвращаются значением, а не пишутся в `*testing.T`, чтобы инъекция
// судила ИСХОД, а не вывод: проверка, сверяющая текст лога, зеленеет от правки
// формулировки и краснеет от неё же, ничего не сказав о свойстве.

// JournalCensusPremiseFailures — причины, по которым перепись БЕСПРЕДМЕТНА.
// Отделены от находок: пустой обход не «ноль нарушений», а «ноль прочитанного».
func JournalCensusPremiseFailures(c JournalWriteFormCensus) []string {
	var out []string
	if c.GoFiles == 0 || c.SQLFiles == 0 {
		out = append(out, fmt.Sprintf("осмотрено файлов Go %d, миграций %d — обход пуст, и "+
			"вердикт беспредметен: «ноль находок» тут неотличимо от «ноль прочитанного»",
			c.GoFiles, c.SQLFiles))
	}
	if len(c.Owners) == 0 {
		out = append(out, "владельцев журнала не найдено ни одного. Перечень ВЫВОДИТСЯ обходом "+
			"(`services/*/internal/subscriptionjournal/` с объявленной константой Table); "+
			"пустой означает, что распознаватель владельцев умер, а не что журналов нет")
	}
	return out
}

// JournalCensusFindings — находки переписи.
func JournalCensusFindings(c JournalWriteFormCensus) []string {
	// Беспредметная перепись НЕ ОБВИНЯЕТ. На пустом обходе всякий распознаватель
	// даёт ноль, и «форма умерла» стало бы утверждением о дереве, которого не
	// читали. Проверка предпосылки стоит здесь, а не только у вызывающего:
	// свойство принадлежит вердикту, а не порядку его печати.
	if len(JournalCensusPremiseFailures(c)) > 0 {
		return nil
	}
	var out []string
	for _, form := range JournalWriteForms {
		if c.Recognizer[form] > 0 {
			continue
		}
		out = append(out, fmt.Sprintf("форма %q не найдена в дереве НИ РАЗУ — значит её "+
			"распознаватель больше не узнаёт предмет, и ноль журнальных точек этой формы "+
			"ничего не утверждает.\nИсходов два: починить распознаватель — либо снять форму "+
			"ВМЕСТЕ с её предметом (строкой переписи, ветвью разбора и пробой инъекции одним "+
			"изменением), если форма из дерева ушла. «Оставить как есть» исходом не является: "+
			"послабление, которое не истечёт никогда, остаётся на вид рабочим", form))
	}
	for _, owner := range c.Owners {
		total := 0
		for _, form := range JournalWriteForms {
			total += len(c.PointsOf(owner.Service, form))
		}
		if total > 0 {
			continue
		}
		out = append(out, fmt.Sprintf("%s: журнал %q объявлен (%s), а точек записи у него НОЛЬ "+
			"по всем формам.\nПодписчик откроет поток, который не наполнится никогда, и "+
			"отличить это от «событий пока нет» он не сможет ничем.\nИсходов два: завести "+
			"производителя — либо снять объявление журнала вместе с его провязкой",
			owner.Service, owner.Table, owner.Decl))
	}
	for _, ins := range c.Unresolved() {
		out = append(out, fmt.Sprintf("%s: имя таблицы подставляется форматированием ЦЕЛИКОМ "+
			"(`INSERT INTO %%s`), и разбор его НЕ РАЗРЕШАЕТ: ни константа пакета, ни параметр "+
			"объемлющей функции.\nПро такой оператор не известно даже того, ПИШЕТ ЛИ ОН "+
			"ЖУРНАЛ, — то есть он может идти мимо порта, мимо словаря видов и мимо общего "+
			"строителя нагрузки, и ни один соседний гейт об этом не скажет.\nИсход: назвать "+
			"таблицу КОНСТАНТОЙ пакета (тогда предмет читается разбором) либо принять её "+
			"ПАРАМЕТРОМ (тогда решение принимает вызывающий, и его считает форма %q)",
			ins.Pos, JournalFormLibraryCall))
	}
	return out
}
