// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// withdrawalproducerarriveswiththeapplier.go — разбор прод-дерева iam на
// СОГЛАСИЕ двух фактов: применитель ролей модуля приводится в действие, и
// производитель отзыва роли существует (задача продукта #1913; форма отзыва —
// `services/iam/docs/engineering/architecture/role-withdrawal-is-a-mark.md`).
//
// # Предмет
//
// Сверка `moduleroles.Reconcile` объявляет ДВА вида расхождения, и у одного из
// них исхода нет: `LiveNotDeclared` — «строка живёт, объявления у неё больше
// нет». Снять такую роль сегодня нечем: единственный производитель снятия
// системной роли в дереве — разовая миграция, то есть выкатка образа iam, ровно
// то, ради устранения чего манифест и заведён.
//
// Пока применитель НЕ приводится в действие в проде, это остаток, а не дефект:
// перечень не производится, потому что не производится ничего. Как только
// применитель начинают звать, остаток становится дефектом, необратимым ДЛЯ
// АРЕНДАТОРА: роль, которую нельзя снять, живёт вечно, и право, выданное через
// неё, продолжает действовать после того, как модуль перестал её объявлять.
//
// Сильнее того: следствие достаёт и до ПУСКА — но не тем механизмом, который
// здесь стоял. Прежняя редакция говорила, что «загрузка на старте СВЕРЯЕТ роли
// и отказывает при расхождении», и это неверно: путь старта роли не сверяет
// вовсе. Предикаты, которыми перемерено (#2010):
//
//	git grep -c 'moduleroles.Reconcile' -- 'services/iam/**/*.go' ':!*_test.go'  → 0
//	git grep -n 'AssertRoleParity\|TierParity' -- 'services/iam/cmd/kaname/*.go' → пусто
//
// `Apply` о снятии не знает by construction, поэтому роль, убранная из раздела,
// пуск НЕ роняет — она просто остаётся жить, и это первый абзац выше.
//
// **Действительный механизм — ключ каталога, и он строже.** `role_rule_ref_res_fk`
// объявлен `ON UPDATE NO ACTION`, а переселение последствий сужено
// `is_system = false` НАМЕРЕННО (`catalog_consequence_sql.go`, довод: «манифест,
// снимающий ресурс, который его же роль называет, противоречит сам себе, и это
// обязано быть отвергнуто ключом»). Значит правка, снимающая роль ВМЕСТЕ с её
// ресурсом, роняет применение каталога отказом ключа — то есть пуск, — а выхода
// правкой манифеста нет: снятие роли из раздела строку не убирает, потому что
// производителя отзыва не существует. Выход снова требует выкатки образа.
//
// Разница названа, а не сглажена: механизм в диагностике обязан быть тот,
// который читатель пойдёт чинить.
//
// # Что здесь считается находкой — ОДНО согласие, а не два запрета
//
// Находка — состояние «применитель приводится в действие, производителя отзыва
// нет». Ни одна половина по отдельности находкой не является:
//
//	приводится в действие · производитель есть  → норма, работа сделана
//	приводится в действие · производителя нет   → НАХОДКА
//	не приводится        · производителя нет    → сегодняшнее дерево: остаток
//	не приводится        · производитель есть   → норма, производитель приехал раньше
//
// Последняя строка — не педантизм: пометка, написанная раньше своего
// вызывающего, была бы вторым мёртвым механизмом рядом с первым, но мёртвый
// механизм ловит не этот гейт, и краснеть здесь на нём значило бы судить чужой
// предмет.
//
// # Обе половины судятся по УЗЛУ РАЗБОРА, а не по слову
//
// «Применитель приводится в действие» — это ВЫЗОВ через импортированный пакет, а
// не упоминание его имени. Разница измерима прямо в дереве: `moduleroleparity`
// называет `moduleroles.Applier` в комментарии, не импортируя пакет вовсе, а
// пять `_test.go` его импортируют по-настоящему. Гейт, судящий подстроку,
// объявил бы прод-вызов там, где стоит проза, и наоборот.
//
// «Производитель отзыва» — это оператор ЗАПИСИ над `roles`, ставящий пометку
// снятия В СПИСКЕ ПРИСВОЕНИЙ. Читать её в условии (`WHERE live`) — не запись, и
// распознаватель, не различающий `SET live = false` и `WHERE live`, объявил бы
// производителем любое чтение живых ролей.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **миграцию как производителя.** Она им быть не вправе: миграция как
//     писатель роли модуля запрещена отдельным гейтом
//     (`migrationnotawriterofmodulerole.go`, Г2), и предмет #1913 в том и
//     состоит, что отзыв не должен требовать выкатки;
//  2. **производителя вне дерева iam.** Таблица `roles` принадлежит iam, и
//     писатель извне означал бы, что предмет переехал целиком;
//  3. **запрос, собранный из кусков в рантайме.** Формы в дереве нет.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// applierImportPath — пакет применителя ролей модуля.
const applierImportPath = "github.com/PRO-Robotech/kacho-iam/internal/apps/kacho/moduleroles"

// applierDriveNames — имена, которыми применитель ПРИВОДИТСЯ В ДЕЙСТВИЕ.
// Перечень выведен из объявлений пакета, а не угадан: `NewApplier` собирает
// применитель, `Reconcile` исполняет сверку. Обе — единственные экспортируемые
// точки входа, через которые пакет вообще может что-то сделать.
var applierDriveNames = map[string]struct{}{
	"NewApplier": {},
	"Reconcile":  {},
}

// RoleWithdrawalSite — координата одной из двух половин.
type RoleWithdrawalSite struct {
	File string
	Line int
	// What — что именно найдено: имя вызванной точки входа либо начало литерала.
	What string
}

// RoleWithdrawalCensus — объём осмотренного одним файлом.
type RoleWithdrawalCensus struct {
	// AppliedImports — импортов пакета применителя прочитано.
	AppliedImports int
	// Selectors — обращений вида `пакет.Имя` прочитано. Печатается отдельно:
	// ноль обращений при непустом импорте означает, что пакет ввезён и не
	// используется, — это другое состояние, чем «не ввезён вовсе».
	Selectors int
	// StringLiterals — строковых литералов прочитано.
	StringLiterals int
	// Comments — комментариев прочитано. Ненулевой счётчик при нулевых находках
	// говорит, что различение «код против прозы» вообще имело предмет.
	Comments int
	// WritesOverRoles — операторов записи над `roles` прочитано.
	WritesOverRoles int
}

// roleWithdrawalWriteRe — начало оператора ЗАПИСИ над таблицей ролей. Граница
// `\b` после `roles` отделяет саму таблицу от её проекций (`role_rule_ref`,
// `role_verb`, `role_rule_selectors`): их запись к отзыву роли отношения не
// имеет.
var roleWithdrawalWriteRe = regexp.MustCompile(`(?is)\b(?:update|insert\s+into)\s+(?:kacho_iam\.)?roles\b`)

// roleWithdrawalMarkRe — пометка снятия В ПРИСВОЕНИИ. Форма взята из решения
// (`live boolean` под `CHECK (live = (retired_at IS NULL))`), а не изобретена:
// домен уже применил её трижды к каталогу прав, и второй формы не заводится.
var roleWithdrawalMarkRe = regexp.MustCompile(`(?i)\b(?:retired_at|live)\s*=`)

// roleWithdrawalSetTailRe — конец списка присвоений. За ним `live` читается, а
// не пишется, и засчитывать его производителем значило бы объявить писателем
// всякое чтение живых ролей.
var roleWithdrawalSetTailRe = regexp.MustCompile(`(?is)\b(?:where|returning|from)\b`)

// ScanRoleWithdrawalWiring разбирает один файл Go.
//
// Возвращает две половины раздельно — приведение применителя в действие и
// производителя отзыва, — потому что находка есть их НЕСОГЛАСИЕ, а не любая из
// них. Сводит половины вызывающий: у одного файла может не быть ни одной.
func ScanRoleWithdrawalWiring(path string, src []byte) (drive, mark []RoleWithdrawalSite, census RoleWithdrawalCensus, err error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, parser.ParseComments)
	if perr != nil {
		return nil, nil, RoleWithdrawalCensus{}, perr
	}
	for _, g := range f.Comments {
		census.Comments += len(g.List)
	}

	// Связывание импорта: имя, под которым пакет применителя виден в ЭТОМ файле.
	// Судить по последнему сегменту пути нельзя — псевдоним законен и молча
	// увёл бы обход мимо настоящего вызова.
	binding := ""
	for _, imp := range f.Imports {
		p, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil || p != applierImportPath {
			continue
		}
		census.AppliedImports++
		if imp.Name != nil {
			binding = imp.Name.Name
		} else {
			binding = applierImportPath[strings.LastIndexByte(applierImportPath, '/')+1:]
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			id, ok := v.X.(*ast.Ident)
			if !ok {
				return true
			}
			census.Selectors++
			if binding == "" || id.Name != binding {
				return true
			}
			if _, ok := applierDriveNames[v.Sel.Name]; ok {
				drive = append(drive, RoleWithdrawalSite{
					File: path, Line: fset.Position(v.Sel.Pos()).Line, What: v.Sel.Name,
				})
			}
		case *ast.BasicLit:
			if v.Kind != token.STRING {
				return true
			}
			census.StringLiterals++
			text := litText(v.Value)
			hits := roleWithdrawalWriteRe.FindAllStringIndex(text, -1)
			census.WritesOverRoles += len(hits)
			for _, m := range hits {
				if !assignsWithdrawalMark(text[m[1]:]) {
					continue
				}
				mark = append(mark, RoleWithdrawalSite{
					File: path, Line: fset.Position(v.Pos()).Line, What: firstLineOf(v.Value),
				})
				break
			}
		}
		return true
	})
	return drive, mark, census, nil
}

// assignsWithdrawalMark — стоит ли пометка снятия в СПИСКЕ ПРИСВОЕНИЙ хвоста
// оператора. Хвост режется по первому `WHERE`/`RETURNING`/`FROM`: дальше та же
// колонка читается, а не пишется.
func assignsWithdrawalMark(tail string) bool {
	i := strings.Index(strings.ToLower(tail), "set")
	if i < 0 {
		return false
	}
	seg := tail[i+len("set"):]
	if end := roleWithdrawalSetTailRe.FindStringIndex(seg); end != nil {
		seg = seg[:end[0]]
	}
	return roleWithdrawalMarkRe.MatchString(seg)
}
