// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// keystrengththreshold.go — разбор объявлений нижнего порога стойкости ключа
// (приёмка F1, сценарий F1-02).
//
// Порог обязан быть объявлен ЧИСЛОМ и ровно в одном месте дерева. Второе
// объявление — второе место об одном предмете: они разойдутся на первой же
// правке, и разойдутся в сторону «принимаем слабее», потому что ослаблять
// проще, чем ужесточать.
//
// # Что здесь считается ВТОРЫМ объявлением
//
// Не всякое число рядом с ключом. Порог — это СРАВНЕНИЕ измеренной стойкости с
// литералом:
//
//	if k.N.BitLen() < 2048 { ... }        ← второе объявление порога
//	if bits < min { ... }                 ← ЗАКОННО: сравнение с объявленным порогом
//	return 256                            ← ЗАКОННО: сообщение измеренной стойкости
//	const KeySize = 32                    ← ЗАКОННО: размер ключа обёртки, не порог
//
// Различает их не число, а РОЛЬ выражения. Разбор по числу («ищем 2048»)
// объявил бы находкой и длину порождаемого ключа, и размер буфера, и любое
// совпадение; разбор по сравнению стойкости с литералом называет ровно то, что
// решает «принять или отвергнуть».
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
// Порог, вынесенный в именованную константу пакета и сравниваемый через неё
// (`bits < rsaFloor`, где `rsaFloor = 2048`), разбору неотличим от законного
// сравнения с объявленным порогом: обе формы сравнивают с идентификатором.
// Закрыть это потребовало бы разрешения значений констант через пакеты, чего
// разбор не делает. Радиус ограничен намеренно и назван здесь, чтобы следующий
// читатель не принял молчание за доказательство.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// KeyStrengthDeclaration — объявление порога.
type KeyStrengthDeclaration struct {
	File string
	Line int
	Name string
	// Literals — числа, которые объявление возвращает. Порог обязан быть
	// ЧИСЛОМ: объявление, вернувшееся вычисляемым выражением, сверять не с чем.
	Literals []int
}

// KeyStrengthComparison — сравнение измеренной стойкости с литералом, то есть
// ВТОРОЕ объявление порога.
type KeyStrengthComparison struct {
	File string
	Line int
	// Expr — левая часть сравнения, как она написана: по ней читатель находит
	// место, не открывая файл.
	Expr string
	// Literal — число, с которым сравнивают.
	Literal int
	// Op — оператор сравнения.
	Op string
}

// KeyStrengthCensus — объём осмотренного одним файлом.
type KeyStrengthCensus struct {
	Funcs       int
	Comparisons int
}

// keyStrengthVocabulary — числа, которыми выражают стойкость ключа.
//
// Словарь закрыт намеренно: он отделяет «порог стойкости» от любого другого
// числа в коде. Без него находкой стал бы всякий предел — размер буфера, длина
// строки, число попыток, — и гейт отключили бы первым же ложным срабатыванием.
//
// Обратная сторона названа честно: порог, выраженный числом вне словаря (скажем,
// 1536), разбору не виден. Такое значение не выпускает ни один известный нам
// генератор ключей, и цена этой границы — пропуск экзотики, а не пропуск класса.
var keyStrengthVocabulary = map[int]bool{
	256: true, 384: true, 512: true, 521: true,
	1024: true, 2048: true, 3072: true, 4096: true,
}

// keyStrengthWords — слова, по которым выражение опознаётся мерой СТОЙКОСТИ.
var keyStrengthWords = map[string]bool{
	"bits":     true,
	"bitlen":   true,
	"bitsize":  true,
	"strength": true,
	"modulus":  true,
}

// ScanKeyStrengthThresholds разбирает один файл: объявления порога с названным
// именем и сравнения измеренной стойкости с литералом.
func ScanKeyStrengthThresholds(path string, src []byte, declName string) (
	[]KeyStrengthDeclaration, []KeyStrengthComparison, KeyStrengthCensus, error,
) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, nil, KeyStrengthCensus{}, err
	}

	var (
		decls    []KeyStrengthDeclaration
		compares []KeyStrengthComparison
		census   KeyStrengthCensus
	)

	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		census.Funcs++
		if fn.Name.Name != declName {
			continue
		}
		decl := KeyStrengthDeclaration{
			File: path, Line: fset.Position(fn.Pos()).Line, Name: fn.Name.Name,
		}
		if fn.Body != nil {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				ret, ok := n.(*ast.ReturnStmt)
				if !ok {
					return true
				}
				for _, r := range ret.Results {
					if v, ok := keyStrengthLiteral(r); ok {
						decl.Literals = append(decl.Literals, v)
					}
				}
				return true
			})
		}
		sort.Ints(decl.Literals)
		decls = append(decls, decl)
	}

	ast.Inspect(f, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		switch be.Op {
		case token.LSS, token.LEQ, token.GTR, token.GEQ:
		default:
			return true
		}
		census.Comparisons++
		// Стойкость бывает с любой стороны: `bits < 2048` и `2048 > bits`.
		for _, pair := range [2][2]ast.Expr{{be.X, be.Y}, {be.Y, be.X}} {
			strengthSide, literalSide := pair[0], pair[1]
			lit, ok := keyStrengthLiteral(literalSide)
			if !ok || !keyStrengthVocabulary[lit] {
				continue
			}
			if !keyStrengthExpr(strengthSide) {
				continue
			}
			compares = append(compares, KeyStrengthComparison{
				File:    path,
				Line:    fset.Position(be.Pos()).Line,
				Expr:    keyExprSource(strengthSide),
				Literal: lit,
				Op:      be.Op.String(),
			})
			break
		}
		return true
	})

	sort.Slice(compares, func(i, j int) bool { return compares[i].Line < compares[j].Line })
	return decls, compares, census, nil
}

// keyStrengthLiteral — целочисленный литерал выражения.
func keyStrengthLiteral(e ast.Expr) (int, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, false
	}
	v, err := strconv.Atoi(lit.Value)
	if err != nil {
		return 0, false
	}
	return v, true
}

// keyStrengthExpr — предикат «это выражение измеряет СТОЙКОСТЬ ключа».
//
// Опознаётся по словам исходного текста выражения, разложенного на слова по
// границам регистра. Разбор по подстроке спутал бы `bits` с `submitsCount`;
// разбор по словам их различает.
func keyStrengthExpr(e ast.Expr) bool {
	text := keyExprSource(e)
	if text == "" {
		return false
	}
	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '(' || r == ')' || r == '*' || r == '&' || r == '[' || r == ']'
	}) {
		// Сегмент целиком: BitLen и BitSize — устоявшиеся имена, и разложение
		// их на слова («bit»+«len») потеряло бы предмет.
		if keyStrengthWords[strings.ToLower(part)] {
			return true
		}
		// Он же по словам: keyBits и minBits несут меру отдельным словом, а
		// submitsCount содержит «bits» лишь ПОДСТРОКОЙ и мерой не является.
		for _, w := range keySplitWords(part) {
			if keyStrengthWords[w] {
				return true
			}
		}
	}
	return false
}

// keyExprSource — текст выражения, как он написан. Вызовы сохраняются вместе со
// скобками, чтобы `k.N.BitLen()` отличалось от поля `k.N.BitLen`.
func keyExprSource(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return keyExprSource(v.X) + "." + v.Sel.Name
	case *ast.CallExpr:
		return keyExprSource(v.Fun) + "()"
	case *ast.StarExpr:
		return "*" + keyExprSource(v.X)
	case *ast.IndexExpr:
		return keyExprSource(v.X) + "[]"
	case *ast.ParenExpr:
		return keyExprSource(v.X)
	case *ast.BinaryExpr:
		return keyExprSource(v.X) + " " + v.Op.String() + " " + keyExprSource(v.Y)
	default:
		return ""
	}
}
