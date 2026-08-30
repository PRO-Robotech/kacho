// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratorsharedtract.go — та половина тракта наката, что проверяется БЕЗ базы,
// объявлена ОДИН раз на дерево.
//
// # Предмет: не «две формы», а второе объявление одного предмета
//
// Соседний гейт [TestMigratorFormIsOneOfTwoAndBothAreDeclared] стережёт ЧИСЛО
// форм и копий. Он молчит о другом: копия, уже существующая на законных
// основаниях, вправе завести у себя СВОЙ текст отказа и СВОЙ разбор `--target`,
// и потолок копий от этого не сдвинется. Именно так накопилось то, ради чего
// заведена задача #1383, и цена оказалась не гипотетической:
//
//   - `--target` разбирали ДВЕ функции, строгие по-разному. `12abc` общий разбор
//     отвергал, копия принимала КАК 12 (`fmt.Sscanf` останавливается на первом
//     несовпадении и ошибки не возвращает); `-5` копия отвергала, общий принимал
//     и отдавал goose. Оператор получал разный исход на одном вводе в
//     зависимости от того, какой сервис накатывает;
//   - тексты отказа предусловий жили тремя копиями, и КАЖДАЯ не называла один из
//     своих же источников DSN: две умалчивали запасную конфигурацию, третья —
//     переменную окружения, которая эту конфигурацию перебивает.
//
// Оба класса невидимы обзору диффа: каждая копия по отдельности защитима, а
// неверна их РАЗНИЦА.
//
// # Что требуется
//
//  1. ПОЛОЖИТЕЛЬНАЯ половина: общий пакет [migratorSharedTractHome] объявляет
//     ВСЕ тексты отказа предусловий. Это проверка собственной предпосылки
//     гейта: если тексты переименуют или унесут, отрицание ниже стало бы
//     вакуумным — искать было бы нечего, и «ноль находок» означало бы «ноль
//     предмета». Такое отрицание молчит вместо того, чтобы звать к себе
//     (testing.md §«Гейт на класс», п.9), поэтому премиса утверждается ОТДЕЛЬНО.
//  2. ОТРИЦАТЕЛЬНАЯ половина: ни один файл тракта вне общего пакета не объявляет
//     этих текстов заново и не заводит собственного разбора `--target`.
//
// # Почему разбор, а не поиск подстроки
//
// Имена текстов и `fmt.Sscanf` встречаются в ЭТОМ ЖЕ дереве в комментариях —
// в том числе в шапках, объясняющих сам запрет (см. выше: в этой шапке есть и
// «dsn is empty», и `fmt.Sscanf`). Гейт по подстроке краснел бы на собственном
// объяснении. Поэтому тексты берутся из УЗЛОВ-ЛИТЕРАЛОВ разобранного файла, а
// разбор `--target` — из объявлений функций и выражений вызова: комментарий
// узлом не является by construction.
//
// # Чего гейт НЕ утверждает
//
// Что накат сведён: goose по-прежнему зовут семь точек, и их сведение ждёт проб
// на живой базе (предусловие названо в `docs/architecture/migrator-form.md`).
// Здесь судится ровно та половина, которая проверяется без базы, — и потому
// сведена уже сегодня.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

const (
	// migratorSharedTractHome — единственный законный дом общей половины тракта.
	// Назван, а не выведен: «пакет, на который ссылаются» — определение через
	// тех, кого проверяем, и оно поехало бы вместе с ними.
	migratorSharedTractHome = "pkg/migratorcli/"

	// migratorTractDecisionDoc — где объявлена целевая форма.
	migratorTractDecisionDoc = "docs/architecture/migrator-form.md"
)

// migratorRefusalMarkers — тексты отказа предусловий наката. Подстроки, а не
// целые сообщения: у отказа про DSN хвост собирается из источников конкретного
// сервиса, и полное совпадение проверяло бы форматную строку, а не предмет.
var migratorRefusalMarkers = []string{
	"service is empty",
	"dialect is not set",
	"dialect spec.Name is empty",
	"dsn is empty",
	"migrations FS is nil",
	"migrations dir is empty",
}

// migratorTractFinding — одна находка с координатой.
type migratorTractFinding struct {
	Rel  string
	What string
}

// migratorTractCensus — объём осмотренного. Отдельное утверждение: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
type migratorTractCensus struct {
	FilesRead       int
	SharedFiles     int
	TractFiles      int
	MarkersDeclared int
	Redeclarations  int
	OwnTargetParser int
}

func (c migratorTractCensus) String() string {
	return fmt.Sprintf(
		"перепись: прочитано файлов %d (общий пакет %d · тракт %d) · "+
			"текстов отказа объявлено %d из %d · переобъявлений %d · своих разборов --target %d",
		c.FilesRead, c.SharedFiles, c.TractFiles,
		c.MarkersDeclared, len(migratorRefusalMarkers),
		c.Redeclarations, c.OwnTargetParser)
}

// migratorTractIsShared — файл принадлежит общему пакету.
func migratorTractIsShared(rel string) bool {
	return strings.HasPrefix(rel, migratorSharedTractHome)
}

// migratorTractIsEntryPoint — файл принадлежит тракту наката сервиса.
func migratorTractIsEntryPoint(rel string) bool {
	return strings.Contains(rel, "/cmd/migrator/") ||
		strings.Contains(rel, "/internal/apps/migrator/")
}

// stringLiteralsOfGoSource возвращает строковые литералы файла — УЗЛАМИ, а не
// поиском по тексту. Тем самым комментарий, называющий тот же текст, литералом
// не считается, и гейт не краснеет на прозе, объясняющей его собственный запрет.
func stringLiteralsOfGoSource(rel, src string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			return true
		}
		out = append(out, v)
		return true
	})
	return out, nil
}

// declaresOwnTargetParser — файл заводит СВОЙ разбор `--target`.
//
// Признаков два, и оба — узлы, а не слова. Перечень форм выведен из того, как
// разбор был записан в дереве до сведения, а не придуман: снятые копии
// объявляли функцию с этим именем и читали значение форматным разбором.
func declaresOwnTargetParser(rel, src string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}
	var out []string
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		if strings.HasSuffix(fn.Name.Name, "TargetVersion") {
			out = append(out, fmt.Sprintf("объявляет свой разбор цели %s()", fn.Name.Name))
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkg.Name == "fmt" && sel.Sel.Name == "Sscanf" {
			out = append(out, "читает значение форматным разбором fmt.Sscanf "+
				"(на \"12abc\" отдаёт 12 БЕЗ ошибки — накат уедет не туда)")
		}
		return true
	})
	return out, nil
}

// migratorTractFindingText формулирует находку так, чтобы она называла причину,
// а не симптом: читатель должен понять, что делать, не открывая этот файл.
func migratorTractFindingText(f migratorTractFinding) string {
	return fmt.Sprintf("%s: %s. Половина тракта, проверяемая без базы, объявлена "+
		"в %s — делегируй туда, а не заводи второе объявление (%s)",
		f.Rel, f.What, migratorSharedTractHome, migratorTractDecisionDoc)
}

func sortedFindingTexts(findings []migratorTractFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, migratorTractFindingText(f))
	}
	sort.Strings(out)
	return out
}
