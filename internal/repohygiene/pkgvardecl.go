// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// pkgvardecl.go — разрешение package-level объявления по ПАКЕТУ, а не по файлу.
//
// # Предмет
//
// Гейт, читающий объявление прод-кода, обязан назвать, ЧТО он читает. Прежде
// внешние потребители называли это КООРДИНАТОЙ ФАЙЛА, и координата пережила свой
// предмет: объявление словаря типов переехало из `fga_types.go` в порождённый
// `tables_gen.go` (задача продукта #1092), а гейты остались на прежнем имени.
//
// # Цена измерена, а не предположена (задача продукта #1944)
//
// Внешних потребителей объявлений пакета `authzmap` — ТРИ; переезд сломал ДВА, и
// оба падали не находкой, а невозможностью отработать:
//
//	клиентская правда о наборе модулей   «в … не найдено объявление objectTypes»
//	проверка глаголов фикстур ролей      «FATAL: не прочитана закрытая таблица типов»
//	ось вердикта по меткам               перенесена вместе с литералом руками
//
// Третий уцелел только потому, что кто-то вспомнил о нём в том же изменении.
// Это и есть класс: пока предметом проверки служит ИМЯ ФАЙЛА, всякий перенос
// объявления внутри пакета обязан сопровождаться правкой каждого, кто его
// читает, — и невыполнение обнаруживается красным прогоном, а не обзором.
//
// # Что здесь предметом вместо файла
//
// ПАКЕТ. Он и есть единица области видимости Go: package-level имя в нём ровно
// одно by construction, и перенос объявления между файлами пакета ничего о нём
// не меняет. Разрешение по пакету снимает класс, а не его сегодняшний экземпляр.
//
// # Три отказа, и все три — отказы, а не молчание
//
//  1. в пакете НЕТ не-тестовых файлов Go — обход пуст, судить не по чему;
//  2. объявления с таким именем НЕТ — переименовано или снято; «находок ноль»
//     обязано быть отличимо от «прочитано ноль», поэтому число прочитанных
//     файлов называется в самом отказе;
//  3. объявлений БОЛЬШЕ ОДНОГО — два места об одном предмете. Внутри одного
//     пакета Go этого не бывает, но каталог может нести файлы разных пакетов
//     (`package foo` и `package foo_test`), и тогда молча взять первое значило бы
//     вынести вердикт о произвольной половине. Отказ называет ОБА файла.
//
// # Тестовые файлы не читаются, и это названо
//
// Синтетика проб держит собственные литералы тех же имён — счёт их сделал бы
// предмет функцией числа проб. Предмет здесь — объявление ПРОДУКТА.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// pkgVarDeclCensus — объём осмотренного при разрешении объявления.
//
// Печатается вызывающим ВСЕГДА: без него «объявление найдено» неотличимо от
// «прочитан один файл из двадцати, и повезло».
type pkgVarDeclCensus struct {
	// PkgFiles — не-тестовых файлов Go пакета прочитано.
	PkgFiles int
	// DeclFile — файл, в котором объявление нашлось (относительный путь).
	DeclFile string
}

// findPackageVarLiteral находит составной литерал package-level переменной
// varName в пакете pkgDir (один уровень каталога, без тестовых файлов).
//
// Возвращает литерал и объём осмотренного. Любой из трёх отказов выше — ошибка,
// а не пустой результат: пустой результат вызывающий прочитал бы как «предмета
// нет», а это «прочитать не удалось».
func findPackageVarLiteral(
	tree *treecorpus.Tree, pkgDir, varName string,
) (*ast.CompositeLit, pkgVarDeclCensus, error) {
	var census pkgVarDeclCensus

	var files []string
	for _, rel := range clientTruthTreeFiles(tree, pkgDir, false, ".go") {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		files = append(files, rel)
	}
	if len(files) == 0 {
		return nil, census, fmt.Errorf(
			"в пакете %s нет ни одного не-тестового файла Go — обход пуст, "+
				"объявление %s искать не в чем", pkgDir, varName)
	}

	var (
		lit   *ast.CompositeLit
		found []string
	)
	for _, rel := range files {
		src, rerr := clientTruthReadTreeFile(tree, rel)
		if rerr != nil {
			return nil, census, fmt.Errorf("чтение %s: %w", rel, rerr)
		}
		census.PkgFiles++
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, rel, src, 0)
		if perr != nil {
			return nil, census, fmt.Errorf("разбор %s: %w", rel, perr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			decl, ok := n.(*ast.GenDecl)
			if !ok || decl.Tok != token.VAR {
				return true
			}
			for _, spec := range decl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if name.Name != varName || i >= len(vs.Values) {
						continue
					}
					cl, ok := vs.Values[i].(*ast.CompositeLit)
					if !ok {
						continue
					}
					lit = cl
					found = append(found, rel)
				}
			}
			return true
		})
	}

	switch len(found) {
	case 0:
		return nil, census, fmt.Errorf(
			"в пакете %s не найдено объявления %s составным литералом "+
				"(не-тестовых файлов прочитано %d) — оно переименовано или снято",
			pkgDir, varName, census.PkgFiles)
	case 1:
		census.DeclFile = found[0]
		return lit, census, nil
	default:
		sort.Strings(found)
		return nil, census, fmt.Errorf(
			"в пакете %s объявление %s встречено %d раза (%s) — два места об одном "+
				"предмете, и вердикт о произвольном из них ничего не значит",
			pkgDir, varName, len(found), strings.Join(found, ", "))
	}
}

// pkgVarLiteralStringKeys — ключи-строки составного литерала, в порядке
// объявления, вместе с их числом.
//
// Ключи берутся УЗЛАМИ, а не текстом: имена стоят и в комментариях рядом с
// объявлением, и распознаватель по подстроке вывел бы набор из объяснения.
func pkgVarLiteralStringKeys(lit *ast.CompositeLit) []string {
	out := make([]string, 0, len(lit.Elts))
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		bl, ok := kv.Key.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			continue
		}
		key, uerr := strconv.Unquote(bl.Value)
		if uerr != nil || key == "" {
			continue
		}
		out = append(out, key)
	}
	return out
}
