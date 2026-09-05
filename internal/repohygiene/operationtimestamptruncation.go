// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// operationtimestamptruncation.go — метки времени Operation усечены до секунды
// на КАЖДОМ преобразователе дерева.
//
// # Предмет
//
// Конвенция продукта требует усечения `created_at`/`modified_at` до секунды в
// proto-ответе: БД хранит микросекунды, клиент их не видит. Operation — ответ
// КАЖДОЙ мутации всех семи сервисов, то есть самая частая поверхность, на
// которой доли секунды могут утечь на провод.
//
// # Почему гейт по дереву, а не два фикса
//
// Класс измерен: девять преобразователей, из них два не усекали (vpc, nlb).
// Починка двух закрывает находку и оставляет класс: следующий сервис напишет
// `timestamppb.New(op.CreatedAt)` и никто не заметит — разница видна только на
// проводе, ни один существующий тест её не спрашивал.
//
// # Что делает гейт и чего НЕ делает
//
// Он идёт по AST и на каждой сборке `operationpb.Operation{…}` требует, чтобы
// значения `CreatedAt`/`ModifiedAt` были усечены. Усечение засчитывается двумя
// способами, потому что оба живут в дереве законно:
//
//   - прямо в выражении — `timestamppb.New(op.CreatedAt.Truncate(time.Second))`;
//   - ЧЕРЕЗ ПОСРЕДНИКА — `ts(op.CreatedAt)` у compute, `TimestampProto(...)` у
//     iam. Посредник разрешается по имени: сперва в СВОЁМ каталоге, затем — для
//     квалифицированного вызова `pkg.F(...)` — в каталоге импортированного
//     пакета, вычисленном из пути импорта.
//
// Посредник обязан быть НАЙДЕН. Ненайденный не засчитывается: «вызывает что-то,
// чего я не вижу» — это не доказательство усечения, а его отсутствие. Ровно так
// работает страж посредника-пустышки в соседнем гейте, и по той же причине:
// цель, зовущая невидимое, зеленеет на любом содержимом.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// modulePath — префикс импортов этого модуля, чтобы перевести путь импорта в
// каталог. Живёт здесь, в НЕтестовом файле, а не рядом с первым потребителем:
// раньше константа стояла в `containerperpackage_test.go`, и второй гейт,
// которому она понадобилась, объявил бы её ЗАНОВО — два места об одном
// предмете, расходящиеся молча (первая редакция этого файла так и сделала,
// причём с другим написанием — без завершающей косой черты).
const modulePath = "github.com/PRO-Robotech/kacho/"

// truncationScanRoots — где ищем преобразователи.
var truncationScanRoots = []string{"services", "gateway", "pkg"}

// truncatedFields — поля Operation, несущие момент времени.
var truncatedFields = []string{"CreatedAt", "ModifiedAt"}

// truncationFinding — одна сборка Operation без усечения.
type truncationFinding struct {
	Where string // файл:строка
	Field string // какое поле
	Expr  string // что стоит значением
	Why   string // почему не засчитано
}

// truncationReport — исход обхода вместе с объёмом осмотренного.
type truncationReport struct {
	Findings     []truncationFinding
	FilesRead    int
	Constructors int // сборок operationpb.Operation{…} найдено
	Fields       int // полей времени рассмотрено
	ViaHelper    int // засчитано через посредника
}

// auditOperationTimestampTruncation обходит дерево.
func auditOperationTimestampTruncation(root string) (truncationReport, error) {
	var rep truncationReport

	// Тела функций дерева: каталог → имя → усекает ли. Собирается ОДИН раз, до
	// разбора сборок, потому что посредник может лежать в другом каталоге.
	helpers := map[string]map[string]bool{}
	files := map[string]*ast.File{}
	fsets := map[string]*token.FileSet{}

	for _, sub := range truncationScanRoots {
		dir := filepath.Join(root, sub)
		tracked, err := treecorpus.UnderWithSuffix(dir, ".go")
		if err != nil {
			return rep, fmt.Errorf("состав %s: %w", sub, err)
		}
		for _, abs := range tracked {
			rel, rerr := filepath.Rel(root, abs)
			if rerr != nil {
				return rep, fmt.Errorf("путь %s: %w", abs, rerr)
			}
			slashed := filepath.ToSlash(rel)
			if strings.HasSuffix(slashed, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, abs, nil, 0)
			if perr != nil {
				// Неразбираемый файл не «пропускается тихо»: это отказ.
				return rep, fmt.Errorf("разбор %s: %w", slashed, perr)
			}
			rep.FilesRead++
			files[slashed] = f
			fsets[slashed] = fset

			d := filepath.ToSlash(filepath.Dir(slashed))
			if helpers[d] == nil {
				helpers[d] = map[string]bool{}
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				helpers[d][fn.Name.Name] = bodyTruncates(fset, fn.Body)
			}
		}
	}

	for _, slashed := range sortedFileKeys(files) {
		f, fset := files[slashed], fsets[slashed]
		imports := importDirs(f)
		selfDir := filepath.ToSlash(filepath.Dir(slashed))

		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isOperationLit(lit.Type) {
				return true
			}
			rep.Constructors++
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || !isTruncatedField(key.Name) {
					continue
				}
				rep.Fields++
				switch verdict, why := valueTruncates(fset, kv.Value, selfDir, imports, helpers); verdict {
				case truncDirect:
					// прямое усечение — засчитано
				case truncViaHelper:
					rep.ViaHelper++
				default:
					rep.Findings = append(rep.Findings, truncationFinding{
						Where: fmt.Sprintf("%s:%d", slashed, fset.Position(kv.Pos()).Line),
						Field: key.Name,
						Expr:  exprText(fset, kv.Value),
						Why:   why,
					})
				}
			}
			return true
		})
	}

	sort.Slice(rep.Findings, func(i, j int) bool {
		if rep.Findings[i].Where == rep.Findings[j].Where {
			return rep.Findings[i].Field < rep.Findings[j].Field
		}
		return rep.Findings[i].Where < rep.Findings[j].Where
	})
	return rep, nil
}

type truncVerdict int

const (
	truncNone truncVerdict = iota
	truncDirect
	truncViaHelper
)

// valueTruncates решает, усечено ли значение поля.
func valueTruncates(
	fset *token.FileSet, v ast.Expr, selfDir string,
	imports map[string]string, helpers map[string]map[string]bool,
) (truncVerdict, string) {
	if containsTruncate(v) {
		return truncDirect, ""
	}
	// Посредник: ищем вызов, чьё имя разрешается в дереве.
	var (
		verdict = truncNone
		why     = "усечения нет ни в выражении, ни в вызванной функции"
	)
	ast.Inspect(v, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if t, found := helpers[selfDir][fn.Name]; found {
				if t {
					verdict = truncViaHelper
					return false
				}
				why = fmt.Sprintf("посредник %s() найден в своём каталоге и НЕ усекает", fn.Name)
			}
		case *ast.SelectorExpr:
			pkg, ok := fn.X.(*ast.Ident)
			if !ok {
				return true
			}
			dir, known := imports[pkg.Name]
			if !known {
				return true
			}
			if t, found := helpers[dir][fn.Sel.Name]; found {
				if t {
					verdict = truncViaHelper
					return false
				}
				why = fmt.Sprintf("посредник %s.%s() найден в %s и НЕ усекает",
					pkg.Name, fn.Sel.Name, dir)
			}
		}
		return true
	})
	return verdict, why
}

// bodyTruncates — тело функции содержит усечение до секунды.
//
// Обход тела делает сам containsTruncate: вкладывать один ast.Inspect в другой
// незачем, а первая редакция так и сделала — и звала containsTruncate на nil,
// которым обходчик отмечает выход из узла. Паника внутри гейта неотличима от
// падения проверяемого дерева.
func bodyTruncates(_ *token.FileSet, body *ast.BlockStmt) bool {
	return containsTruncate(body)
}

// containsTruncate — в поддереве есть вызов `.Truncate(time.Second)`.
func containsTruncate(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Truncate" || len(call.Args) != 1 {
			return true
		}
		// Аргумент обязан быть именно секундой: `Truncate(time.Millisecond)`
		// усечением по конвенции НЕ является.
		arg, ok := call.Args[0].(*ast.SelectorExpr)
		if !ok || arg.Sel.Name != "Second" {
			return true
		}
		if pkg, ok := arg.X.(*ast.Ident); !ok || pkg.Name != "time" {
			return true
		}
		found = true
		return false
	})
	return found
}

// isOperationLit — тип литерала есть `<пакет>.Operation` из домена operation.
func isOperationLit(t ast.Expr) bool {
	sel, ok := t.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Operation" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return strings.Contains(strings.ToLower(pkg.Name), "operation")
}

func isTruncatedField(name string) bool {
	for _, f := range truncatedFields {
		if f == name {
			return true
		}
	}
	return false
}

// importDirs — алиас пакета → каталог дерева (только импорты этого модуля).
func importDirs(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.HasPrefix(path, modulePath) {
			continue
		}
		dir := strings.TrimPrefix(path, modulePath)
		name := filepath.Base(dir)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out[name] = dir
	}
	return out
}

func exprText(fset *token.FileSet, e ast.Expr) string {
	start := fset.Position(e.Pos())
	end := fset.Position(e.End())
	if start.Line != end.Line {
		return fmt.Sprintf("<выражение на строках %d-%d>", start.Line, end.Line)
	}
	return fmt.Sprintf("<строка %d, колонки %d-%d>", start.Line, start.Column, end.Column)
}

// sortedFileKeys — отсортированные пути разобранных файлов. Отдельное имя, а не
// `sortedKeys`: та занята в пакете картой другого типа.
func sortedFileKeys(m map[string]*ast.File) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
