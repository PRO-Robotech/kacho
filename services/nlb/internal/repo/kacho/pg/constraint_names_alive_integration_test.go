// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
)

// TestGate_EveryMappedConstraintNameExistsInSchema — ГЕЙТ КЛАССА, зеркальный к
// TestGate_EveryRestrictFKHasBlockerNamingContract.
//
// ПРЕДМЕТ. Отображение SQLSTATE в контрактный текст ветвится по ИМЕНИ
// ограничения, которое назвал сервер. Имя, снятое миграцией, ветку не удаляет:
// она остаётся, компилируется, читается как действующая — и не исполняется
// никогда. Это мёртвый код с худшим свойством, чем обычный: он ОПИСЫВАЕТ
// поведение продукта, поэтому следующий читатель принимает его за контракт и
// пишет по нему приёмку либо «чинит» под него живую ветку.
//
// Гейт нашёл ровно такую ветку: композитный ключ прежней сводной таблицы,
// дропнутый вместе с ней (0018 снимает ограничение, 0022 — саму таблицу), а
// комментарий рядом объяснял, что запись держат «ради совместимости отката».
// Откат 0018 действительно её пересоздаёт, но ссылается на таблицу, которой
// после 0022 нет, — то есть основание записи не выполнялось само.
//
// ПРЕДПОСЫЛКА ГЕЙТА проверяется и печатается: разбор обязан найти НЕПУСТОЙ
// набор имён в ветвлении, иначе его молчание значит «ничего не прочитано», а не
// «находок нет».
//
// ЧИТАЕТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ: имена берутся разбором синтаксиса
// (`case`-литералы того `switch`, чей тег — поле имени ограничения), поэтому имя
// в комментарии или в строке сообщения гейт не считает объявленным — и наоборот,
// переносом ветки в другое место файла его не обмануть.
//
// РАСПОЗНАЮТСЯ ОБЕ ЗАКОННЫЕ ФОРМЫ ЗАПИСИ, и это не мелочь: форма, о которой
// распознаватель не знает, даёт не находку и не молчание, а НЕВИДИМОСТЬ — всё
// записанное в ней выпадает из наблюдения, и «имён найдено N» становится
// утверждением о меньшем, чем кажется. Формы две:
//
//	switch pgErr.ConstraintName { case "…": }   — имя поля драйвера
//	switch f.Constraint { case "…": }           — имя поля разобранного отказа
//	if f.Constraint == "…"                      — сравнение вместо ветвления
//
// Вторая появилась вместе с домом `pkg/db/pgfault`, третья была законной и до
// него — и до этой правки гейт не видел её вовсе.
func TestGate_EveryMappedConstraintNameExistsInSchema(t *testing.T) {
	mapped, files := constraintNamesSwitchedOn(t)
	require.NotEmpty(t, mapped,
		"разбор не нашёл ни одного имени ограничения в ветвлении по ConstraintName — "+
			"предпосылка гейта не выполняется, его молчание ничего не значит (осмотрено файлов: %d)", files)

	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	ctx := context.Background()

	// Сервер называет в 23505 ИМЯ ИНДЕКСА, а в 23503/23514 — имя ограничения,
	// поэтому живыми считаются оба множества: иначе гейт объявил бы мёртвыми все
	// ветки по уникальным индексам, которые как раз исполняются.
	alive := map[string]struct{}{}
	rows, err := pool.Query(ctx, `
		SELECT c.conname FROM pg_constraint c
		  JOIN pg_namespace n ON n.oid = c.connamespace
		 WHERE n.nspname = 'kacho_nlb'
		UNION
		SELECT c.relname FROM pg_class c
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'kacho_nlb' AND c.relkind = 'i'`)
	require.NoError(t, err)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		alive[name] = struct{}{}
	}
	rows.Close()
	require.NoError(t, rows.Err())
	require.NotEmpty(t, alive, "в схеме kacho_nlb не нашлось ни ограничений, ни индексов")

	var dead []string
	for _, name := range mapped {
		if _, ok := alive[name]; !ok {
			dead = append(dead, name)
		}
	}
	sort.Strings(dead)

	t.Logf("осмотрено: файлов отображения %d · имён в ветвлении %d · живых имён в схеме %d",
		files, len(mapped), len(alive))
	assert.Empty(t, dead,
		"ветка отображения ошибки по имени, которого в схеме нет, — исполниться не может "+
			"и описывает несуществующее поведение:\n  %s", strings.Join(dead, "\n  "))
}

// constraintNamesSwitchedOn — литералы `case` тех `switch`, чей тег обращается к
// полю ConstraintName. Возвращает имена и число осмотренных файлов.
func constraintNamesSwitchedOn(t *testing.T) ([]string, int) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	require.NoError(t, err)

	var names []string
	seen := map[string]struct{}{}
	files := 0
	for _, pkg := range pkgs {
		for range pkg.Files {
			files++
		}
		ast.Inspect(pkg, func(n ast.Node) bool {
			// Форма «сравнение»: `f.Constraint == "имя"`.
			if bin, ok := n.(*ast.BinaryExpr); ok && bin.Op == token.EQL {
				for _, side := range []ast.Expr{bin.X, bin.Y} {
					sel, isSel := side.(*ast.SelectorExpr)
					if !isSel || !constraintNameField(sel.Sel.Name) {
						continue
					}
					other := bin.Y
					if side == bin.Y {
						other = bin.X
					}
					lit, isLit := other.(*ast.BasicLit)
					if !isLit || lit.Kind != token.STRING {
						continue
					}
					v, uerr := strconv.Unquote(lit.Value)
					if uerr != nil {
						continue
					}
					if _, dup := seen[v]; !dup {
						seen[v] = struct{}{}
						names = append(names, v)
					}
				}
				return true
			}
			sw, ok := n.(*ast.SwitchStmt)
			if !ok || sw.Tag == nil {
				return true
			}
			sel, ok := sw.Tag.(*ast.SelectorExpr)
			if !ok || !constraintNameField(sel.Sel.Name) {
				return true
			}
			for _, stmt := range sw.Body.List {
				cc, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expr := range cc.List {
					lit, ok := expr.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					if _, dup := seen[v]; !dup {
						seen[v] = struct{}{}
						names = append(names, v)
					}
				}
			}
			return true
		})
	}
	sort.Strings(names)
	return names, files
}

// constraintNameField — имена полей, несущих имя нарушенного ограничения.
//
// Их два: `ConstraintName` у ошибки драйвера и `Constraint` у отказа,
// разобранного домом `pkg/db/pgfault`. Перечень закрыт намеренно: распознаватель,
// принимающий любое поле, начал бы засчитывать посторонние сравнения и завышать
// перепись — то есть отвечал бы шире, чем знает.
func constraintNameField(name string) bool {
	return name == "ConstraintName" || name == "Constraint"
}
