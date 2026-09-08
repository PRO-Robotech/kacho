// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verdictsubjectglue_test.go — Г3 приёмки R7-1: СУБЪЕКТ ВЫДАЧИ ОСТАЁТСЯ
// ИНДЕКСИРУЕМЫМ ВХОДОМ.
//
// # Предмет
//
// Склейка `subject_type || ':' || subject_id` в предикате отбора выводит обе
// колонки из-под любого индекса: сравнивать приходится ВЫЧИСЛЕННОЕ значение, а
// вычисленное значение отбирает строки только после того, как они прочитаны.
// Пока склейка стоит на пути принятия решения, «выдача называет этого субъекта»
// перестаёт быть сужением и становится фильтром — то есть работа растёт с
// числом выдач в облаке, а не с числом выдач спрашиваемому.
//
// # ГРАНИЦА, И ОНА НАМЕРЕННАЯ
//
// Предмет — склейка СУБЪЕКТА ВЫДАЧИ, и только она. Склейка ЧЛЕНА ГРУППЫ
// (`member_type || ':' || member_id`) в границы под-фазы НЕ входит (§7, п. 10
// приёмки): на пути принятия решения её нет вовсе — членство ищется парой
// колонок, — а оставшиеся два места суть ПРОЕКЦИЯ обратных вопросов. Гейт на
// них обязан МОЛЧАТЬ: покраснев, он дал бы находку вне предмета, её «починили»
// бы чужой правкой, и работа уехала бы в чужую линию.
//
// # ЧТО СЧИТАЕТСЯ НАХОДКОЙ, А ЧТО ЗАКОННЫМ БЛИЗНЕЦОМ
//
// Находка — склейка в ПРЕДИКАТЕ (`ON`, `WHERE`, `HAVING`, `AND`, `OR`): там она
// отбирает строки. Законный близнец — та же склейка в СПИСКЕ ВЫБОРКИ: там она
// ничего не отбирает, а называет ответ, и заменить её парой колонок значило бы
// поменять форму ответа ради формы запроса.
//
// # ОБЪЁМ И ЕГО ГРАНИЦА, НАЗВАННАЯ ЧЕСТНО
//
// Гейт читает строковые литералы прод-кода пути вердикта, разбирая Go по
// синтаксическому дереву: иначе SQL внутри Go-комментария читался бы как код.
// Внутри литерала снимаются SQL-комментарии — комментарий, объясняющий запрет,
// не должен считаться его нарушением. Собранный на лету SQL и миграции не
// покрыты, и это сказано здесь, а не подразумевается.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// verdictGlueRoot — путь принятия решения и обратных вопросов к нему.
//
// Перечень объявлен здесь, потому что он и есть ОБЪЁМ гейта, и печатается в
// переписи вместе с числом прочитанных файлов.
const verdictGlueRoot = "services/iam/internal/repo/kaname/pg/relverdict"

// gluePattern — склейка субъекта выдачи в любой форме написания алиаса.
//
// Ищется по ЯДРУ (`|| ':' ||` между двумя колонками субъекта), а не по точной
// строке с алиасом: алиас таблицы свободен, и предикат, привязанный к нему,
// нашёл бы ноль на первом же переименовании — то есть молчал бы ровно там, где
// должен говорить.
const (
	glueLeft  = "subject_type"
	glueRight = "subject_id"
)

type glueFinding struct {
	file, clause string
	line         int
	text         string
}

type glueCensus struct {
	files      int
	literals   int
	occurrence int // склеек субъекта встречено всего
	projection int // из них в списке выборки — законные близнецы
}

func TestVerdictSubjectIsComparedByColumnPairNotByGlue(t *testing.T) {
	findings, c := collectSubjectGlue(t, filepath.Join(repoRoot(t), verdictGlueRoot))

	// ПРОВЕРКА СВОЕЙ ПРЕДПОСЫЛКИ. Запрет обоснован тем, что в этих файлах есть
	// SQL и в нём есть колонки субъекта. Перестанет разбор их узнавать — «ноль
	// находок» будет означать «ноль прочитанного», и гейт станет зелен навсегда.
	if c.files == 0 || c.literals == 0 {
		t.Fatalf("предпосылка гейта не выполнена: файлов %d, литералов с SQL %d. "+
			"Либо каталог переехал, либо разбор перестал узнавать запросы — в обоих "+
			"случаях вердикт «чисто» ничего не значит", c.files, c.literals)
	}

	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: файлов %d, литералов с SQL %d, склеек субъекта встречено %d, "+
		"из них в списке выборки (законные близнецы) %d, находок %d",
		c.files, c.literals, c.occurrence, c.projection, len(findings))

	for _, f := range findings {
		t.Errorf("%s:%d: субъект выдачи сравнивается склейкой в предикате (%s): %s\n"+
			"    Склейка выводит обе колонки из-под индекса: отбор идёт по вычисленному "+
			"значению, то есть уже по прочитанным строкам. Сравнение обязано идти ПАРОЙ "+
			"КОЛОНОК (subject_type, subject_id).", f.file, f.line, f.clause, f.text)
	}
}

func collectSubjectGlue(t *testing.T, dir string) ([]glueFinding, glueCensus) {
	t.Helper()
	var (
		out []glueFinding
		c   glueCensus
	)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог пути вердикта не читается (%s): %v", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("файл %s: %v", name, err)
		}
		c.files++
		f, cc := auditFileForSubjectGlue(name, body)
		out = append(out, f...)
		c.literals += cc.literals
		c.occurrence += cc.occurrence
		c.projection += cc.projection
	}
	return out, c
}

func auditFileForSubjectGlue(name string, body []byte) ([]glueFinding, glueCensus) {
	var (
		out []glueFinding
		c   glueCensus
	)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, body, 0)
	if err != nil {
		return nil, c
	}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		sql, err := strconv.Unquote(lit.Value)
		if err != nil || !strings.Contains(sql, glueLeft) {
			return true
		}
		c.literals++
		base := fset.Position(lit.Pos()).Line
		f, cc := auditSQLForSubjectGlue(name, base, stripSQLLineComments(sql))
		out = append(out, f...)
		c.occurrence += cc.occurrence
		c.projection += cc.projection
		return true
	})
	return out, c
}

// auditSQLForSubjectGlue — разбор ИСПОЛНЯЕМОЙ части: комментарии сняты
// вызывающим, а положение склейки определяется КЛАУЗОЙ, в которой она стоит.
func auditSQLForSubjectGlue(file string, baseLine int, sql string) ([]glueFinding, glueCensus) {
	var (
		out []glueFinding
		c   glueCensus
	)
	for off := 0; ; {
		i := strings.Index(sql[off:], glueLeft)
		if i < 0 {
			break
		}
		at := off + i
		off = at + len(glueLeft)

		// Склейка — это `subject_type || ':' || …subject_id`. Хвост берётся
		// коротким окном: длиннее склейки он не бывает, а брать до конца строки
		// значило бы засчитать соседнее упоминание колонки за склейку.
		end := at + 64
		if end > len(sql) {
			end = len(sql)
		}
		win := sql[at:end]
		if !strings.Contains(win, "||") || !strings.Contains(win, glueRight) {
			continue
		}
		c.occurrence++
		clause := clauseAt(sql, at)
		if clause == "projection" {
			c.projection++
			continue
		}
		out = append(out, glueFinding{
			file: file, line: baseLine + strings.Count(sql[:at], "\n"),
			clause: clause, text: strings.TrimSpace(strings.SplitN(win, "\n", 2)[0]),
		})
	}
	return out, c
}

// clauseAt — в какой клаузе стоит смещение: в списке выборки или в предикате.
//
// Разбор идёт НАЗАД по ключевым словам: ближайшее слева определяет клаузу.
// Полноценного разбора SQL в дереве нет, и объявлять его здесь значило бы
// обещать больше, чем сделано; этого различения запрету достаточно, потому что
// оно отделяет ровно два состояния — «отбирает строки» и «называет ответ».
func clauseAt(sql string, off int) string {
	head := strings.ToUpper(sql[:off])
	type kw struct {
		word, clause string
	}
	// Порядок не важен: берётся САМОЕ ПРАВОЕ вхождение любого из них.
	words := []kw{
		{"SELECT", "projection"},
		{" ON ", "predicate"}, {"\nON ", "predicate"},
		{"WHERE", "predicate"}, {"HAVING", "predicate"},
		{" AND ", "predicate"}, {"\nAND ", "predicate"},
		{" OR ", "predicate"}, {"\nOR ", "predicate"},
	}
	best, clause := -1, "projection"
	for _, w := range words {
		if i := strings.LastIndex(head, w.word); i > best {
			best, clause = i, w.clause
		}
	}
	return clause
}
