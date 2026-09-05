// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listfilterapplied_test.go — гейт против владельца, который ОБЪЯВЛЯЕТ поле
// фильтруемым и не применяет разобранное выражение.
//
// Предмет. `filter.Parse(expr, whitelist)` возвращает разобранное выражение —
// поле, оператор и значение. Владелец, объявивший в whitelist БОЛЬШЕ ОДНОГО
// поля, обязан прочитать РАЗОБРАННОЕ поле: либо дословно (`ast.Field` — когда
// колонка адресуется не так, как поле написано), либо через `ast.ToSQL(...)`,
// который строит предикат из того же поля. Владелец, который вместо этого
// подставляет колонку литералом, отвечает ОДНОЙ колонкой на N объявленных
// полей: `filter=zone_id="…"` молча читается как фильтр по имени, и вызывающий
// получает уверенный, но неверный ответ.
//
// Почему именно N>1. При whitelist из одного поля литерал совпадает с
// объявлением by construction, и находки быть не может. Опасность появляется
// ровно в тот момент, когда во whitelist добавляют второе поле, — а это правка
// в одной строке, которая не выглядит как смена поведения. Гейт краснеет именно
// на ней, то есть в момент появления класса, а не тогда, когда кто-то снова
// пройдёт по дереву руками.
//
// Чего гейт НЕ утверждает. Он не проверяет, что литеральная колонка совпадает с
// именем поля при whitelist из одного поля (совпадение там не нарушаемо), и не
// проверяет соответствие имени поля имени колонки в SQL — это не выводится
// синтаксически. Объём осмотренного печатается, поэтому «ноль находок» отличимо
// от «ноль прочитанного».
//
// Разбор идёт по синтаксическому дереву, а не по тексту: имя `ToSQL` в
// комментарии, объясняющем эту же дисциплину, текстовый поиск принял бы за само
// применение.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// filterParseSite — одно место вызова filter.Parse с разобранным whitelist'ом.
type filterParseSite struct {
	file    string
	line    int
	fn      string
	fields  []string
	applied bool // прочитано ли разобранное поле (.Field или .ToSQL)
}

// collectFilterParseSites обходит дерево и находит вызовы `filter.Parse(...)` в
// НЕ-тестовом коде, определяя размер объявленного whitelist'а и то, читает ли
// охватывающая функция разобранное поле.
//
// Состав берётся у ИНДЕКСА (`pkg/treecorpus`), а не обходом диска: правила
// игнорирования действуют на любой глубине, и под `services/`, `gateway/`, `pkg/`
// на всякой машине, где поднимали стенд, лежат распаковки чартов, сборочные
// каталоги и отчёты прогонов. Обход диска сделал бы вердикт свойством рабочего
// каталога, а не коммита, — и в обе стороны: красный на файле, которого в
// репозитории нет, и молчание в свежем checkout там, где гейт обязан говорить.
func collectFilterParseSites(t *testing.T, roots []string) (sites []filterParseSite, filesRead int) {
	t.Helper()
	repo := repoRoot(t)

	for _, root := range roots {
		abs := filepath.Join(repo, root)
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		paths, err := treecorpus.UnderWithSuffix(abs, ".go")
		if err != nil {
			t.Fatalf("состав дерева под %s: %v — без него «ноль находок» неотличимо "+
				"от «ноль прочитанного»", root, err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			// testdata Go не компилирует: тамошний вызов `filter.Parse` — фикстура
			// чужой пробы, а не владелец списка полей.
			if strings.Contains(filepath.ToSlash(path), "/testdata/") {
				continue
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				continue
			}
			filesRead++

			// Пакетные []string-переменные — whitelist часто объявлен рядом.
			pkgLists := map[string][]string{}
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i < len(vs.Values) {
							if lit, ok := stringSliceLiteral(vs.Values[i]); ok {
								pkgLists[name.Name] = lit
							}
						}
					}
				}
			}

			rel, _ := filepath.Rel(repo, path)
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				// Применение ищем в области ОХВАТЫВАЮЩЕЙ функции: разбор и
				// построение предиката пишутся соседними строками.
				applied := false
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					sel, ok := n.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if sel.Sel.Name == "Field" || sel.Sel.Name == "ToSQL" {
						applied = true
					}
					return true
				})
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Parse" {
						return true
					}
					pkg, ok := sel.X.(*ast.Ident)
					if !ok || pkg.Name != "filter" {
						return true
					}
					if len(call.Args) < 2 {
						return true
					}
					fields, ok := stringSliceLiteral(call.Args[1])
					if !ok {
						if id, isIdent := call.Args[1].(*ast.Ident); isIdent {
							if lit, found := pkgLists[id.Name]; found {
								fields, ok = lit, true
							}
						}
					}
					if !ok {
						return true // whitelist не выводится синтаксически — не наш предмет
					}
					sites = append(sites, filterParseSite{
						file:    rel,
						line:    fset.Position(call.Pos()).Line,
						fn:      fd.Name.Name,
						fields:  fields,
						applied: applied,
					})
					return true
				})
			}
		}
	}
	return sites, filesRead
}

// stringSliceLiteral распознаёт `[]string{"a","b"}`.
func stringSliceLiteral(e ast.Expr) ([]string, bool) {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	at, ok := cl.Type.(*ast.ArrayType)
	if !ok {
		return nil, false
	}
	if id, ok := at.Elt.(*ast.Ident); !ok || id.Name != "string" {
		return nil, false
	}
	out := make([]string, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		bl, ok := el.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return nil, false
		}
		out = append(out, strings.Trim(bl.Value, `"`))
	}
	return out, true
}

// TestMultiFieldFilterOwnerAppliesTheParsedField — владелец, объявивший больше
// одного фильтруемого поля, обязан применять РАЗОБРАННОЕ поле.
func TestMultiFieldFilterOwnerAppliesTheParsedField(t *testing.T) {
	sites, filesRead := collectFilterParseSites(t, []string{"services", "gateway", "pkg"})

	multi := 0
	var findings []string
	for _, s := range sites {
		if len(s.fields) <= 1 {
			continue
		}
		multi++
		if !s.applied {
			findings = append(findings, s.file+":"+itoaFilterGate(s.line)+
				" — функция "+s.fn+" объявляет фильтруемыми поля "+
				strings.Join(s.fields, ", ")+
				", но не читает разобранное поле (ни .Field, ни .ToSQL): "+
				"объявлено "+itoaFilterGate(len(s.fields))+" полей, применяется одна колонка литералом")
		}
	}
	sort.Strings(findings)

	// Перепись — отдельное утверждение: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	t.Logf("осмотрено: %d не-тестовых файлов, %d мест filter.Parse с выводимым whitelist'ом, из них %d многополевых",
		filesRead, len(sites), multi)

	if filesRead == 0 || len(sites) == 0 {
		t.Fatalf("гейт ничего не прочитал (файлов %d, мест %d) — предпосылка не выполнена, "+
			"вердикт недействителен", filesRead, len(sites))
	}
	if multi == 0 {
		t.Fatalf("многополевых whitelist'ов в дереве не найдено при %d местах разбора — "+
			"распознавание whitelist'а сломано, гейт не может ничего поймать", len(sites))
	}
	if len(findings) > 0 {
		t.Fatalf("владелец объявил поле фильтруемым и не применяет разобранное выражение:\n  %s",
			strings.Join(findings, "\n  "))
	}
}

func itoaFilterGate(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
