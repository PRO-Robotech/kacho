// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokencheckcomposition.go — разбор состава обязательных проверок токена
// (приёмка F1, сценарий F1-28).
//
// Три разных вопроса, три разных разбора, и смешивать их нельзя:
//
//  1. ГДЕ ОБЪЯВЛЕН перечень обязательных проверок — объявлений обязано быть одно;
//  2. КТО СТРОИТ проверяющего — построения находятся в точках сборки, по
//     ПОЛНОМУ пути импорта, а не по имени пакета в исходнике;
//  3. ЧТО ОБЪЯВЛЯЕТ о себе реализация — состав проверок, который она называет.
//
// # Почему построение опознаётся по пути импорта, а не по имени
//
// Имя пакета в исходнике задаёт вызывающий: `jwks.New` и `verifier.New` бывают
// одним и тем же пакетом, а бывают разными. Разбор читает объявления импортов
// файла и приводит вызов к полному пути — тогда псевдоним ничего не решает, и
// словарь производителей не приходится держать в двух написаниях.
//
// # Почему состав читается ПО ПАКЕТУ, а не по телу метода
//
// Реализация собирает своё объявление как ей удобно: часть проверок перечислена
// переменной уровня пакета, часть добавляется по условию. Разбор тела метода
// пришлось бы учить каждой форме сборки, и он расходился бы с ней при первой же
// правке. Поэтому вопрос ставится шире и грубее: КАКИЕ константы перечня пакет
// вообще называет.
//
// Цена этого выбора названа честно: константа, названная и не исполняемая,
// разбору неотличима от исполняемой. Разбор судит ОБЪЯВЛЕНИЕ; правдивость
// объявления держит проба самого пакета, а не гейт по дереву. Гейт закрывает
// другой класс — тот, где состав вообще НЕ ВЫРАЖЕН и потому не может покраснеть
// ни у кого.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// checkPrefix — имя типа константы перечня; оно же префикс имён самих констант.
//
// Одна величина, а не две: тип и его константы названы одним корнем намеренно, и
// разводить их здесь значило бы завести два места об одном предмете внутри
// разбора, который этот класс и запрещает.
const checkPrefix = "Check"

// TokenCheckSite — координата в дереве.
type TokenCheckSite struct {
	File string
	Line int
	Name string
}

// VerifierConstruction — построение проверяющего в точке сборки.
type VerifierConstruction struct {
	File string
	Line int
	// Producer — ПОЛНЫЙ путь импорта плюс имя функции: то, чем построение
	// опознаётся независимо от псевдонима пакета в исходнике.
	Producer string
}

// CheckNaming — упоминание константы перечня в исходнике.
type CheckNaming struct {
	File string
	Line int
	// Ident — имя константы (CheckExpiry), не её значение.
	Ident string
	// Reasoned — рядом с упоминанием объявлена ПРИЧИНА. Требуется от проверки
	// СВЕРХ перечня: молчаливое расхождение является находкой.
	Reasoned bool
}

// TokenCheckCensus — объём осмотренного одним файлом.
type TokenCheckCensus struct {
	Decls    int
	Calls    int
	Idents   int
	Comments int
}

// ScanCheckListDeclarations — объявления перечня обязательных проверок.
//
// Объявлением считается ФУНКЦИЯ либо ПЕРЕМЕННАЯ уровня пакета с этим именем.
// Второе объявление — второе место об одном предмете: оно разойдётся с первым на
// первой же правке, и различие между двумя перечнями не станет ничьей находкой,
// потому что не будет выражено.
func ScanCheckListDeclarations(path string, src []byte, listName string) ([]TokenCheckSite, TokenCheckCensus, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, TokenCheckCensus{}, err
	}
	var out []TokenCheckSite
	var census TokenCheckCensus
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			census.Decls++
			if d.Recv == nil && d.Name.Name == listName {
				out = append(out, TokenCheckSite{
					File: path, Line: fset.Position(d.Pos()).Line, Name: d.Name.Name,
				})
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, nm := range vs.Names {
					census.Decls++
					if nm.Name == listName {
						out = append(out, TokenCheckSite{
							File: path, Line: fset.Position(nm.Pos()).Line, Name: nm.Name,
						})
					}
				}
			}
		}
	}
	return out, census, nil
}

// ScanCheckConstants — константы перечня: имя идентификатора и его значение.
//
// Читается ОБЪЯВЛЕНИЕ, а не память: сопоставление «идентификатор → значение»
// нужно разбору, чтобы сверять названный состав с перечнем, и вторая копия этого
// сопоставления разошлась бы с первой молча.
func ScanCheckConstants(path string, src []byte, typeName string) (map[string]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || vs.Type == nil {
				continue
			}
			id, ok := vs.Type.(*ast.Ident)
			if !ok || id.Name != typeName {
				continue
			}
			for i, nm := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				out[nm.Name] = val
			}
		}
	}
	return out, nil
}

// ScanVerifierConstructions — построения проверяющего в этом файле.
//
// producers ключуется ПОЛНЫМ путём импорта плюс именем функции. Вызов
// приводится к тому же виду через объявления импортов файла, поэтому псевдоним
// пакета на исход не влияет.
func ScanVerifierConstructions(path string, src []byte, producers map[string]bool) ([]VerifierConstruction, TokenCheckCensus, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, TokenCheckCensus{}, err
	}
	imports := tokenCheckImportAliases(f)
	var out []VerifierConstruction
	var census TokenCheckCensus
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		census.Calls++
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		importPath, ok := imports[pkgIdent.Name]
		if !ok {
			return true
		}
		full := importPath + "." + sel.Sel.Name
		if producers[full] {
			out = append(out, VerifierConstruction{
				File: path, Line: fset.Position(call.Pos()).Line, Producer: full,
			})
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, census, nil
}

// ScanCheckComposition — что этот файл объявляет о составе своих проверок.
//
// Возвращает объявления состава (функции с названным именем, возвращающие срез
// констант перечня) и упоминания самих констант вместе с признаком «рядом
// объявлена причина».
func ScanCheckComposition(
	path string, src []byte, policyImportPath, declName string,
) (decls []TokenCheckSite, namings []CheckNaming, census TokenCheckCensus, err error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, parser.ParseComments)
	if perr != nil {
		return nil, nil, TokenCheckCensus{}, perr
	}
	alias := ""
	for name, p := range tokenCheckImportAliases(f) {
		if p == policyImportPath {
			alias = name
			break
		}
	}
	if alias == "" {
		// Файл политику не импортирует — говорить о его составе нечего.
		return nil, nil, TokenCheckCensus{}, nil
	}

	// Строки, на которых объявлена причина. Причина ищется в КОММЕНТАРИИ, а не в
	// имени: имя обязано называть проверку, а не оправдываться.
	reasonLines := map[int]bool{}
	for _, group := range f.Comments {
		for _, c := range group.List {
			census.Comments++
			if tokenCheckMentionsReason(c.Text) {
				reasonLines[fset.Position(c.Pos()).Line] = true
			}
		}
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != declName {
			continue
		}
		if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
			continue
		}
		arr, ok := fn.Type.Results.List[0].Type.(*ast.ArrayType)
		if !ok {
			continue
		}
		sel, ok := arr.Elt.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != alias {
			continue
		}
		decls = append(decls, TokenCheckSite{
			File: path, Line: fset.Position(fn.Pos()).Line, Name: fn.Name.Name,
		})
	}

	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != alias {
			return true
		}
		census.Idents++
		// Отбор по префиксу — ДЕШЁВЫЙ предварительный, а не решающий: какая из
		// названных величин действительно константа перечня, решает таблица
		// констант у вызывающего. Имя самого ТИПА из отбора исключено явно —
		// оно встречается в каждой сигнатуре пакета и константой не является.
		if !strings.HasPrefix(sel.Sel.Name, checkPrefix) || sel.Sel.Name == checkPrefix {
			return true
		}
		line := fset.Position(sel.Pos()).Line
		reasoned := false
		// Причина считается объявленной, если комментарий стоит на той же строке
		// либо в пределах пяти строк выше: перечень пишут столбиком, и причина
		// одного элемента стоит над ним, а не в конце файла.
		for l := line - 5; l <= line; l++ {
			if reasonLines[l] {
				reasoned = true
				break
			}
		}
		namings = append(namings, CheckNaming{
			File: path, Line: line, Ident: sel.Sel.Name, Reasoned: reasoned,
		})
		return true
	})
	sort.Slice(namings, func(i, j int) bool { return namings[i].Line < namings[j].Line })
	return decls, namings, census, nil
}

// tokenCheckMentionsReason — предикат «в комментарии объявлена причина».
//
// Словарь закрыт и мал намеренно: чем шире перечень слов, тем ближе он к
// «всякий комментарий сойдёт за обоснование», а это и есть молчаливое
// расхождение, которое требование про причину и запрещает.
func tokenCheckMentionsReason(text string) bool {
	lower := strings.ToLower(text)
	for _, w := range []string{"причин", "reason", "обоснован"} {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// tokenCheckImportAliases — «имя пакета в исходнике → путь импорта».
//
// Пакет без явного псевдонима опознаётся по последнему сегменту пути. Это не
// всегда верно (имя пакета вправе отличаться от каталога), и там, где неверно,
// разбор просто не найдёт производителя — молча пропустит, а не выдумает.
// Граница названа здесь, чтобы её проверяли, а не предполагали.
func tokenCheckImportAliases(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			name = p[i+1:]
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				continue
			}
			name = imp.Name.Name
		}
		out[name] = p
	}
	return out
}
