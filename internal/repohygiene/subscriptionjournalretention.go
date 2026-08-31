// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionjournalretention.go — разбор ПОЛОС ОДНОГО МЕХАНИЗМА: что каждый
// владелец журнала подписки ОБЪЯВИЛ про удержание и что у него ПРОВЯЗАНО.
//
// # Предмет — РАЗНИЦА МЕЖДУ ПОЛОСАМИ, а не свойство каждой
//
// Журнал подписки заведён у пяти владельцев одной формой (`pkg/subscription`), и
// у каждого своё объявление удержания. Проверять полосы ПООТДЕЛЬНОСТИ здесь
// нельзя: такая проверка требует знать, каким объявление ДОЛЖНО быть, — а это и
// есть спорный вопрос, решаемый владельцем домена. Сравнение полос спрашивает
// другое, и на это ответ есть всегда: «решал ли кто-нибудь, что они различаются»
// (`architecture.md` §«Параллельные полосы одного механизма обязаны сверяться
// МЕЖДУ СОБОЙ»).
//
// Отсюда предмет: объявление и провязка — ОДНО решение. Владелец, объявивший
// `RetainsFromEarliestRow`, обязан нести уборщика; владелец, объявивший
// `RetainsEverything`, обязан его НЕ нести. Обе стороны важны одинаково:
//
//   - объявил чистку, уборщика нет — служебное сообщение открытия называет
//     подписчику нижнюю возобновимую позицию, которая никогда не двигается;
//     контракт говорит «удерживаю не всё», а таблица растёт вечно;
//   - уборщик есть, объявлено удержание — сервер шлёт подписчику признак
//     «удерживаю ВСЁ», то есть обещание, что отказ «позиция утрачена» не
//     наступит никогда, — и тут же снимает строки у него из-под курсора. Это
//     худшая из двух: потеря молчаливая.
//
// # Почему разбор, а не поиск по слову
//
// Слово `RetainsEverything` встречается в этом дереве в комментариях чаще, чем в
// объявлениях: у каждого владельца оно стоит в прозе, объясняющей решение.
// Предикат по подстроке краснел бы на собственном объяснении. Поэтому объявление
// читается УЗЛОМ синтаксического дерева — полем составного литерала
// `subscription.Storage`, — а провязка ищется вызовом по имени функции.
//
// # ПРЕДПОСЫЛКИ РАЗБОРА — заявлены, потому что они факты о дереве
//
//  1. объявление владельца лежит в `services/<svc>/internal/subscriptionjournal/`
//     и собирается составным литералом `subscription.Storage{…}`;
//  2. имя таблицы задаётся в том же пакете строковой константой либо литералом
//     в самом поле `Table`;
//  3. уборка поднимается вызовом `subscription.StartJournalRetentionSweep` —
//     прямо либо через обёртку в том же дереве владельца.
//
// Каждая предпосылка может измениться, поэтому гейт печатает объём КАЖДОЙ полосы
// и падает на пустом обходе: ноль полос, ноль объявлений, ноль разобранных имён
// таблиц означают слепоту разбора, а не благополучие дерева.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// JournalLane — одна полоса механизма: владелец журнала и его объявление.
type JournalLane struct {
	// Owner — «services/registry».
	Owner string
	// Table — имя таблицы, как его объявил владелец (со схемой либо без).
	Table string
	// Retention — имя объявленного значения: «RetainsEverything» либо
	// «RetainsFromEarliestRow». Пусто, если поле не объявлено вовсе.
	Retention string
	// AgeColumn — имя колонки срока; пусто, если поле не объявлено.
	AgeColumn string
	// File — где объявление найдено (для текста отказа).
	File string
	// Sweeper — провязан ли уборщик журнала в дереве этого владельца.
	Sweeper bool
	// SweeperFile — где найдена провязка.
	SweeperFile string
}

// JournalLaneCensus — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type JournalLaneCensus struct {
	// FilesRead — файлов объявления прочитано.
	FilesRead int
	// Lanes — полос распознано.
	Lanes int
	// RetentionDeclared — из них объявили удержание словом.
	RetentionDeclared int
	// TablesResolved — из них имя таблицы разобрано (не осталось выражением).
	TablesResolved int
	// SweepFilesRead — прод-файлов дерева владельцев просмотрено на провязку.
	SweepFilesRead int
	// Sweeping — полос, у которых провязка найдена.
	Sweeping int
}

// sweepStarterName — имя, которым поднимается уборка журнала.
//
// Одно на дерево: второе имя означало бы вторую точку подъёма, и полоса,
// поднявшая уборку иначе, стала бы этому разбору невидимой — не «разрешённой», а
// НЕ ОСМОТРЕННОЙ.
const sweepStarterName = "StartJournalRetentionSweep"

// ScanJournalLane читает объявление одного владельца.
//
// Возвращает полосу и признак «объявление найдено». Отсутствие литерала
// `subscription.Storage` в файле — не отказ: у владельца может лежать несколько
// файлов, и решает вызывающий, обойдя их все.
func ScanJournalLane(owner, path string, src []byte) (JournalLane, bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return JournalLane{}, false, err
	}

	consts := jrStringConsts(f)

	lane := JournalLane{Owner: owner, File: path}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Storage" {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "subscription" {
			return true
		}
		found = true
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Table":
				lane.Table = jrResolveString(kv.Value, consts)
			case "AgeColumn":
				lane.AgeColumn = jrResolveString(kv.Value, consts)
			case "Retention":
				if s, ok := kv.Value.(*ast.SelectorExpr); ok && s.Sel != nil {
					lane.Retention = s.Sel.Name
				}
			}
		}
		return false
	})
	return lane, found, nil
}

// JournalSweepWiredIn отвечает, поднимается ли в этом файле уборка журнала.
//
// Судит ВЫЗОВ по имени функции — узлом разбора, а не подстрокой: имя стоит в
// комментариях обоих владельцев, и предикат по слову зеленел бы на прозе.
func JournalSweepWiredIn(path string, src []byte) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	wired := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != sweepStarterName {
			return true
		}
		wired = true
		return false
	})
	return wired, nil
}

// jrStringConsts — строковые константы файла: `Table = "kacho_x.y"`.
func jrStringConsts(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if s, ok := jrLiteralString(vs.Values[i]); ok {
					out[name.Name] = s
				}
			}
		}
	}
	return out
}

// jrResolveString — значение поля: литерал либо константа ТОГО ЖЕ пакета.
//
// Всё прочее остаётся пустым, и это НЕ «поле не объявлено»: перепись считает
// разобранные имена отдельно, поэтому нераспознанная форма видна числом, а не
// подшита к отсутствию.
func jrResolveString(e ast.Expr, consts map[string]string) string {
	if s, ok := jrLiteralString(e); ok {
		return s
	}
	if id, ok := e.(*ast.Ident); ok {
		return consts[id.Name]
	}
	return ""
}

func jrLiteralString(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// RetainsSweeping / RetainsEverythingName — имена объявленных значений.
//
// Стоят литералами намеренно: разбор судит ЧУЖОЕ объявление, и вывод имени из
// того же пакета, который он проверяет, сделал бы сверку тождественно истинной
// при переименовании.
const (
	RetainsSweeping       = "RetainsFromEarliestRow"
	RetainsEverythingName = "RetainsEverything"
)

// JournalLaneFindings — ПРАВИЛО СВЕРКИ ПОЛОС, отделённое от обхода дерева.
//
// Отделено затем, чтобы способность правила падать доказывалась ПОДАННЫМ входом,
// а не подгонкой дерева: инъекция кормит его синтетическими полосами и требует
// красного по каждой оси отдельно, а на законной паре — молчания.
//
// `ageOK` отвечает, объявлена ли колонка срока умолчанием часов БАЗЫ. Это
// свойство схемы владельца, читается оно из миграций, и подавать его функцией —
// единственный способ проверить правило без дерева.
func JournalLaneFindings(lanes []JournalLane, ageOK func(JournalLane) bool) []string {
	var findings []string
	for _, l := range lanes {
		name := l.Owner + "/" + TableNameOf(l.Table)
		switch l.Retention {
		case RetainsSweeping:
			if !l.Sweeper {
				findings = append(findings, name+
					": объявлено "+RetainsSweeping+", а уборщик НЕ провязан — "+
					"служебное сообщение открытия называет подписчику нижнюю возобновимую "+
					"позицию, которая не двигается никогда, а таблица растёт вечно")
			}
			if l.AgeColumn == "" {
				findings = append(findings, name+
					": объявлено "+RetainsSweeping+" без колонки срока — "+
					"предикат уборки не из чего построить")
			} else if !ageOK(l) {
				findings = append(findings, name+
					": колонка срока "+l.AgeColumn+" не объявлена `DEFAULT now()` ни одной "+
					"миграцией — часы отметки и часы уборки перестали быть одним источником, "+
					"и у порога появилось слагаемое, которого никто не назвал")
			}
		case RetainsEverythingName:
			if l.Sweeper {
				findings = append(findings, name+
					": уборщик провязан, а объявлено "+RetainsEverythingName+" — "+
					"сервер шлёт подписчику признак «удерживаю ВСЁ» и тут же снимает строки "+
					"у него из-под курсора; потеря МОЛЧАЛИВАЯ")
			}
			if l.AgeColumn != "" {
				findings = append(findings, name+
					": колонка срока названа при "+RetainsEverythingName+
					" — объявление, которого не читает никто")
			}
		default:
			declared := l.Retention
			if declared == "" {
				declared = "«не объявлено»"
			}
			findings = append(findings, name+
				": удержание объявлено значением "+declared+
				", которого разбор не знает — полоса вне наблюдения")
		}
	}
	return findings
}

// TableNameOf — имя таблицы без схемы: полосы объявляют его по-разному, а
// сравнивать их надо одной единицей.
func TableNameOf(table string) string {
	if i := strings.LastIndex(table, "."); i >= 0 {
		return table[i+1:]
	}
	return table
}
