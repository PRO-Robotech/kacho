// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// clusteranchorliteral.go — разбор «написание якоря кластера ОБЪЯВЛЕНО, а не
// повторено рукой».
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Якорь кластера — объект, на котором висит право кластерного администратора.
// Написание его лежит в базе (строка `kaname.clusters`, умолчание столбца,
// предикат ограничения-синглтона) и в коде — И В КОДЕ ОНО ЖИВЁТ ДВАЖДЫ: своей
// константой у фундамента и своей у службы, потому что модуля два и общей
// константы у них нет by construction.
//
// Пока каждое обращение идёт через одну из двух констант, переход написания —
// правка двух мест. Место, повторившее строку РУКОЙ, из этой арифметики
// выпадает: оно продолжает писать и читать прежний объект после того, как
// строка переехала, — и продолжает молча, потому что код собирается, а типы
// сходятся.
//
// Цена названа приёмкой F5: ошибка вокруг якоря отбирает доступ у ТОГО
// ЕДИНСТВЕННОГО, кто мог бы её починить.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЗНАЧЕНИЕ ЧИТАЕТСЯ ИЗ ОБЪЯВЛЕНИЯ, А НЕ ВПИСАНО В ГЕЙТ
//
// Написание якоря меняется — ради этого перехода гейт и заведён. Гейт, знающий
// строку наизусть, умер бы в тот самый день, когда она переехала, и умер бы
// молча: «литералов прежнего написания ноль» верно на дереве, где написание
// уже другое.
//
// Поэтому предикат сформулирован БЕЗ строки: значение берётся из самих
// объявлений, и гейт спрашивает «есть ли литерал, равный объявленному». Он
// переживает переход by construction и продолжает стеречь то же свойство.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ГЕЙТ УТВЕРЖДАЕТ, И ЭТО ДВА РАЗНЫХ УТВЕРЖДЕНИЯ
//
//  1. объявления СОГЛАСНЫ между собой. Два модуля, две константы, одна строка:
//     разойтись они могут молча, и разошедшиеся дадут службе и краю разные
//     объекты при одном вопросе о доступе;
//  2. литералов МИМО объявлений нет. Обращение через константу переезжает
//     вместе с ней; повторённая рукой строка — нет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА НАЗВАНА ВСЛУХ
//
//  1. Гейт судит СТРОКОВЫЙ ЛИТЕРАЛ, а не слово в файле. Комментарий, шапка
//     миграции и разбор в прозе называют якорь законно и обязаны продолжать —
//     запрет на слово краснел бы на исправном дереве и был бы снят первым же
//     обходом. Разбор читает узел синтаксического дерева, поэтому различает
//     это by construction.
//  2. Гейт судит РАВЕНСТВО, а не вхождение. Литерал, несущий написание внутри
//     фразы (описание поля провайдера, текст справки, запрос с якорем внутри
//     SQL), считается ОТДЕЛЬНО и печатается переписью: он тоже переезжает при
//     переходе, но у него нет формы «взять константу», и требовать её значило
//     бы требовать невозможного.
//  3. Гейт не смотрит на пробы и на порождённый код. Проба вправе называть оба
//     написания — именно этим она и судит переход.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// ClusterAnchorConstName — имя константы, объявляющей написание якоря.
//
// Имя ОДНО на оба модуля намеренно: константы две (модуля два), но носят они
// одно имя, поэтому разбор находит обе одним предикатом, а расхождение их
// значений становится находкой, а не невидимостью.
const ClusterAnchorConstName = "ClusterSingletonID"

// ClusterAnchorObjectPrefix — приставка формы объекта прав.
//
// Якорь встречается в двух формах: сам идентификатор и объект модели прав
// `cluster:<идентификатор>`. Вторая собирается сложением с константой, поэтому
// литерал, равный ей целиком, — тот же обход объявления.
const ClusterAnchorObjectPrefix = "cluster:"

// AnchorDeclaration — объявление написания якоря.
type AnchorDeclaration struct {
	File  string
	Line  int
	Value string
}

// AnchorFinding — место, повторившее написание якоря мимо объявления.
type AnchorFinding struct {
	File string
	Line int
	// Literal — то, что стоит в узле, целиком.
	Literal string
	// Kind — «якорь» либо «объект»: читателю находки надо знать, какую из двух
	// форм он видит, потому что чинятся они по-разному (константа против
	// сложения с приставкой).
	Kind string
}

// AnchorCensus — объём осмотренного. «Ноль находок» обязано быть отличимо от
// «ноль прочитанного».
type AnchorCensus struct {
	// Files — прочитано непроверочных файлов Go.
	Files int
	// Literals — осмотрено строковых литералов.
	Literals int
	// Declarations — найдено объявлений написания.
	Declarations int
	// Embedded — литералов, несущих написание ВНУТРИ фразы, а не равных ему.
	//
	// Печатается, но находкой не считается: у такого литерала нет формы «взять
	// константу» (описание поля, текст справки, запрос с якорем внутри SQL).
	// Он тоже переезжает при переходе, и знать его число надо — поэтому оно
	// названо, а не умолчано.
	Embedded int
}

// FindClusterAnchorLiterals разбирает исходники (имя → содержимое) и возвращает
// объявления написания якоря и места, повторившие его мимо объявлений.
//
// Значение якоря НЕ передаётся: оно берётся из самих объявлений. Гейт,
// знающий написание наизусть, умер бы молча в день его перехода.
func FindClusterAnchorLiterals(
	sources map[string]string,
) ([]AnchorDeclaration, []AnchorFinding, AnchorCensus, error) {
	var (
		decls    []AnchorDeclaration
		findings []AnchorFinding
		census   AnchorCensus
	)

	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	// Первый проход — объявления. Значение якоря обязано быть известно ДО
	// того, как разбор начнёт судить литералы: иначе он не знает, что ищет.
	type parsed struct {
		fset *token.FileSet
		file *ast.File
	}
	files := make(map[string]parsed, len(names))
	for _, name := range names {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, sources[name], parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, census, fmt.Errorf("разбор %s: %w", name, err)
		}
		files[name] = parsed{fset: fset, file: f}
		census.Files++

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
				for i, ident := range vs.Names {
					if ident.Name != ClusterAnchorConstName || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					decls = append(decls, AnchorDeclaration{
						File:  name,
						Line:  fset.Position(lit.Pos()).Line,
						Value: value,
					})
				}
			}
		}
	}
	census.Declarations = len(decls)
	if len(decls) == 0 {
		// Предпосылка гейта не выполнена: искать нечего, и его молчание
		// ничего не значит. Это отказ, а не «находок нет».
		return decls, nil, census, fmt.Errorf(
			"объявлений %s в дереве ноль — гейт не знает, какое написание стережёт",
			ClusterAnchorConstName)
	}

	// Множество законных значений и позиций объявлений.
	values := map[string]bool{}
	declAt := map[string]bool{}
	for _, d := range decls {
		values[d.Value] = true
		declAt[fmt.Sprintf("%s:%d", d.File, d.Line)] = true
	}

	// Второй проход — литералы.
	for _, name := range names {
		p := files[name]
		ast.Inspect(p.file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			census.Literals++
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			line := p.fset.Position(lit.Pos()).Line
			if declAt[fmt.Sprintf("%s:%d", name, line)] {
				return true // само объявление
			}
			for anchor := range values {
				switch {
				case value == anchor:
					findings = append(findings, AnchorFinding{
						File: name, Line: line, Literal: value, Kind: "якорь",
					})
					return true
				case value == ClusterAnchorObjectPrefix+anchor:
					findings = append(findings, AnchorFinding{
						File: name, Line: line, Literal: value, Kind: "объект",
					})
					return true
				case strings.Contains(value, anchor):
					census.Embedded++
					return true
				}
			}
			return true
		})
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	sort.Slice(decls, func(i, j int) bool { return decls[i].File < decls[j].File })
	return decls, findings, census, nil
}
