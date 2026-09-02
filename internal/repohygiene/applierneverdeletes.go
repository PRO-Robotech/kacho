// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// applierneverdeletes.go — разбор пакета применителя ролей на предмет удаления
// строки роли (приёмка
// `services/iam/docs/engineering/acceptance/roles-come-as-data-not-migrations.md`
// §3.1, держатель Г3; сценарий MOD-RD-15).
//
// # Предмет
//
// Роль с выдачами удалить нельзя — `access_bindings_role_fk … ON DELETE
// RESTRICT` отвергнет операцию, и применитель встанет на первой же роли,
// которой кто-то пользуется. А если бы не отверг, каскад унёс бы селекторы,
// проекцию глаголов и проекцию сегментов МОЛЧА: каскад ничего не печатает.
// Отзыв роли — отдельный предмет (#1823), и «снять и положить» вместо
// приведения его не заменяет.
//
// # Что здесь считается находкой — ДВЕ оси, и первая сильнее
//
//  1. **порт применителя объявляет удаляющий глагол.** Это ось by construction:
//     пока порт его не несёт, применитель не может удалить НИЧЕГО, каким бы ни
//     был его код. Ось судит имена методов интерфейса, а не их тела;
//  2. **в пакете стоит оператор `DELETE` над таблицей ролей.** Ось судит
//     СТРОКОВЫЙ ЛИТЕРАЛ узла разбора, а не текст файла: слово `DELETE` стоит и
//     в комментариях, объясняющих сам запрет, — гейт, судящий подстроку,
//     краснел бы на собственном объяснении.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
// Запрос, собранный из кусков в рантайме (`"DELE" + "TE FROM roles"`), и запрос,
// приехавший параметром. Первое — форма, которой в дереве нет; второе ловит
// первая ось: чтобы позвать чужое удаление, порт обязан его объявить.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// ApplierDeleteSite — координата находки.
type ApplierDeleteSite struct {
	File string
	Line int
	// Kind — ось: `port-verb` либо `sql-literal`.
	Kind string
	// What — что именно найдено: имя метода либо начало литерала.
	What string
}

// ApplierDeleteCensus — объём осмотренного одним файлом.
type ApplierDeleteCensus struct {
	// InterfaceMethods — методов интерфейсов прочитано.
	InterfaceMethods int
	// StringLiterals — строковых литералов прочитано.
	StringLiterals int
	// Comments — комментариев прочитано. Печатается отдельно: их гейт судить НЕ
	// обязан, и ненулевой счётчик при нулевых находках говорит, что различение
	// «код против текста» вообще имело предмет.
	Comments int
}

// applierDeleteVerbRe — имена методов, означающие удаление.
var applierDeleteVerbRe = regexp.MustCompile(`^(Delete|Remove|Drop|Purge|Retire)`)

// applierDeleteSQLRe — оператор удаления над таблицей ролей в строковом
// литерале.
//
// # Образец привязан к НАЧАЛУ СТРОКИ, и это несущее
//
// Запрос, отдаваемый оператору, НАЧИНАЕТСЯ глаголом (возможно, после отступа,
// перевода строки или общего табличного выражения) — таков он и в дереве, где
// запросы пишутся сырыми строками с переносами. Проза о самом запрете
// («применитель не производит DELETE FROM roles») несёт то же слово ПОСРЕДИ
// предложения. Образец без привязки не различает их и краснеет на тексте
// отказа, объясняющем запрет, — то есть на собственном объяснении гейта. Это
// поймал законный близнец инъекции, а не подозрение.
//
// Слепая зона названа: сообщение об отказе, НАЧИНАЮЩЕЕСЯ с самого оператора,
// стало бы находкой. Форма редкая, чинится перефразированием, и цена ошибки
// здесь — лишний разбор, а не пропущенное удаление.
var applierDeleteSQLRe = regexp.MustCompile(`(?im)^\s*(with\b[^\n]*\n\s*)?delete\s+from\s+(kacho_iam\.)?roles\b`)

// ScanApplierDeletes разбирает один файл пакета применителя.
func ScanApplierDeletes(path string, src []byte) (sites []ApplierDeleteSite, census ApplierDeleteCensus, err error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, parser.ParseComments)
	if perr != nil {
		return nil, ApplierDeleteCensus{}, perr
	}
	for _, g := range f.Comments {
		census.Comments += len(g.List)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.InterfaceType:
			if v.Methods == nil {
				return true
			}
			for _, m := range v.Methods.List {
				for _, name := range m.Names {
					census.InterfaceMethods++
					if applierDeleteVerbRe.MatchString(name.Name) {
						sites = append(sites, ApplierDeleteSite{
							File: path, Line: fset.Position(name.Pos()).Line,
							Kind: "port-verb", What: name.Name,
						})
					}
				}
			}
		case *ast.BasicLit:
			if v.Kind != token.STRING {
				return true
			}
			census.StringLiterals++
			if applierDeleteSQLRe.MatchString(litText(v.Value)) {
				sites = append(sites, ApplierDeleteSite{
					File: path, Line: fset.Position(v.Pos()).Line,
					Kind: "sql-literal", What: firstLineOf(v.Value),
				})
			}
		}
		return true
	})
	return sites, census, nil
}

// litText — содержимое литерала БЕЗ кавычек. Обязательно: образец привязан к
// началу строки, а `v.Value` несёт открывающую кавычку — без снятия привязка
// не срабатывала бы никогда, и вторая ось гейта молчала бы всегда. Поймано
// инъекцией, а не чтением.
func litText(v string) string {
	if u, err := strconv.Unquote(v); err == nil {
		return u
	}
	return strings.Trim(v, "`\"")
}

// firstLineOf — начало литерала для текста находки: целый запрос в сообщении
// нечитаем, а координата и первая строка называют место однозначно.
func firstLineOf(s string) string {
	s = strings.TrimSpace(strings.Trim(s, "`\""))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return strings.TrimSpace(s)
}
