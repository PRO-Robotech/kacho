// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Пакет repohygiene держит свойства ДЕРЕВА, а не одного сервиса.
//
// Здесь — паритет форм имени поля в известном наборе `update_mask`.
//
// ПРЕДМЕТ. Край разбирает тело запроса через protojson, а тот приводит имена в
// `updateMask` к именам полей контракта: клиент прислал `countryCode`, сервис
// получил `country_code`. Проверка маски сравнивает строки ДОСЛОВНО
// (`validate.UpdateMask`), поэтому набор, знающий только camelCase-форму
// многословного поля, объявляет поле изменяемым и не даёт изменить его НИ ПРИ
// КАКОМ входе через край — «два правила об одном поле» из конвенций.
//
// ПОЧЕМУ КЛАСС НЕ ВСКРЫВАЛСЯ. Односложное имя (`status`, `labels`) в обеих
// формах совпадает. Пока все изменяемые поля были односложными, расхождение
// невидимо; первое же многословное (`countryCode` у региона) дало отказ на
// сквозной пробе — 4 упавших утверждения при исправном во всём остальном пути.
//
// ПОЧЕМУ ПАРИТЕТ, А НЕ «ТОЛЬКО snake». Дословную форму выбирает не сервис:
// через край приходит snake_case, а прямой вызов gRPC несёт то, что положил
// вызывающий. Держать обе формы — уже принятое в дереве решение (registry несёт
// `project_id` и `projectId` рядом, с объяснением); гейт распространяет его на
// всех, вместо того чтобы заводить второе правило о том же предмете.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// maskSet — известный набор update_mask, найденный в дереве.
type maskSet struct {
	Pkg  string // каталог пакета — им же связываются вызов и объявление
	Name string
	File string
	Line int
	Keys []string
}

// MaskFormFinding — ключ, у которого нет двойника в другой форме.
type MaskFormFinding struct {
	File, Set, Key, Want string
	Line                 int
}

// snakeOf приводит lowerCamelCase к форме имени поля контракта посегментно:
// точка разделяет вложенные поля и в преобразовании не участвует.
func snakeOf(k string) string {
	segs := strings.Split(k, ".")
	for i, seg := range segs {
		var b strings.Builder
		for _, r := range seg {
			if r >= 'A' && r <= 'Z' {
				b.WriteByte('_')
				b.WriteRune(r + ('a' - 'A'))
				continue
			}
			b.WriteRune(r)
		}
		segs[i] = b.String()
	}
	return strings.Join(segs, ".")
}

// hasUpper отвечает, многословно ли имя в camelCase-форме.
func hasUpper(k string) bool {
	for _, r := range k {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

// CollectMaskSets разбирает исходники и возвращает наборы, КОТОРЫЕ РЕАЛЬНО
// переданы в validate.UpdateMask, плюс число осмотренных файлов и найденных
// вызовов. Наборы, никуда не переданные, предметом не являются: это может быть
// что угодно с той же формой типа.
func CollectMaskSets(sources map[string]string) (sets []maskSet, files, calls int) {
	fset := token.NewFileSet()
	decls := map[string]maskSet{} // "pkgdir\x00имя" → набор
	used := map[string]bool{}

	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, rel := range paths {
		f, err := parser.ParseFile(fset, rel, sources[rel], 0)
		if err != nil {
			continue // неразбираемый файл судить нечем; счётчик его не считает
		}
		files++
		dir := rel
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			dir = rel[:i]
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.ValueSpec:
				for i, name := range node.Names {
					if i >= len(node.Values) {
						break
					}
					cl, ok := node.Values[i].(*ast.CompositeLit)
					if !ok || !isStringStructSetType(cl.Type) {
						continue
					}
					keys := make([]string, 0, len(cl.Elts))
					for _, e := range cl.Elts {
						kv, ok := e.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						lit, ok := kv.Key.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						keys = append(keys, strings.Trim(lit.Value, `"`))
					}
					if len(keys) == 0 {
						continue
					}
					decls[dir+"\x00"+name.Name] = maskSet{
						Pkg: dir, Name: name.Name, File: rel,
						Line: fset.Position(name.Pos()).Line, Keys: keys,
					}
				}
			case *ast.CallExpr:
				if !isUpdateMaskCall(node.Fun) || len(node.Args) < 3 {
					return true
				}
				id, ok := node.Args[2].(*ast.Ident)
				if !ok {
					return true
				}
				calls++
				used[dir+"\x00"+id.Name] = true
			}
			return true
		})
	}

	keys := make([]string, 0, len(used))
	for k := range used {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := decls[k]; ok {
			sets = append(sets, s)
		}
	}
	return sets, files, calls
}

func isStringStructSetType(t ast.Expr) bool {
	m, ok := t.(*ast.MapType)
	if !ok {
		return false
	}
	kid, ok := m.Key.(*ast.Ident)
	if !ok || kid.Name != "string" {
		return false
	}
	st, ok := m.Value.(*ast.StructType)
	return ok && (st.Fields == nil || len(st.Fields.List) == 0)
}

func isUpdateMaskCall(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		return f.Sel.Name == "UpdateMask"
	case *ast.Ident:
		return f.Name == "UpdateMask"
	}
	return false
}

// JudgeMaskForms возвращает ключи, у которых нет двойника в другой форме, и
// число рассмотренных многословных ключей — «ноль находок» обязано быть
// отличимо от «ноль многословных ключей».
func JudgeMaskForms(sets []maskSet) (findings []MaskFormFinding, multiword int) {
	for _, s := range sets {
		have := map[string]bool{}
		for _, k := range s.Keys {
			have[k] = true
		}
		for _, k := range s.Keys {
			if !hasUpper(k) {
				continue
			}
			multiword++
			if want := snakeOf(k); !have[want] {
				findings = append(findings, MaskFormFinding{
					File: s.File, Line: s.Line, Set: s.Name, Key: k, Want: want,
				})
			}
		}
	}
	return findings, multiword
}
